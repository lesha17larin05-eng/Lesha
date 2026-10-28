package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/middleware"
)

// Анкета перед «Точкой перемен».
//
// Ссылка /anketa?u=<user>&t=<hmac> – одна на человека. Приходит письмом после
// оплаты услуги или от Алексея напрямую (админка: карточка пользователя или
// «Анкета для клиента» по email). Страница берёт вопросы отсюда
// (GET /api/anketa), ответы уходят POST'ом, сохраняются в questionnaires и
// приходят Алексею на почту. Заполнить можно повторно – анкета перезапишется. Без галочки согласия на обработку
// сведений о здоровье (152-ФЗ, особая категория) анкета не принимается.

const anketaPrefix = "anketa:"

func AnketaToken(secret, userID string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(anketaPrefix + userID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *App) anketaURL(userID uuid.UUID) string {
	return a.anketaURLKind(userID, "start")
}

// anketaURLKind – ссылка на анкету нужного вида (start – перед первым
// занятием, gruppa – перед групповым ведением). Вид в подпись не входит:
// он выбирает только набор вопросов.
func (a *App) anketaURLKind(userID uuid.UUID, kind string) string {
	u := a.Cfg.AppHost + "/anketa?u=" + userID.String() + "&t=" + AnketaToken(a.Cfg.JWTSecret, userID.String())
	if kind != "" && kind != "start" {
		u += "&k=" + kind
	}
	return u
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

// Вопросы анкеты группового ведения (утверждены 23.10.2026) и замеры до старта.
var anketaGruppaQuestions = []anketaQuestion{
	{Key: "name_age", Group: "О вас", Required: true, Short: true, Label: "Имя и возраст"},
	{Key: "telegram", Group: "О вас", Required: true, Short: true, Label: "Ваш ник в Telegram",
		Hint: "Чтобы добавить вас в чат группы"},
	{Key: "goal", Group: "Цель", Required: true, Label: "Чего хотите добиться за эти 8 недель?",
		Hint: "Например: выглядеть подтянутее, стать сильнее, больше энергии, наладить регулярность – или что-то своё"},
	{Key: "success", Group: "Цель", Label: "Как вы поймёте, что всё получилось?"},
	{Key: "activity", Group: "Опыт", Required: true, Label: "Какая у вас сейчас физическая активность?",
		Hint: "Спорт, зал, бег, прогулки – или почти никакой"},
	{Key: "stopped", Group: "Опыт", Label: "Если уже пробовали заниматься регулярно, что мешало продолжить?"},
	{Key: "injuries", Group: "Здоровье", Health: true, Label: "Были ли травмы, переломы или операции? Какие и когда"},
	{Key: "limits", Group: "Здоровье", Health: true, Label: "Есть ли ограничения или рекомендации от врача?"},
	{Key: "pain", Group: "Здоровье", Health: true, Label: "Что-то беспокоит в теле сейчас?",
		Hint: "Если есть боль – где и насколько сильно, от 0 до 10"},
	{Key: "sleep", Group: "Образ жизни", Label: "Как вы спите: сколько часов, легко ли засыпаете и встаёте?"},
	{Key: "day", Group: "Образ жизни", Label: "Как проходит обычный день?", Hint: "Сидячая работа или на ногах, сколько примерно шагов"},
	{Key: "energy", Group: "Образ жизни", Short: true, Label: "Уровень энергии сейчас, от 1 до 10"},
	{Key: "stress", Group: "Образ жизни", Short: true, Label: "Уровень стресса сейчас, от 1 до 10"},
	{Key: "days", Group: "Тренировки", Required: true, Label: "В какие дни недели вам удобнее тренироваться? Есть ли гиря – какого веса и работали ли с ней?"},
	{Key: "extra", Group: "Тренировки", Label: "Что ещё мне важно о вас знать?"},
	{Key: "height", Group: "Замеры – утром, до завтрака", Required: true, Short: true, Label: "Рост, см"},
	{Key: "weight", Group: "Замеры – утром, до завтрака", Required: true, Short: true, Label: "Вес, кг"},
	{Key: "waist", Group: "Замеры – утром, до завтрака", Required: true, Short: true, Label: "Обхват талии, см",
		Hint: "По линии пупка, живот не втягивать"},
	{Key: "hips", Group: "Замеры – утром, до завтрака", Required: true, Short: true, Label: "Обхват бёдер, см",
		Hint: "По самой широкой части"},
	{Key: "breath", Group: "Замеры – утром, до завтрака", Required: true, Short: true, Label: "Задержка дыхания, секунд",
		Hint: "3 спокойных вдоха-выдоха, на 4-м вдохе задержите дыхание и засеките время"},
}

// anketaForm – вид анкеты: тексты страницы, тема письма и вопросы.
type anketaForm struct {
	Eyebrow   string           `json:"eyebrow"`
	Title     string           `json:"title"` // %s – имя, если известно
	Lead      string           `json:"lead"`
	Done      string           `json:"done"`
	Subject   string           `json:"-"` // тема письма Алексею
	Questions []anketaQuestion `json:"-"`
}

var anketaForms = map[string]anketaForm{
	"start": {
		Eyebrow: "Перед занятием", Title: "пара вопросов перед занятием",
		Lead:    "Отвечайте своими словами – можно коротко. Чем честнее, тем точнее я соберу план под вас. Займёт 5–7 минут.",
		Done:    "Прочитаю её и напишу вам в течение 1–2 дней – договоримся о времени занятия.",
		Subject: "Анкета перед занятием", Questions: anketaQuestions,
	},
	"gruppa": {
		Eyebrow: "Групповое ведение", Title: "анкета и замеры перед стартом",
		Lead: "Отвечайте своими словами – можно коротко. По анкете я соберу ваш план на первую неделю, а замеры сравним в конце. " +
			"Займёт 10 минут.",
		Done:    "Прочитаю её и соберу ваш план на первую неделю. Добавлю вас в чат группы в Telegram ближе к старту. До встречи 7 ноября!",
		Subject: "Анкета группы", Questions: anketaGruppaQuestions,
	},
}

// anketaFormFor – вид анкеты по ключу; неизвестный – «start».
func anketaFormFor(kind string) (string, anketaForm) {
	if f, ok := anketaForms[kind]; ok {
		return kind, f
	}
	return "start", anketaForms["start"]
}

// anketaKind – какую анкету показать: из ссылки (?k=), иначе ту, что человек
// уже заполнял, иначе «start».
func anketaKind(r *http.Request, saved *db.Questionnaire) string {
	if k := r.URL.Query().Get("k"); k != "" {
		k, _ = anketaFormFor(k)
		return k
	}
	if saved != nil {
		k, _ := anketaFormFor(saved.Kind)
		return k
	}
	return "start"
}

const anketaMaxLen = 3000 // символов на ответ

// anketaUser – пользователь из подписанной ссылки, иначе nil.
func (a *App) anketaUser(r *http.Request) *db.User {
	id, err := uuid.Parse(r.URL.Query().Get("u"))
	t := r.URL.Query().Get("t")
	if err != nil || t == "" {
		return nil
	}
	if !hmac.Equal([]byte(AnketaToken(a.Cfg.JWTSecret, id.String())), []byte(t)) {
		return nil
	}
	u, err := a.Repo.GetUser(r.Context(), id)
	if err != nil {
		return nil
	}
	return u
}

// GetAnketa – GET /api/anketa?o=&t=: вопросы, имя и уже сохранённые ответы.
func (a *App) GetAnketa(w http.ResponseWriter, r *http.Request) {
	u := a.anketaUser(r)
	if u == nil {
		writeErr(w, 400, "invalid_link")
		return
	}
	name := ""
	if f := strings.Fields(u.Name); len(f) > 0 {
		name = f[0]
	}
	answers := map[string]string{}
	submitted := false
	saved, err := a.Repo.GetQuestionnaire(r.Context(), u.ID)
	if err != nil {
		saved = nil
	}
	kind, form := anketaFormFor(anketaKind(r, saved))
	if saved != nil && saved.Kind == kind {
		answers, submitted = saved.Answers, true
	}
	writeJSON(w, 200, map[string]any{
		"name": name, "kind": kind, "form": form, "questions": form.Questions, "answers": answers, "submitted": submitted,
	})
}

// SubmitAnketa – POST /api/anketa?o=&t= {answers, consent_health}.
func (a *App) SubmitAnketa(w http.ResponseWriter, r *http.Request) {
	u := a.anketaUser(r)
	if u == nil {
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
	saved, err := a.Repo.GetQuestionnaire(r.Context(), u.ID)
	if err != nil {
		saved = nil
	}
	kind, form := anketaFormFor(anketaKind(r, saved))
	clean := map[string]string{}
	var missing []string
	for _, q := range form.Questions {
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
	if err := a.Repo.SaveQuestionnaire(r.Context(), u.ID, kind, clean); err != nil {
		slog.Warn("anketa save", "err", err)
		writeErr(w, 500, "db")
		return
	}
	a.notifyAnketa(u, kind, clean, saved != nil && saved.Kind == kind)
	writeJSON(w, 200, map[string]any{"ok": 1})
}

// notifyAnketa – ответы анкеты Алексею на почту.
func (a *App) notifyAnketa(u *db.User, kind string, answers map[string]string, updated bool) {
	_, form := anketaFormFor(kind)
	subject := form.Subject + ": " + u.Name
	if updated {
		subject = form.Subject + " обновлена: " + u.Name
	}
	var sb strings.Builder
	sb.WriteString(`<div style="font-family:-apple-system,Segoe UI,Roboto,Arial,sans-serif;font-size:15px;line-height:1.55;max-width:640px;">`)
	sb.WriteString(`<p><b>` + html.EscapeString(u.Name) + `</b> &lt;` + html.EscapeString(u.Email) + `&gt;</p>`)
	group := ""
	for _, q := range form.Questions {
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
	a.notifyAdmin("📋 ", subject, sb.String())
}

// sendAnketaEmail – письмо человеку со ссылкой на анкету (отправка напрямую из админки).
func (a *App) sendAnketaEmail(u *db.User) error {
	greeting := "Здравствуйте!"
	if f := strings.Fields(u.Name); len(f) > 0 {
		greeting = "Здравствуйте, " + html.EscapeString(f[0]) + "!"
	}
	link := a.anketaURL(u.ID)
	return a.Mail.Send(u.Email, "Анкета перед занятием",
		"<p>"+greeting+" Это Алексей Ларин.</p>"+
			"<p>Перед нашим занятием заполните, пожалуйста, короткую анкету – 5–7 минут: цель, что беспокоит, "+
			"опыт и удобное время. Так я не буду тратить занятие на расспросы и сразу соберу план под вас.</p>"+
			`<p><a href="`+link+`" style="display:inline-block;background:#e8652a;color:#fff;text-decoration:none;`+
			`padding:12px 24px;border-radius:100px;font-weight:600">Заполнить анкету&nbsp;→</a></p>`+
			"<p>Если что-то непонятно – просто ответьте на это письмо или напишите в "+
			`<a href="https://t.me/larin_lesha">Телеграм</a>.</p>`+
			"<p>Алексей Ларин</p>")
}

// AdminAnketa – POST /api/admin/anketa {user_id? | email + name?, send?}.
// Ссылка на анкету для любого человека: по id из карточки или по email
// (если человека ещё нет – заводим «пустой» аккаунт, как при оплате услуги).
// send=true – ещё и письмо со ссылкой.
func (a *App) AdminAnketa(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID string `json:"user_id"`
		Email  string `json:"email"`
		Name   string `json:"name"`
		Send   bool   `json:"send"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	var uid uuid.UUID
	if in.UserID != "" {
		id, err := uuid.Parse(in.UserID)
		if err != nil {
			writeErr(w, 400, "bad_id")
			return
		}
		uid = id
	} else {
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if !strings.Contains(email, "@") || len(email) < 5 || len(email) > 254 {
			writeErr(w, 400, "bad_email")
			return
		}
		id, err := a.findOrCreateUser(r.Context(), email, strings.TrimSpace(in.Name))
		if err != nil {
			writeErr(w, 500, "db")
			return
		}
		uid = id
	}
	u, err := a.Repo.GetUser(r.Context(), uid)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	if in.Send {
		if err := a.sendAnketaEmail(u); err != nil {
			slog.Warn("anketa email", "to", u.Email, "err", err)
			writeErr(w, 500, "smtp")
			return
		}
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "anketa_link", "user", &u.ID, map[string]any{"sent": in.Send})
	writeJSON(w, 200, map[string]any{"url": a.anketaURL(u.ID), "user_id": u.ID, "email": u.Email, "sent": in.Send})
}
