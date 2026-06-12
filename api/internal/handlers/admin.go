package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/middleware"
)

func (a *App) AdminStats(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	day := now.Add(-24 * time.Hour)
	week := now.Add(-7 * 24 * time.Hour)
	month := now.Add(-30 * 24 * time.Hour)
	revToday, _ := a.Repo.RevenueSince(r.Context(), day)
	revWeek, _ := a.Repo.RevenueSince(r.Context(), week)
	revMonth, _ := a.Repo.RevenueSince(r.Context(), month)
	online, _ := a.Repo.CountOnlineUsers(r.Context(), 5*time.Minute)
	// total users
	var users int
	_ = a.Repo.Pool.QueryRow(r.Context(), `SELECT count(*) FROM users`).Scan(&users)
	writeJSON(w, 200, map[string]any{
		"users_total":   users,
		"online":        online,
		"revenue_day":   revToday,
		"revenue_week":  revWeek,
		"revenue_month": revMonth,
	})
}

func (a *App) AdminUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("search")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit := 50
	users, err := a.Repo.ListUsers(r.Context(), q, limit, (page-1)*limit)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]any{
			"id": u.ID, "email": u.Email, "name": u.Name, "phone": u.Phone, "role": u.Role,
			"email_verified": u.EmailVerifiedAt != nil, "last_seen_at": u.LastSeenAt, "created_at": u.CreatedAt,
		})
	}
	writeJSON(w, 200, out)
}

func (a *App) AdminUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	u, err := a.Repo.GetUser(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	enrolls, _ := a.Repo.UserEnrollments(r.Context(), id)
	orders, _ := a.Repo.ListOrders(r.Context(), "", nil, nil, 100, 0)
	// filter to this user
	myOrders := []*db.Order{}
	for _, o := range orders {
		if o.UserID == id {
			myOrders = append(myOrders, o)
		}
	}
	writeJSON(w, 200, map[string]any{
		"user":        u,
		"enrollments": enrolls,
		"orders":      myOrders,
	})
}

type grantReq struct{ CourseID uuid.UUID }

func (a *App) AdminGrant(w http.ResponseWriter, r *http.Request) {
	uid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	var in grantReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	if err := a.Repo.Grant(r.Context(), uid, in.CourseID, "admin", &adminID); err != nil {
		writeErr(w, 500, "db")
		return
	}
	a.Repo.Audit(r.Context(), adminID, "grant", "enrollment", &uid, map[string]any{"course_id": in.CourseID})
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (a *App) AdminRevoke(w http.ResponseWriter, r *http.Request) {
	uid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	cid, err := uuid.Parse(chi.URLParam(r, "course_id"))
	if err != nil {
		writeErr(w, 400, "bad_course")
		return
	}
	_ = a.Repo.Revoke(r.Context(), uid, cid)
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "revoke", "enrollment", &uid, map[string]any{"course_id": cid})
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

// AdminDeleteUser удаляет пользователя из БД вместе с зависимыми
// записями (enrollments, sessions, tokens, lesson_progress) через каскад FK.
// Защита: запрещаем удалять админа и пользователей с оплаченными заказами
// (для аудита 152-ФЗ и контроля выплат).
func (a *App) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	uid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	if uid == adminID {
		writeErr(w, 400, "cannot_delete_self")
		return
	}
	u, err := a.Repo.GetUser(r.Context(), uid)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	if u.Role == "admin" {
		writeErr(w, 403, "cannot_delete_admin")
		return
	}
	// Проверка: есть ли оплаченные заказы — таких пользователей не удаляем.
	var hasPaid bool
	if err := a.Repo.Pool.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM orders WHERE user_id=$1 AND status='paid')`, uid).Scan(&hasPaid); err == nil && hasPaid {
		writeErr(w, 409, "has_paid_orders")
		return
	}
	if _, err := a.Repo.Pool.Exec(r.Context(), `DELETE FROM users WHERE id=$1`, uid); err != nil {
		writeErr(w, 500, "db")
		return
	}
	a.Repo.Audit(r.Context(), adminID, "delete", "user", &uid, map[string]any{"email": u.Email})
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

// --- COURSES CRUD ---

// AdminGetCourse returns a course with full modules + lessons (no filtering).
func (a *App) AdminGetCourse(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	c, err := a.Repo.GetCourseByID(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	modules, _ := a.Repo.ListModules(r.Context(), c.ID)
	lessons, _ := a.Repo.ListLessons(r.Context(), c.ID)
	writeJSON(w, 200, map[string]any{"course": c, "modules": modules, "lessons": lessons})
}

type moduleInputJSON struct {
	CourseID  uuid.UUID `json:"course_id"`
	Title     string    `json:"title"`
	SortOrder int       `json:"sort_order"`
}

func (a *App) AdminCreateModule(w http.ResponseWriter, r *http.Request) {
	var in moduleInputJSON
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	if in.Title == "" {
		writeErr(w, 400, "title_required")
		return
	}
	id, err := a.Repo.CreateModule(r.Context(), in.CourseID, in.Title, in.SortOrder)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "create", "module", &id, map[string]any{"course_id": in.CourseID})
	writeJSON(w, 201, map[string]any{"id": id})
}

type moduleUpdateJSON struct {
	Title     string `json:"title"`
	SortOrder int    `json:"sort_order"`
}

func (a *App) AdminUpdateModule(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	var in moduleUpdateJSON
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	if in.Title == "" {
		writeErr(w, 400, "title_required")
		return
	}
	if err := a.Repo.UpdateModule(r.Context(), id, in.Title, in.SortOrder); err != nil {
		writeErr(w, 500, "db")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "update", "module", &id, nil)
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (a *App) AdminDeleteModule(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	if _, err := a.Repo.Pool.Exec(r.Context(), `DELETE FROM modules WHERE id=$1`, id); err != nil {
		writeErr(w, 500, "db")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "delete", "module", &id, nil)
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

// AdminGetLesson returns full lesson by id (no access filter).
func (a *App) AdminGetLesson(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	l, err := a.Repo.GetLessonByID(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	var video any
	if l.VideoID != nil {
		if v, err := a.Repo.GetVideo(r.Context(), *l.VideoID); err == nil {
			video = v
		}
	}
	writeJSON(w, 200, map[string]any{"lesson": l, "video": video})
}

func (a *App) AdminListCourses(w http.ResponseWriter, r *http.Request) {
	cs, err := a.Repo.ListCourses(r.Context(), false)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	writeJSON(w, 200, cs)
}

type courseInputJSON struct {
	Slug, Title, Subtitle, Description, CoverImageURL, Kind string
	PriceRub                                                *int  `json:"price_rub"`
	IsPublished                                             bool  `json:"is_published"`
	SortOrder                                               int   `json:"sort_order"`
}

func (a *App) AdminCreateCourse(w http.ResponseWriter, r *http.Request) {
	var in courseInputJSON
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	if in.Kind != "free" && in.Kind != "paid" {
		writeErr(w, 400, "bad_kind")
		return
	}
	id, err := a.Repo.CreateCourse(r.Context(), db.CourseInput{
		Slug: in.Slug, Title: in.Title, Subtitle: in.Subtitle, Description: in.Description,
		CoverImageURL: in.CoverImageURL, Kind: in.Kind, PriceRub: in.PriceRub,
		IsPublished: in.IsPublished, SortOrder: in.SortOrder,
	})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "create", "course", &id, nil)
	writeJSON(w, 201, map[string]any{"id": id})
}

func (a *App) AdminUpdateCourse(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	var in courseInputJSON
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	if err := a.Repo.UpdateCourse(r.Context(), id, db.CourseInput{
		Slug: in.Slug, Title: in.Title, Subtitle: in.Subtitle, Description: in.Description,
		CoverImageURL: in.CoverImageURL, Kind: in.Kind, PriceRub: in.PriceRub,
		IsPublished: in.IsPublished, SortOrder: in.SortOrder,
	}); err != nil {
		writeErr(w, 500, "db")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "update", "course", &id, nil)
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (a *App) AdminDeleteCourse(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	_ = a.Repo.DeleteCourse(r.Context(), id)
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "delete", "course", &id, nil)
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

// --- LESSONS CRUD ---

type lessonInputJSON struct {
	CourseID    uuid.UUID  `json:"course_id"`
	ModuleID    *uuid.UUID `json:"module_id"`
	Title, Slug string
	ContentMD   string `json:"content_md"`
	VideoID     *uuid.UUID `json:"video_id"`
	DurationSec int        `json:"duration_sec"`
	SortOrder   int        `json:"sort_order"`
	IsPreview   bool       `json:"is_preview"`
}

func (a *App) AdminCreateLesson(w http.ResponseWriter, r *http.Request) {
	var in lessonInputJSON
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	id, err := a.Repo.CreateLesson(r.Context(), db.LessonInput{
		CourseID: in.CourseID, ModuleID: in.ModuleID, Title: in.Title, Slug: in.Slug,
		ContentMD: in.ContentMD, VideoID: in.VideoID, DurationSec: in.DurationSec,
		SortOrder: in.SortOrder, IsPreview: in.IsPreview,
	})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (a *App) AdminUpdateLesson(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	var in lessonInputJSON
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	if err := a.Repo.UpdateLesson(r.Context(), id, db.LessonInput{
		CourseID: in.CourseID, ModuleID: in.ModuleID, Title: in.Title, Slug: in.Slug,
		ContentMD: in.ContentMD, VideoID: in.VideoID, DurationSec: in.DurationSec,
		SortOrder: in.SortOrder, IsPreview: in.IsPreview,
	}); err != nil {
		writeErr(w, 500, "db")
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (a *App) AdminDeleteLesson(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	_ = a.Repo.DeleteLesson(r.Context(), id)
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

// --- ORDERS / ONLINE / AUDIT ---

func (a *App) AdminOrders(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	orders, err := a.Repo.ListOrders(r.Context(), status, nil, nil, 200, 0)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	writeJSON(w, 200, orders)
}

func (a *App) AdminOnline(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Repo.Pool.Query(r.Context(),
		`SELECT id, email, COALESCE(name,''), last_seen_at FROM users WHERE last_seen_at > now() - interval '5 minutes' ORDER BY last_seen_at DESC`)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var email, name string
		var seen time.Time
		_ = rows.Scan(&id, &email, &name, &seen)
		out = append(out, map[string]any{"id": id, "email": email, "name": name, "last_seen_at": seen})
	}
	writeJSON(w, 200, out)
}

func (a *App) AdminAuditLog(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Repo.Pool.Query(r.Context(),
		`SELECT id, admin_id, action, target_type, target_id, meta, created_at FROM audit_log ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, adminID uuid.UUID
		var action, targetType string
		var targetID *uuid.UUID
		var meta map[string]any
		var ts time.Time
		_ = rows.Scan(&id, &adminID, &action, &targetType, &targetID, &meta, &ts)
		out = append(out, map[string]any{
			"id": id, "admin_id": adminID, "action": action, "target_type": targetType,
			"target_id": targetID, "meta": meta, "created_at": ts,
		})
	}
	writeJSON(w, 200, out)
}
