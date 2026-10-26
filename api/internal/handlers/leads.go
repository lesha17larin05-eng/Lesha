package handlers

import (
	"encoding/csv"
	"encoding/json"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/middleware"
)

// Заявки с маркетинговых страниц (/coaching, /start).
// Публичный POST /api/leads (CSRF обязателен, rate-limit в роутере) +
// админские GET /api/admin/leads и PATCH /api/admin/leads/{id}.

type leadReq struct {
	Name      string `json:"name"`
	Contact   string `json:"contact"`
	Message   string `json:"message"`
	Source    string `json:"source"`
	ConsentPD bool   `json:"consent_pd"`
}

// consultation остаётся в списке: страница закрыта и редиректит на /start,
// но старые ссылки и закладки могут прислать этот источник.
var leadSources = map[string]bool{"coaching": true, "start": true, "consultation": true, "gruppa": true, "personal": true}

func (a *App) CreateLead(w http.ResponseWriter, r *http.Request) {
	var in leadReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Contact = strings.TrimSpace(in.Contact)
	in.Message = strings.TrimSpace(in.Message)
	if in.Name == "" || len(in.Contact) < 5 {
		writeErr(w, 400, "invalid_input")
		return
	}
	if !in.ConsentPD {
		writeErr(w, 400, "consent_pd_required")
		return
	}
	if len(in.Name) > 200 || len(in.Contact) > 200 || len(in.Message) > 3000 {
		writeErr(w, 400, "too_long")
		return
	}
	if !leadSources[in.Source] {
		in.Source = "other"
	}
	id, err := a.Repo.CreateLead(r.Context(), in.Name, in.Contact, in.Message, in.Source, true)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	// Уведомление Алексею. html.EscapeString – пользовательский ввод в письме.
	sourceTitle := map[string]string{
		"coaching":     "Личное ведение",
		"start":        "Точка перемен",
		"gruppa":       "Групповое ведение",
		"personal":     "Персональные тренировки",
		"consultation": "Консультация (старая страница)",
		"other":        "Сайт",
	}[in.Source]
	a.notifyAdmin("📩 ", "Новая заявка: "+sourceTitle,
		"<p><b>Имя:</b> "+html.EscapeString(in.Name)+"</p>"+
			"<p><b>Контакт:</b> "+html.EscapeString(in.Contact)+"</p>"+
			"<p><b>Откуда:</b> "+sourceTitle+"</p>"+
			(func() string {
				if in.Message == "" {
					return ""
				}
				return "<p><b>Сообщение:</b><br>" + html.EscapeString(in.Message) + "</p>"
			})()+
			(func() string {
				// «Точка перемен» и группа: сначала переписка, потом Алексей пересылает
				// ссылку на оплату – она открывает скрытый блок оплаты на странице.
				page := map[string]string{"start": "/start", "gruppa": "/gruppa"}[in.Source]
				if page == "" {
					return ""
				}
				return "<p><b>Ссылка на оплату для человека:</b> " + a.Cfg.AppHost + page + "#oplata</p>"
			})()+
			"<p>Все люди: <a href=\""+a.Cfg.AppHost+"/admin/leads\">"+a.Cfg.AppHost+"/admin/leads</a></p>")
	writeJSON(w, 201, map[string]any{"ok": 1, "id": id})
}

func (a *App) AdminLeads(w http.ResponseWriter, r *http.Request) {
	leads, err := a.Repo.ListLeads(r.Context(), 3000)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	if leads == nil {
		leads = []db.Lead{}
	}
	writeJSON(w, 200, leads)
}

// Справочники «Людей». Источники ручных записей – где человек написал;
// источники с сайта (coaching, gruppa, …) тоже допустимы при правке.
var (
	leadStatuses  = map[string]bool{"new": true, "in_progress": true, "bought": true, "later": true, "no": true, "done": true}
	leadInterests = map[string]bool{"": true, "gruppa": true, "personal": true, "coaching": true, "course": true, "question": true, "start": true, "other": true}
	manualSources = map[string]bool{"telegram": true, "max": true, "instagram": true, "vk": true, "offline": true, "referral": true, "manual": true}
	reLeadDate    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func validLeadSource(s string) bool { return manualSources[s] || leadSources[s] || s == "other" }

// validLeadDate – "" (очистить) или реальная дата YYYY-MM-DD.
func validLeadDate(s string) bool {
	if s == "" {
		return true
	}
	if !reLeadDate.MatchString(s) {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

type leadAdminReq struct {
	Name     *string `json:"name"`
	Contact  *string `json:"contact"`
	Source   *string `json:"source"`
	Interest *string `json:"interest"`
	Note     *string `json:"note"`
	NextStep *string `json:"next_step"`
	NextAt   *string `json:"next_at"`
	Status   *string `json:"status"`
}

func trimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	s := strings.TrimSpace(*p)
	return &s
}

// validate – общая проверка полей; возвращает код ошибки или "".
func (in *leadAdminReq) validate(create bool) string {
	in.Name, in.Contact, in.Source, in.Interest = trimPtr(in.Name), trimPtr(in.Contact), trimPtr(in.Source), trimPtr(in.Interest)
	in.Note, in.NextStep, in.NextAt, in.Status = trimPtr(in.Note), trimPtr(in.NextStep), trimPtr(in.NextAt), trimPtr(in.Status)
	if create && (in.Name == nil || *in.Name == "") {
		return "name_required"
	}
	if in.Name != nil && *in.Name == "" {
		return "name_required"
	}
	for _, f := range []*string{in.Name, in.Contact, in.Source, in.Interest, in.NextStep} {
		if f != nil && len(*f) > 300 {
			return "too_long"
		}
	}
	if in.Note != nil && len(*in.Note) > 5000 {
		return "too_long"
	}
	if in.Status != nil && !leadStatuses[*in.Status] {
		return "bad_status"
	}
	if in.Interest != nil && !leadInterests[*in.Interest] {
		return "bad_interest"
	}
	if in.Source != nil && !validLeadSource(*in.Source) {
		return "bad_source"
	}
	if in.NextAt != nil && !validLeadDate(*in.NextAt) {
		return "bad_date"
	}
	return ""
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// AdminCreateLead – POST /api/admin/leads: человек, написавший напрямую
// (Telegram, Max, лично…). Тот же контакт уже есть → 409 duplicate + id.
func (a *App) AdminCreateLead(w http.ResponseWriter, r *http.Request) {
	var in leadAdminReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	if code := in.validate(true); code != "" {
		writeErr(w, 400, code)
		return
	}
	if in.Source == nil || *in.Source == "" {
		s := "manual"
		in.Source = &s
	}
	if c := str(in.Contact); c != "" {
		if ex, err := a.Repo.FindLeadByContact(r.Context(), c); err == nil {
			writeJSON(w, 409, map[string]any{"error": "duplicate", "id": ex.ID, "name": ex.Name})
			return
		}
	}
	id, err := a.Repo.CreateLeadManual(r.Context(), db.LeadInput{
		Name: str(in.Name), Contact: str(in.Contact), Source: str(in.Source), Interest: str(in.Interest),
		Note: str(in.Note), NextStep: str(in.NextStep), Status: str(in.Status), NextAt: in.NextAt,
	})
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "lead_create", "lead", &id, map[string]any{"source": str(in.Source)})
	writeJSON(w, 201, map[string]any{"ok": 1, "id": id})
}

// reBulkSep – разделитель «имя – контакт» в строке массового добавления.
var (
	reBulkSep    = regexp.MustCompile(`\s+[–—-]\s+|\t|;|,`)
	rePhoneOnly  = regexp.MustCompile(`^[\d\s()-]{7,}$`)
	reBulkMarker = regexp.MustCompile(`^\s*(?:[•*·–—-]|\d{1,3}[.)])\s+`)
)

// parseBulkLine – «Имя – @контакт» → (имя, контакт). Без разделителя:
// если строка похожа на контакт (@, цифры, почта) – это контакт, иначе имя.
func parseBulkLine(line string) (name, contact string) {
	line = strings.TrimSpace(reBulkMarker.ReplaceAllString(line, ""))
	if line == "" {
		return "", ""
	}
	if loc := reBulkSep.FindStringIndex(line); loc != nil {
		return strings.TrimSpace(line[:loc[0]]), strings.TrimSpace(line[loc[1]:])
	}
	if strings.Contains(line, "@") || strings.HasPrefix(line, "+") || rePhoneOnly.MatchString(line) {
		return line, line
	}
	return line, ""
}

// AdminBulkLeads – POST /api/admin/leads/bulk {text, source, interest}:
// по строке на человека. Пустые строки и уже известные контакты пропускаем.
func (a *App) AdminBulkLeads(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text     string `json:"text"`
		Source   string `json:"source"`
		Interest string `json:"interest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	if in.Source == "" {
		in.Source = "manual"
	}
	if !validLeadSource(in.Source) {
		writeErr(w, 400, "bad_source")
		return
	}
	if !leadInterests[in.Interest] {
		writeErr(w, 400, "bad_interest")
		return
	}
	if len(in.Text) > 100000 {
		writeErr(w, 400, "too_long")
		return
	}
	added, skipped := 0, []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(in.Text, "\n") {
		name, contact := parseBulkLine(line)
		if name == "" {
			continue
		}
		if len(name) > 300 || len(contact) > 300 {
			skipped = append(skipped, name)
			continue
		}
		key := strings.ToLower(contact)
		if contact != "" {
			if seen[key] {
				skipped = append(skipped, name)
				continue
			}
			if _, err := a.Repo.FindLeadByContact(r.Context(), contact); err == nil {
				skipped = append(skipped, name)
				continue
			}
			seen[key] = true
		}
		if _, err := a.Repo.CreateLeadManual(r.Context(), db.LeadInput{
			Name: name, Contact: contact, Source: in.Source, Interest: in.Interest,
		}); err != nil {
			writeErr(w, 500, "db")
			return
		}
		added++
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "lead_bulk", "lead", nil, map[string]any{"added": added, "skipped": len(skipped)})
	writeJSON(w, 200, map[string]any{"added": added, "skipped": skipped})
}

// AdminUpdateLead – PATCH /api/admin/leads/{id}: любые поля, переданные в теле.
func (a *App) AdminUpdateLead(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	var in leadAdminReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_status")
		return
	}
	if code := in.validate(false); code != "" {
		writeErr(w, 400, code)
		return
	}
	p := db.LeadPatch{Name: in.Name, Contact: in.Contact, Source: in.Source, Interest: in.Interest,
		Note: in.Note, NextStep: in.NextStep, NextAt: in.NextAt, Status: in.Status}
	if err := a.Repo.UpdateLead(r.Context(), id, p); err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	if in.Status != nil {
		a.Repo.Audit(r.Context(), adminID, "lead_status", "lead", &id, map[string]any{"status": *in.Status})
	} else {
		a.Repo.Audit(r.Context(), adminID, "lead_update", "lead", &id, nil)
	}
	l, _ := a.Repo.GetLead(r.Context(), id)
	writeJSON(w, 200, map[string]any{"ok": 1, "lead": l})
}

// AdminLeadsCSV – GET /api/admin/leads.csv: выгрузка для Excel (UTF-8 с BOM, «;»).
func (a *App) AdminLeadsCSV(w http.ResponseWriter, r *http.Request) {
	leads, err := a.Repo.ListLeads(r.Context(), 100000)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="lyudi.csv"`)
	_, _ = w.Write([]byte("\xEF\xBB\xBF"))
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	_ = cw.Write([]string{"Добавлен", "Имя", "Контакт", "Откуда", "Интерес", "Статус", "Следующий шаг", "Дата шага", "Заметка", "Сообщение с сайта"})
	for _, l := range leads {
		_ = cw.Write([]string{l.CreatedAt.Format("2006-01-02"), l.Name, l.Contact, l.Source, l.Interest, l.Status,
			l.NextStep, str(l.NextAt), l.Note, l.Message})
	}
	cw.Flush()
}
