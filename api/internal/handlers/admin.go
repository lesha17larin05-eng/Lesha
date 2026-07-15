package handlers

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/auth"
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
	// Фильтры: ?course=<slug> — только с доступом к курсу;
	// ?verified=1|0 — по статусу подтверждения email; ?sort=last_seen.
	course := r.URL.Query().Get("course")
	var verified *bool
	switch r.URL.Query().Get("verified") {
	case "1", "true":
		v := true
		verified = &v
	case "0", "false":
		v := false
		verified = &v
	}
	sort := r.URL.Query().Get("sort")
	limit := 50
	users, err := a.Repo.ListUsers(r.Context(), q, course, verified, sort, limit, (page-1)*limit)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	total, _ := a.Repo.CountUsers(r.Context(), q, course, verified)
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]any{
			"id": u.ID, "email": u.Email, "name": u.Name, "phone": u.Phone, "role": u.Role,
			"email_verified": u.EmailVerifiedAt != nil, "last_seen_at": u.LastSeenAt, "created_at": u.CreatedAt,
		})
	}
	writeJSON(w, 200, map[string]any{
		"users": out, "total": total, "page": page, "per_page": limit,
	})
}

// AdminUser — полная карточка пользователя: профиль + согласия (152-ФЗ) +
// доступы с прогрессом и поурочной детализацией + заказы.
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
	pdAt, mktAt, _ := a.Repo.UserConsents(r.Context(), id)

	enrolls, _ := a.Repo.UserEnrollmentsInfo(r.Context(), id)
	courses := make([]map[string]any, 0, len(enrolls))
	for _, e := range enrolls {
		total, done, _ := a.Repo.CourseProgress(r.Context(), id, e.CourseID)
		lessons, _ := a.Repo.UserCourseLessons(r.Context(), id, e.CourseID)
		// последняя активность по курсу — самый свежий updated_at из уроков
		var lastActivity *time.Time
		for _, l := range lessons {
			if l.UpdatedAt != nil && (lastActivity == nil || l.UpdatedAt.After(*lastActivity)) {
				lastActivity = l.UpdatedAt
			}
		}
		pct := 0
		if total > 0 {
			pct = done * 100 / total
		}
		courses = append(courses, map[string]any{
			"course_id": e.CourseID, "slug": e.Slug, "title": e.Title, "kind": e.Kind,
			"granted_by": e.GrantedBy, "granted_at": e.GrantedAt,
			"lessons_total": total, "lessons_done": done, "progress_pct": pct,
			"last_activity_at": lastActivity,
			"lessons":          lessons,
		})
	}

	orders, _ := a.Repo.OrdersByUser(r.Context(), id)
	if orders == nil {
		orders = []db.UserOrderInfo{}
	}

	writeJSON(w, 200, map[string]any{
		"user": map[string]any{
			"id": u.ID, "email": u.Email, "name": u.Name, "phone": u.Phone, "role": u.Role,
			"email_verified_at": u.EmailVerifiedAt, "created_at": u.CreatedAt, "last_seen_at": u.LastSeenAt,
			"consent_pd_at": pdAt, "consent_marketing_at": mktAt,
		},
		"courses": courses,
		"orders":  orders,
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
	// Берём оплаты с JOIN на курсы и пользователей, чтобы в админке сразу
	// были видны человекочитаемые названия (а не UUID).
	q := `
SELECT o.id, o.order_num, o.amount_rub, o.status, o.created_at,
       o.course_id, COALESCE(c.title,''), COALESCE(c.slug,''),
       o.user_id, COALESCE(u.email,''), COALESCE(u.name,'')
FROM orders o
LEFT JOIN courses c ON c.id = o.course_id
LEFT JOIN users u ON u.id = o.user_id
`
	args := []any{}
	if status != "" {
		q += " WHERE o.status = $1"
		args = append(args, status)
	}
	q += " ORDER BY o.created_at DESC LIMIT 200"
	rows, err := a.Repo.Pool.Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, courseID, userID uuid.UUID
		var orderNum int64
		var amount int
		var status, courseTitle, courseSlug, userEmail, userName string
		var createdAt time.Time
		if err := rows.Scan(&id, &orderNum, &amount, &status, &createdAt,
			&courseID, &courseTitle, &courseSlug,
			&userID, &userEmail, &userName); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id":           id,
			"order_num":    orderNum,
			"amount_rub":   amount,
			"status":       status,
			"created_at":   createdAt,
			"course_id":    courseID,
			"course_title": courseTitle,
			"course_slug":  courseSlug,
			"user_id":      userID,
			"user_email":   userEmail,
			"user_name":    userName,
		})
	}
	writeJSON(w, 200, out)
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

// grantByEmailReq — тело запроса для массовой выдачи доступа по email.
// Поддерживает 1+ email; для каждого создаётся юзер (если его нет),
// выдаётся enrollment в курс, отправляется письмо с reset-ссылкой
// (для новых) или с уведомлением о доступе (для существующих).
type grantByEmailReq struct {
	Emails []string `json:"emails"`
}

type grantByEmailResult struct {
	Email     string `json:"email"`
	Status    string `json:"status"`               // ok | already_enrolled | invalid_email | error
	IsNewUser bool   `json:"is_new_user"`          // создали нового юзера
	InviteURL string `json:"invite_url,omitempty"` // для новых — ссылка установки пароля
	Error     string `json:"error,omitempty"`
}

// AdminGrantByEmail — массовая выдача доступа к курсу по списку email.
// Для каждого email: создаёт юзера если не было, выдаёт enrollment в курс,
// шлёт приветственное письмо. Идемпотентно: повторный вызов не сломает уже
// выданный доступ. Возвращает per-email статус, чтобы UI показал результат.
func (a *App) AdminGrantByEmail(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	c, err := a.Repo.GetCourseBySlug(r.Context(), slug)
	if err != nil {
		writeErr(w, 404, "course_not_found")
		return
	}
	var in grantByEmailReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	if len(in.Emails) == 0 {
		writeErr(w, 400, "no_emails")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	out := make([]grantByEmailResult, 0, len(in.Emails))
	for _, raw := range in.Emails {
		email := strings.TrimSpace(strings.ToLower(raw))
		res := grantByEmailResult{Email: email}
		if !strings.Contains(email, "@") || len(email) < 5 {
			res.Status = "invalid_email"
			out = append(out, res)
			continue
		}

		// Найдём юзера или создадим нового
		u, err := a.Repo.GetUserByEmail(r.Context(), email)
		var uid uuid.UUID
		// needsInvite — пароль не установлен, надо прислать reset-ссылку
		// (актуально и для новых, и для тех, кого создали ранее через SQL).
		needsInvite := false
		if errors.Is(err, db.ErrNotFound) {
			// Создаём с unusable-паролем — реальный пароль юзер установит по reset-ссылке.
			// Согласие на ПД считаем выставленным админом от имени пользователя — без
			// этого по 152-ФЗ хранение PII запрещено. Это решение Алексея как контролёра данных.
			uid, err = a.Repo.CreateUser(r.Context(), email, "!unusable!"+uuid.New().String(), "", "user")
			if err != nil {
				res.Status = "error"
				res.Error = "create_user_failed"
				out = append(out, res)
				continue
			}
			// email_verified=true, потому что доступ выдаёт админ — email доверенный.
			_ = a.Repo.MarkEmailVerified(r.Context(), uid)
			_ = a.Repo.SaveConsent(r.Context(), uid, true, false)
			res.IsNewUser = true
			needsInvite = true
		} else if err != nil {
			res.Status = "error"
			res.Error = "lookup_failed"
			out = append(out, res)
			continue
		} else {
			uid = u.ID
			// Если у уже существующего юзера unusable-пароль — он ещё не активировал
			// аккаунт, ему тоже нужно прислать reset-ссылку, иначе войти не сможет.
			if strings.HasPrefix(u.PasswordHash, "!unusable!") {
				needsInvite = true
			}
		}

		// Выдаём enrollment (идемпотентно, ON CONFLICT DO NOTHING).
		// Проверим: была ли запись раньше — для статуса в ответе.
		existed, _ := a.Repo.HasEnrollment(r.Context(), uid, c.ID)
		if err := a.Repo.Grant(r.Context(), uid, c.ID, "admin", &adminID); err != nil {
			res.Status = "error"
			res.Error = "grant_failed"
			out = append(out, res)
			continue
		}
		a.Repo.Audit(r.Context(), adminID, "grant_by_email", "enrollment", &uid, map[string]any{
			"course_id": c.ID, "course_slug": c.Slug, "email": email,
		})
		if existed {
			res.Status = "already_enrolled"
		} else {
			res.Status = "ok"
		}

		// Письмо отправляем ВСЕГДА — пользователь должен узнать о доступе.
		// Если пароль не установлен (новый юзер или ранее созданный через SQL/массовый
		// импорт) — добавляем reset-ссылку, иначе только уведомление.
		var body string
		subject := "Доступ к курсу «" + c.Title + "» открыт"
		if needsInvite {
			raw, hash, _ := auth.RandomToken(32)
			_ = a.Repo.CreatePasswordResetToken(r.Context(), uid, hash, 7*24*time.Hour)
			inviteURL := a.Cfg.AppHost + "/auth/reset?token=" + raw
			res.InviteURL = inviteURL
			body = "<p>Здравствуйте!</p>" +
				"<p>Алексей Ларин открыл вам доступ к курсу <b>«" + c.Title + "»</b> на сайте leshalarin.ru.</p>" +
				"<p>Установите пароль по ссылке (живёт 7 дней):<br><a href=\"" + inviteURL + "\">" + inviteURL + "</a></p>" +
				"<p>Логин — этот email. После установки пароля курс будет доступен в кабинете: " +
				"<a href=\"" + a.Cfg.AppHost + "/cabinet/courses\">" + a.Cfg.AppHost + "/cabinet/courses</a></p>"
		} else {
			body = "<p>Здравствуйте!</p>" +
				"<p>Алексей Ларин открыл вам доступ к курсу <b>«" + c.Title + "»</b>.</p>" +
				"<p>Курс уже доступен в вашем кабинете: " +
				"<a href=\"" + a.Cfg.AppHost + "/cabinet/courses\">" + a.Cfg.AppHost + "/cabinet/courses</a></p>"
		}
		a.Mail.Async(email, subject, body)
		out = append(out, res)
	}
	writeJSON(w, 200, map[string]any{"results": out, "course": map[string]any{
		"slug": c.Slug, "title": c.Title,
	}})
}

// AdminUsersExport — CSV-выгрузка подписчиков для сервиса рассылок.
// Только пользователи с согласием на рассылку; фильтры как у списка.
// UTF-8 BOM — чтобы Excel корректно открывал кириллицу.
func (a *App) AdminUsersExport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("search")
	course := r.URL.Query().Get("course")
	var verified *bool
	switch r.URL.Query().Get("verified") {
	case "1", "true":
		v := true
		verified = &v
	case "0", "false":
		v := false
		verified = &v
	}
	rows, err := a.Repo.ListUsersForExport(r.Context(), q, course, verified)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "users_export_csv", "user", nil,
		map[string]any{"count": len(rows), "course": course, "verified": r.URL.Query().Get("verified")})

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="subscribers.csv"`)
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM для Excel
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"email", "name", "phone", "registered_at", "courses"})
	for _, u := range rows {
		_ = cw.Write([]string{u.Email, u.Name, u.Phone, u.CreatedAt.Format("2006-01-02"), u.Courses})
	}
	cw.Flush()
}

// AdminActivity — журнал занятий: кто какой урок смотрел/прошёл и когда.
func (a *App) AdminActivity(w http.ResponseWriter, r *http.Request) {
	course := r.URL.Query().Get("course")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 500 {
		limit = 200
	}
	rows, err := a.Repo.ListLessonActivity(r.Context(), course, limit)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	if rows == nil {
		rows = []db.ActivityRow{}
	}
	writeJSON(w, 200, rows)
}
