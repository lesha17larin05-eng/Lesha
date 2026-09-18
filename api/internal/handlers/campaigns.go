package handlers

import (
	"context"
	"encoding/json"
	"html"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/middleware"
)

// Рассылки из админки.
//
// Как это работает:
//  1. Алексей создаёт рассылку: имя, тема, текст, группа получателей, темп.
//     В этот момент список получателей фиксируется (снимок группы).
//  2. Отправляет тестовое письмо себе, смотрит.
//  3. Жмёт «Отправить» — рассылка переходит в статус sending.
//  4. Фоновый отправщик раз в sendTick берёт одного адресата и шлёт письмо,
//     пока не упрётся в дневной лимит рассылки или общий предохранитель.
//
// Письма уходят с того же ящика, что и письма о доступах, поэтому темп
// низкий, а общий суточный предохранитель — ниже лимита Яндекса (300).

const (
	// Как часто отправщик просыпается. Реальный интервал между письмами
	// задаётся у каждой рассылки (campaigns.pause_sec) — тик только проверяет,
	// не пора ли отправить следующее.
	sendTick        = 10 * time.Second
	globalDailyCap  = 200 // максимум писем рассылок в сутки, всего
	maxDailyLimit   = 150 // максимум писем в сутки у одной рассылки
	minPauseSec     = 30
	maxPauseSec     = 3600
	defaultPauseSec = 120 // две минуты — спокойный темп по умолчанию
	recipientsLimit = 500 // сколько адресатов показываем на странице
)

type campaignReq struct {
	Name       string `json:"name"`
	Subject    string `json:"subject"`
	Body       string `json:"body"`
	Segment    string `json:"segment"`
	DailyLimit int    `json:"daily_limit"`
	PauseSec   int    `json:"pause_sec"`
}

func (a *App) AdminSegments(w http.ResponseWriter, r *http.Request) {
	segs, err := a.Repo.SegmentsWithCounts(r.Context())
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	writeJSON(w, 200, map[string]any{
		"segments":         segs,
		"daily_limit_max":  maxDailyLimit,
		"daily_limit_hint": 50,
		"global_daily_cap": globalDailyCap,
		"pause_sec_hint":   defaultPauseSec,
		"pause_sec_min":    minPauseSec,
		"pause_sec_max":    maxPauseSec,
	})
}

func (a *App) AdminCreateCampaign(w http.ResponseWriter, r *http.Request) {
	var in campaignReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Subject = strings.TrimSpace(in.Subject)
	in.Body = strings.TrimSpace(in.Body)
	if in.Name == "" || in.Subject == "" || in.Body == "" {
		writeErr(w, 400, "empty_fields")
		return
	}
	if !db.SegmentExists(in.Segment) {
		writeErr(w, 400, "bad_segment")
		return
	}
	if in.DailyLimit < 1 {
		in.DailyLimit = 50
	}
	if in.DailyLimit > maxDailyLimit {
		in.DailyLimit = maxDailyLimit
	}
	if in.PauseSec == 0 {
		in.PauseSec = defaultPauseSec
	}
	if in.PauseSec < minPauseSec {
		in.PauseSec = minPauseSec
	}
	if in.PauseSec > maxPauseSec {
		in.PauseSec = maxPauseSec
	}
	id, total, err := a.Repo.CreateCampaign(r.Context(), in.Name, in.Subject, in.Body, in.Segment, in.DailyLimit, in.PauseSec)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "campaign_create", "campaign", &id,
		map[string]any{"segment": in.Segment, "total": total,
			"daily_limit": in.DailyLimit, "pause_sec": in.PauseSec})
	writeJSON(w, 201, map[string]any{"id": id, "total": total})
}

func (a *App) AdminListCampaigns(w http.ResponseWriter, r *http.Request) {
	list, err := a.Repo.ListCampaigns(r.Context())
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	if list == nil {
		list = []db.Campaign{}
	}
	writeJSON(w, 200, list)
}

func (a *App) AdminGetCampaign(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	c, err := a.Repo.GetCampaign(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	rows, _ := a.Repo.CampaignRecipientRows(r.Context(), id, recipientsLimit)
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"campaign": c, "recipients": rows})
}

var campaignActions = map[string]string{"start": "sending", "pause": "paused"}

func (a *App) AdminCampaignAction(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	status, ok := campaignActions[chi.URLParam(r, "action")]
	if !ok {
		writeErr(w, 400, "bad_action")
		return
	}
	if _, err := a.Repo.GetCampaign(r.Context(), id); err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	if err := a.Repo.SetCampaignStatus(r.Context(), id, status); err != nil {
		writeErr(w, 500, "db")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "campaign_"+chi.URLParam(r, "action"), "campaign", &id, nil)
	writeJSON(w, 200, map[string]any{"ok": 1, "status": status})
}

// AdminTestCampaign шлёт письмо рассылки самому админу — посмотреть,
// как оно выглядит, до отправки людям.
func (a *App) AdminTestCampaign(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	c, err := a.Repo.GetCampaign(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	u, err := a.Repo.GetUser(r.Context(), adminID)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	body := a.campaignHTML(c, u.Name, u.ID)
	if err := a.Mail.Send(u.Email, "[ТЕСТ] "+c.Subject, body); err != nil {
		writeErr(w, 500, "smtp")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": 1, "sent_to": u.Email})
}

// ---- Сборка письма ----

var urlRe = regexp.MustCompile(`https?://[^\s<>"]+`)

// campaignHTML превращает простой текст в письмо: абзацы, ссылки,
// подпись, ссылка отписки и невидимый счётчик открытий.
func (a *App) campaignHTML(c *db.Campaign, name string, userID uuid.UUID) string {
	greeting := "Здравствуйте!"
	if n := strings.TrimSpace(name); n != "" {
		greeting = "Здравствуйте, " + html.EscapeString(strings.Fields(n)[0]) + "!"
	}

	var sb strings.Builder
	sb.WriteString(`<!doctype html><html lang="ru"><head><meta charset="utf-8"></head>` +
		`<body style="margin:0;padding:0;background:#fdfaf5;">` +
		`<div style="max-width:560px;margin:0 auto;padding:28px 22px;` +
		`font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Arial,sans-serif;` +
		`font-size:16px;line-height:1.65;color:#1a1a1a;">`)
	sb.WriteString(`<p>` + greeting + `</p>`)

	for _, para := range strings.Split(strings.ReplaceAll(c.Body, "\r\n", "\n"), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		esc := html.EscapeString(para)
		esc = urlRe.ReplaceAllStringFunc(esc, func(u string) string {
			return `<a href="` + u + `" style="color:#e8652a;">` + u + `</a>`
		})
		esc = strings.ReplaceAll(esc, "\n", "<br>")
		sb.WriteString(`<p>` + esc + `</p>`)
	}

	sb.WriteString(`<p>Алексей Ларин<br><span style="color:#555;">Тренер по развитию здоровья</span></p>`)

	unsub := a.unsubscribeURL(userID)
	sb.WriteString(`<hr style="border:none;border-top:1px solid #ece8e0;margin:26px 0 14px;">` +
		`<p style="font-size:13px;color:#777;line-height:1.6;margin:0;">` +
		`Вы получили это письмо, потому что регистрировались на leshalarin.ru и согласились получать новости. ` +
		`<a href="` + unsub + `" style="color:#777;">Отписаться</a> – письма про ваши курсы при этом останутся.</p>`)
	sb.WriteString(`<img src="` + a.pixelURL(c.ID.String(), userID) +
		`" width="1" height="1" alt="" style="display:block;width:1px;height:1px;border:0;">`)
	sb.WriteString(`</div></body></html>`)
	return sb.String()
}

func (a *App) unsubscribeURL(userID uuid.UUID) string {
	return a.Cfg.AppHost + "/api/unsubscribe?u=" + userID.String() +
		"&t=" + UnsubscribeToken(a.Cfg.JWTSecret, userID.String())
}

func (a *App) pixelURL(campaign string, userID uuid.UUID) string {
	return a.Cfg.AppHost + "/api/pixel.gif?u=" + userID.String() +
		"&c=" + campaign + "&t=" + PixelToken(a.Cfg.JWTSecret, campaign, userID.String())
}

// ---- Фоновый отправщик ----

// RunCampaignWorker раз в sendTick отправляет одно письмо активной рассылки.
// Останавливается по отмене контекста.
func RunCampaignWorker(ctx context.Context, a *App) {
	t := time.NewTicker(sendTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := a.sendOneCampaignEmail(ctx); err != nil && err != db.ErrNotFound {
				slog.Warn("campaign worker", "err", err)
			}
		}
	}
}

func (a *App) sendOneCampaignEmail(ctx context.Context) error {
	// Общий предохранитель: рассылки не должны съесть суточный лимит
	// ящика, с которого уходят письма о доступах к курсам.
	if n, err := a.Repo.SentLast24h(ctx); err != nil {
		return err
	} else if n >= globalDailyCap {
		return nil
	}

	c, err := a.Repo.NextCampaignToSend(ctx)
	if err != nil {
		return err
	}
	rec, err := a.Repo.NextRecipient(ctx, c.ID)
	if err == db.ErrNotFound {
		// адресаты кончились — рассылка завершена
		return a.Repo.SetCampaignStatus(ctx, c.ID, "done")
	}
	if err != nil {
		return err
	}

	body := a.campaignHTML(c, rec.Name, rec.UserID)
	headers := map[string]string{
		"List-Unsubscribe":      "<" + a.unsubscribeURL(rec.UserID) + ">",
		"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
	}
	if err := a.Mail.SendWithHeaders(rec.Email, c.Subject, body, headers); err != nil {
		slog.Warn("campaign send failed", "email", rec.Email, "err", err)
		return a.Repo.MarkRecipient(ctx, rec.ID, "failed", err.Error())
	}
	slog.Info("campaign sent", "campaign", c.Name, "email", rec.Email)
	return a.Repo.MarkRecipient(ctx, rec.ID, "sent", "")
}
