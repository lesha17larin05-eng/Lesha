package handlers

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/middleware"
)

// Заявки с маркетинговых страниц (/coaching, /consultation).
// Публичный POST /api/leads (CSRF обязателен, rate-limit в роутере) +
// админские GET /api/admin/leads и PATCH /api/admin/leads/{id}.

type leadReq struct {
	Name      string `json:"name"`
	Contact   string `json:"contact"`
	Message   string `json:"message"`
	Source    string `json:"source"`
	ConsentPD bool   `json:"consent_pd"`
}

var leadSources = map[string]bool{"coaching": true, "consultation": true}

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
	// Уведомление Алексею. html.EscapeString — пользовательский ввод в письме.
	sourceTitle := map[string]string{
		"coaching":     "Личное ведение",
		"consultation": "Консультация",
		"other":        "Сайт",
	}[in.Source]
	a.Mail.Async(a.Cfg.LeadNotifyEmail, "Новая заявка: "+sourceTitle,
		"<p><b>Имя:</b> "+html.EscapeString(in.Name)+"</p>"+
			"<p><b>Контакт:</b> "+html.EscapeString(in.Contact)+"</p>"+
			"<p><b>Откуда:</b> "+sourceTitle+"</p>"+
			(func() string {
				if in.Message == "" {
					return ""
				}
				return "<p><b>Сообщение:</b><br>" + html.EscapeString(in.Message) + "</p>"
			})()+
			"<p>Все заявки: <a href=\""+a.Cfg.AppHost+"/admin/leads\">"+a.Cfg.AppHost+"/admin/leads</a></p>")
	writeJSON(w, 201, map[string]any{"ok": 1, "id": id})
}

func (a *App) AdminLeads(w http.ResponseWriter, r *http.Request) {
	leads, err := a.Repo.ListLeads(r.Context(), 500)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	if leads == nil {
		leads = []db.Lead{}
	}
	writeJSON(w, 200, leads)
}

var leadStatuses = map[string]bool{"new": true, "in_progress": true, "done": true}

func (a *App) AdminUpdateLead(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !leadStatuses[in.Status] {
		writeErr(w, 400, "bad_status")
		return
	}
	if err := a.Repo.UpdateLeadStatus(r.Context(), id, in.Status); err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "lead_status", "lead", &id, map[string]any{"status": in.Status})
	writeJSON(w, 200, map[string]any{"ok": 1})
}
