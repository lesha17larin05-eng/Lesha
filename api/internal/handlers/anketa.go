package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/db"
)

// Анкета перед «Точкой перемен».
//
// После оплаты услуги человек получает письмо со ссылкой /anketa?o=<order>&t=<hmac>.
// Страница берёт вопросы отсюда (GET /api/anketa), ответы уходят POST'ом,
// сохраняются в questionnaires и приходят Алексею на почту. Заполнить можно
// повторно – анкета перезапишется. Без галочки согласия на обработку
// сведений о здоровье (152-ФЗ, особая категория) анкета не принимается.

const anketaPrefix = "anketa:"

func AnketaToken(secret, orderID string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(anketaPrefix + orderID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *App) anketaURL(orderID uuid.UUID) string {
	return a.Cfg.AppHost + "/anketa?o=" + orderID.String() + "&t=" + AnketaToken(a.Cfg.JWTSecret, orderID.String())
}

type anketaQuestion struct {
	Key      string `json:"key"`
	Group    string `json:"group"`
	Label    string `json:"label"`
	Hint     string `json:"hint,omitempty"`
	Required bool   `json:"required"`
	Short    bool   `json:"short"` // одна строка вместо многострочного поля
	Health   bool   `json:"health,omitempty"`
}

// Вопросы анкеты. Меняя формулировки, не меняйте ключи – по ним хранятся ответы.
var anketaQuestions = []anketaQuestion{
	{Key: "goal", Group: "О цели", Required: true, Label: "Что хотите изменить в первую очередь?",
		Hint: "Спина, вес, энергия, осанка, вернуться к спорту – или что-то своё"},
	{Key: "in3months", Group: "О цели", Label: "Представьте, что прошло 3 месяца и всё получилось. Что изменилось в вашей жизни?"},
	{Key: "pain", Group: "О цели", Label: "Что сейчас беспокоит больше всего?",
		Hint: "Если есть боль или дискомфорт – где, когда появляется и насколько сильно от 0 до 10"},
	{Key: "activity", Group: "Опыт", Required: true, Label: "Как вы двигались раньше и как сейчас?",
		Hint: "Спорт, зал, йога, прогулки – или почти ничего"},
	{Key: "stopped", Group: "Опыт", Label: "Если уже пробовали заниматься – что мешало продолжить?"},
	{Key: "likes", Group: "Опыт", Label: "Какие движения или нагрузки вам нравятся, а какие точно нет?"},
	{Key: "injuries", Group: "Здоровье и ограничения", Health: true, Label: "Были ли травмы, переломы или операции? Какие и когда.",
		Hint: "Если не было – так и напишите"},
	{Key: "limits", Group: "Здоровье и ограничения", Health: true,
		Label: "Есть ли ограничения от врача, диагнозы, связанные со спиной, суставами, сердцем или давлением? Сейчас беременность или недавние роды?"},
	{Key: "fears", Group: "Здоровье и ограничения", Health: true, Label: "Есть ли упражнения, которые вызывают боль или которых вы опасаетесь?"},
	{Key: "sleep", Group: "Образ жизни", Label: "Как вы спите – сколько часов, легко ли засыпаете и встаёте?"},
	{Key: "day", Group: "Образ жизни", Label: "Как проходит обычный день?", Hint: "Сидячая работа или на ногах, сколько примерно шагов"},
	{Key: "energy", Group: "Образ жизни", Short: true, Label: "Уровень стресса и энергии сейчас – от 1 до 10",
		Hint: "Например: стресс 7, энергия 4"},
	{Key: "time", Group: "Практическое", Required: true, Label: "Сколько раз в неделю и сколько минут реально готовы заниматься? Где – дома, на улице, в зале? Есть ли коврик, резинки, гантели?"},
	{Key: "slots", Group: "Практическое", Required: true, Label: "Когда удобно провести занятие (55 минут онлайн)?",
		Hint: "2–3 варианта дней и времени"},
	{Key: "contact", Group: "Практическое", Short: true, Label: "Где удобнее переписываться?",
		Hint: "Телеграм @ник или номер для WhatsApp / Max"},
	{Key: "extra", Group: "Практическое", Label: "Что-то, чего я не спросил, но что важно знать?"},
}

const anketaMaxLen = 3000 // символов на ответ

// anketaOrder – заказ услуги из подписанной ссылки, иначе nil.
func (a *App) anketaOrder(r *http.Request) *db.Order {
	id, err := uuid.Parse(r.URL.Query().Get("o"))
	t := r.URL.Query().Get("t")
	if err != nil || t == "" {
		return nil
	}
	if !hmac.Equal([]byte(AnketaToken(a.Cfg.JWTSecret, id.String())), []byte(t)) {
		return nil
	}
	o, err := a.Repo.GetOrder(r.Context(), id)
	if err != nil || o.Service == "" || o.Status != "paid" {
		return nil
	}
	return o
}

// GetAnketa – GET /api/anketa?o=&t=: вопросы, имя и уже сохранённые ответы.
func (a *App) GetAnketa(w http.ResponseWriter, r *http.Request) {
	o := a.anketaOrder(r)
	if o == nil {
		writeErr(w, 400, "invalid_link")
		return
	}
	name := ""
	if u, err := a.Repo.GetUser(r.Context(), o.UserID); err == nil {
		if f := strings.Fields(u.Name); len(f) > 0 {
			name = f[0]
		}
	}
	answers := map[string]string{}
	submitted := false
	if q, err := a.Repo.GetQuestionnaire(r.Context(), o.ID); err == nil {
		answers, submitted = q.Answers, true
	}
	writeJSON(w, 200, map[string]any{
		"name": name, "questions": anketaQuestions, "answers": answers, "submitted": submitted,
	})
}

// SubmitAnketa – POST /api/anketa?o=&t= {answers, consent_health}.
func (a *App) SubmitAnketa(w http.ResponseWriter, r *http.Request) {
	o := a.anketaOrder(r)
	if o == nil {
		writeErr(w, 400, "invalid_link")
		return
	}
	var in struct {
		Answers       map[string]string `json:"answers"`
		ConsentHealth bool              `json:"consent_health"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	clean := map[string]string{}
	var missing []string
	for _, q := range anketaQuestions {
		v := strings.TrimSpace(in.Answers[q.Key])
		if rs := []rune(v); len(rs) > anketaMaxLen {
			v = string(rs[:anketaMaxLen])
		}
		if v != "" {
			clean[q.Key] = v
		} else if q.Required {
			missing = append(missing, q.Key)
		}
	}
	// Обе проблемы сразу – чтобы человек поправил всё за один раз.
	if !in.ConsentHealth {
		writeJSON(w, 400, map[string]any{"error": "consent_health_required", "missing": missing})
		return
	}
	if len(missing) > 0 {
		writeJSON(w, 400, map[string]any{"error": "required", "missing": missing})
		return
	}
	_, existed := a.Repo.GetQuestionnaire(r.Context(), o.ID)
	if err := a.Repo.SaveQuestionnaire(r.Context(), o.ID, o.UserID, clean); err != nil {
		slog.Warn("anketa save", "err", err)
		writeErr(w, 500, "db")
		return
	}
	a.notifyAnketa(r, o, clean, existed == nil)
	writeJSON(w, 200, map[string]any{"ok": 1})
}

// notifyAnketa – ответы анкеты Алексею на почту.
func (a *App) notifyAnketa(r *http.Request, o *db.Order, answers map[string]string, updated bool) {
	u, err := a.Repo.GetUser(r.Context(), o.UserID)
	if err != nil {
		return
	}
	subject := "Анкета «Точки перемен»: " + u.Name
	if updated {
		subject = "Анкета обновлена: " + u.Name
	}
	var sb strings.Builder
	sb.WriteString(`<div style="font-family:-apple-system,Segoe UI,Roboto,Arial,sans-serif;font-size:15px;line-height:1.55;max-width:640px;">`)
	sb.WriteString(`<p><b>` + html.EscapeString(u.Name) + `</b> &lt;` + html.EscapeString(u.Email) + `&gt; · заказ №` +
		strconv.FormatInt(o.OrderNum, 10) + `</p>`)
	group := ""
	for _, q := range anketaQuestions {
		if q.Group != group {
			group = q.Group
			sb.WriteString(`<h3 style="margin:22px 0 8px;font-size:16px;color:#1a2744;">` + html.EscapeString(group) + `</h3>`)
		}
		v := answers[q.Key]
		if v == "" {
			v = "—"
		}
		sb.WriteString(`<p style="margin:0 0 12px;"><span style="color:#777;">` + html.EscapeString(q.Label) + `</span><br>` +
			strings.ReplaceAll(html.EscapeString(v), "\n", "<br>") + `</p>`)
	}
	sb.WriteString(`<p style="margin-top:24px;"><a href="` + a.Cfg.AppHost + `/admin/users/` + u.ID.String() + `">Карточка в админке →</a></p></div>`)
	a.Mail.Async(a.Cfg.LeadNotifyEmail, subject, sb.String())
}
