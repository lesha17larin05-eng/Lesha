package handlers

import (
	"context"
	"encoding/json"
	"html"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/middleware"
)

// Автоцепочка писем после регистрации на «Мягкий старт».
//
// Как работает:
//  1. Раз в dripTick воркер ищет одного человека, которому пора очередной шаг
//     (условия – db.NextDripRecipient), и отправляет ему одно письмо.
//  2. Письма уходят с 9:00 до 21:00 по Москве, не чаще одного в день на человека.
//  3. Письма с предложением (Offer) тем, у кого уже есть «Здоровая спина» или
//     оплачена «Точка перемен», не уходят – шаг помечается skipped.
//  4. Всё выключается тумблером drip_enabled в админке (/admin/settings).
//
// Письма уходят с того же ящика Яндекса, что и рассылки, поэтому у цепочки
// свой суточный лимит и общий с рассылками предохранитель.

const (
	dripTick       = 30 * time.Second
	dripDailyCap   = 100 // писем цепочки в сутки
	mailboxSafeCap = 250 // рассылки + цепочка в сутки (лимит Яндекса – 300, нужен запас на письма о доступах)
	dripFromHour   = 9   // по Москве
	dripToHour     = 21
)

// moscowNow – Москва живёт в UTC+3 круглый год. Фиксированный сдвиг вместо
// time.LoadLocation: в контейнере может не быть базы часовых поясов.
func moscowNow(now time.Time) time.Time { return now.UTC().Add(3 * time.Hour) }

func dripEnabled(ctx context.Context, a *App) bool {
	s, _ := a.Repo.Settings(ctx)
	return s["drip_enabled"] == "true"
}

// RunDripWorker – фоновая отправка цепочки. Останавливается по отмене контекста.
func RunDripWorker(ctx context.Context, a *App) {
	t := time.NewTicker(dripTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := a.sendOneDripEmail(ctx, time.Now()); err != nil && err != db.ErrNotFound {
				slog.Warn("drip worker", "err", err)
			}
		}
	}
}

// sendOneDripEmail отправляет не больше одного письма цепочки. now передаётся
// параметром ради тестов.
func (a *App) sendOneDripEmail(ctx context.Context, now time.Time) error {
	if !dripEnabled(ctx, a) {
		return nil
	}
	if h := moscowNow(now).Hour(); h < dripFromHour || h >= dripToHour {
		return nil
	}
	dripSent, err := a.Repo.DripSentLast24h(ctx)
	if err != nil {
		return err
	}
	campSent, err := a.Repo.SentLast24h(ctx)
	if err != nil {
		return err
	}
	if dripSent >= dripDailyCap || dripSent+campSent >= mailboxSafeCap {
		return nil
	}
	startAt, err := a.Repo.DripStartAt(ctx)
	if err != nil {
		return err
	}

	for _, st := range dripSteps {
		rec, err := a.Repo.NextDripRecipient(ctx, st.Step, st.Day, startAt, now)
		if err == db.ErrNotFound {
			continue
		}
		if err != nil {
			return err
		}
		if st.Offer {
			paid, err := a.Repo.HasPaidProduct(ctx, rec.UserID)
			if err != nil {
				return err
			}
			if paid {
				// Уже купил – предложение не шлём. Следующего адресата
				// обработаем на следующем тике.
				return a.Repo.RecordDrip(ctx, rec.UserID, st.Step, "skipped", "")
			}
		}
		body := a.dripHTML(st, rec.Name, rec.UserID)
		headers := map[string]string{
			"List-Unsubscribe":      "<" + a.unsubscribeURL(rec.UserID) + ">",
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		}
		if err := a.Mail.SendWithHeaders(rec.Email, st.Subject, body, headers); err != nil {
			slog.Warn("drip send failed", "step", st.Step, "email", rec.Email, "err", err)
			return a.Repo.RecordDrip(ctx, rec.UserID, st.Step, "failed", err.Error())
		}
		slog.Info("drip sent", "step", st.Step, "email", rec.Email)
		return a.Repo.RecordDrip(ctx, rec.UserID, st.Step, "sent", "")
	}
	return db.ErrNotFound
}

// ---- Сборка письма ----

var (
	dripLinkRe     = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^\s)]+)\)`)
	dripBoldRe     = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	dripOnlyLinkRe = regexp.MustCompile(`^\[([^\]]+)\]\((https?://[^\s)]+)\)$`)
	dripOLRe       = regexp.MustCompile(`^\d+\.\s+`)
)

const (
	dripLinkStyle = `color:#e8652a;`
	dripBtnStyle  = `display:inline-block;background:#e8652a;color:#fff;text-decoration:none;` +
		`padding:13px 26px;border-radius:100px;font-size:16px;font-weight:600;`
)

func dripInline(s string) string {
	s = html.EscapeString(s)
	s = dripLinkRe.ReplaceAllString(s, `<a href="$2" style="`+dripLinkStyle+`">$1</a>`)
	s = dripBoldRe.ReplaceAllString(s, `<b>$1</b>`)
	return s
}

// dripGreeting подставляет имя: «{имя}, привет!» → «Анна, привет!» или «Привет!».
func dripGreeting(body, name string) string {
	first := ""
	if f := strings.Fields(strings.TrimSpace(name)); len(f) > 0 {
		first = f[0]
	}
	if first == "" {
		body = strings.ReplaceAll(body, "{имя}, привет!", "Привет!")
		return strings.ReplaceAll(body, "{имя}", "")
	}
	return strings.ReplaceAll(body, "{имя}", first)
}

// renderDripBody – разметка из drip_texts.go в HTML.
func renderDripBody(body string) string {
	var sb strings.Builder
	for _, block := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		switch {
		case dripOnlyLinkRe.MatchString(block):
			m := dripOnlyLinkRe.FindStringSubmatch(block)
			sb.WriteString(`<p style="margin:22px 0;"><a href="` + html.EscapeString(m[2]) + `" style="` + dripBtnStyle + `">` +
				html.EscapeString(m[1]) + `</a></p>`)
		case strings.HasPrefix(lines[0], "- "):
			sb.WriteString(`<ul style="padding-left:22px;margin:0 0 16px;">`)
			for _, l := range lines {
				sb.WriteString(`<li style="margin:0 0 8px;">` + dripInline(strings.TrimPrefix(l, "- ")) + `</li>`)
			}
			sb.WriteString(`</ul>`)
		case dripOLRe.MatchString(lines[0]):
			sb.WriteString(`<ol style="padding-left:22px;margin:0 0 16px;">`)
			for _, l := range lines {
				sb.WriteString(`<li style="margin:0 0 8px;">` + dripInline(dripOLRe.ReplaceAllString(l, "")) + `</li>`)
			}
			sb.WriteString(`</ol>`)
		case strings.HasPrefix(lines[0], "> "):
			var q []string
			for _, l := range lines {
				q = append(q, dripInline(strings.TrimPrefix(strings.TrimPrefix(l, ">"), " ")))
			}
			sb.WriteString(`<blockquote style="margin:0 0 16px;padding:12px 18px;border-left:3px solid #e8652a;` +
				`background:#fdf3e8;border-radius:0 10px 10px 0;font-style:italic;">` + strings.Join(q, "<br>") + `</blockquote>`)
		default:
			var ls []string
			for _, l := range lines {
				ls = append(ls, dripInline(l))
			}
			sb.WriteString(`<p style="margin:0 0 16px;">` + strings.Join(ls, "<br>") + `</p>`)
		}
	}
	return sb.String()
}

// dripHTML – письмо целиком: тело, подвал с отпиской и счётчик открытий.
// Подпись уже есть в тексте, поэтому отдельно её не добавляем.
func (a *App) dripHTML(st dripStep, name string, userID uuid.UUID) string {
	var sb strings.Builder
	sb.WriteString(`<!doctype html><html lang="ru"><head><meta charset="utf-8"></head>` +
		`<body style="margin:0;padding:0;background:#fdfaf5;">` +
		`<div style="max-width:560px;margin:0 auto;padding:28px 22px;` +
		`font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Arial,sans-serif;` +
		`font-size:16px;line-height:1.65;color:#1a1a1a;">`)
	sb.WriteString(renderDripBody(dripGreeting(st.Body, name)))
	sb.WriteString(`<hr style="border:none;border-top:1px solid #ece8e0;margin:26px 0 14px;">`)
	sb.WriteString(`<p style="font-size:13px;color:#777;line-height:1.6;margin:0;">` +
		`Вы получили это письмо, потому что записались на бесплатный курс «Мягкий старт» на leshalarin.ru ` +
		`и согласились получать материалы. <a href="` + a.unsubscribeURL(userID) + `" style="color:#777;">Отписаться</a> – ` +
		`доступ к урокам при этом останется.</p>`)
	sb.WriteString(`<img src="` + a.pixelURL("drip-"+strconv.Itoa(st.Step), userID) +
		`" width="1" height="1" alt="" style="display:block;width:1px;height:1px;border:0;">`)
	sb.WriteString(`</div></body></html>`)
	return sb.String()
}

// ---- Админка ----

// AdminDrip – GET /api/admin/drip: включена ли цепочка и сводка по шагам.
func (a *App) AdminDrip(w http.ResponseWriter, r *http.Request) {
	stats, err := a.Repo.DripStats(r.Context(), len(dripSteps))
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	startAt, _ := a.Repo.DripStartAt(r.Context())
	steps := make([]map[string]any, 0, len(dripSteps))
	for i, st := range dripSteps {
		row := map[string]any{"step": st.Step, "day": st.Day, "subject": st.Subject, "offer": st.Offer}
		if i < len(stats) {
			row["sent"], row["skipped"], row["failed"], row["opened"] =
				stats[i].Sent, stats[i].Skipped, stats[i].Failed, stats[i].Opened
		}
		steps = append(steps, row)
	}
	writeJSON(w, 200, map[string]any{
		"enabled": dripEnabled(r.Context(), a), "start_at": startAt, "steps": steps,
	})
}

// AdminDripTest – POST /api/admin/drip/test {"email"?}: все письма цепочки
// себе на почту с пометкой [ТЕСТ], чтобы посмотреть, как они выглядят.
func (a *App) AdminDripTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	to := strings.TrimSpace(in.Email)
	if to == "" {
		to = a.Cfg.LeadNotifyEmail
	}
	if !strings.Contains(to, "@") || len(to) < 5 {
		writeErr(w, 400, "bad_email")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	u, err := a.Repo.GetUser(r.Context(), adminID)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	for _, st := range dripSteps {
		body := a.dripHTML(st, u.Name, u.ID)
		if err := a.Mail.Send(to, "[ТЕСТ "+strconv.Itoa(st.Step)+"/"+strconv.Itoa(len(dripSteps))+"] "+st.Subject, body); err != nil {
			slog.Warn("drip test send failed", "to", to, "err", err)
			writeErr(w, 500, "smtp")
			return
		}
	}
	a.Repo.Audit(r.Context(), adminID, "drip_test", "drip", nil, map[string]any{"to": to})
	writeJSON(w, 200, map[string]any{"ok": 1, "sent_to": to, "count": len(dripSteps)})
}
