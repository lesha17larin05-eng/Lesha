package handlers

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/auth"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/middleware"
)

// Ссылка «Открыть курс» из письма после регистрации на бесплатный курс.
// Одноразовая: подтверждает почту и логинит.
const freeAccessLinkTTL = 7 * 24 * time.Hour

// Сколько после регистрации можно исправить адрес почты (пока он не подтверждён).
const fixEmailWindow = 48 * time.Hour

// grantFreeCourses выдаёт доступ ко всем опубликованным бесплатным курсам.
// Идемпотентно (Repo.Grant – ON CONFLICT DO NOTHING). Возвращает число выдач.
func (a *App) grantFreeCourses(ctx context.Context, uid uuid.UUID) int {
	n := 0
	courses, err := a.Repo.ListCourses(ctx, true)
	if err != nil {
		return 0
	}
	for _, c := range courses {
		if c.Kind != "free" {
			continue
		}
		if err := a.Repo.Grant(ctx, uid, c.ID, "free", nil); err == nil {
			n++
		}
	}
	return n
}

// startSession выдаёт access/refresh-куки – человек сразу залогинен.
func (a *App) startSession(w http.ResponseWriter, r *http.Request, uid uuid.UUID, role string) error {
	access, err := auth.IssueAccessToken(a.Cfg.JWTSecret, uid, role)
	if err != nil {
		return err
	}
	refreshRaw, refreshHash, _ := auth.RandomToken(32)
	if err := a.Repo.CreateSession(r.Context(), uid, refreshHash, r.UserAgent(), middleware.ClientIP(r), auth.RefreshTTL); err != nil {
		return err
	}
	setAuthCookies(w, r, access, refreshRaw)
	return nil
}

// sendFreeAccessEmail – письмо «доступ открыт»: кнопка в кабинет (она же
// подтверждает почту) и данные для входа. Возвращает ссылку (для dev/тестов).
func (a *App) sendFreeAccessEmail(ctx context.Context, uid uuid.UUID, to, password string) string {
	rawTok, hashTok, _ := auth.RandomToken(32)
	_ = a.Repo.CreateEmailToken(ctx, uid, hashTok, freeAccessLinkTTL)
	link := a.Cfg.AppHost + "/auth/verify?token=" + rawTok
	a.Mail.Async(to, "Доступ к курсу «Мягкий старт» открыт",
		"<p>Здравствуйте! Это Алексей Ларин.</p>"+
			"<p>Доступ к курсу «Мягкий старт» открыт – все 8 уроков уже в вашем личном кабинете. "+
			"Начните с того, что сейчас важнее всего.</p>"+
			`<p><a href="`+link+`" style="display:inline-block;background:#e8652a;color:#fff;`+
			`text-decoration:none;padding:12px 24px;border-radius:100px;font-weight:600">Открыть курс →</a></p>`+
			"<p style=\"color:#777;font-size:13px\">Кнопка сразу входит в кабинет и подтверждает вашу почту. Действует 7 дней.</p>"+
			"<hr>"+
			"<p><b>Данные для входа в личный кабинет:</b><br>"+
			"Сайт: leshalarin.ru<br>"+
			"Логин: "+html.EscapeString(to)+"<br>"+
			"Пароль: "+password+"</p>"+
			"<p>Сохраните это письмо – пригодится, чтобы вернуться к урокам.</p>")
	return link
}

// FixEmail – POST /api/auth/fix-email {"email": "..."}.
// Исправление опечатки в адресе сразу после регистрации: только для
// залогиненного, только пока почта не подтверждена и не позже 48 часов после
// регистрации. Генерирует новый пароль и шлёт письмо на новый адрес.
func (a *App) FixEmail(w http.ResponseWriter, r *http.Request) {
	uid, ok := middleware.UserID(r.Context())
	if !ok {
		writeErr(w, 401, "unauthorized")
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	if !strings.Contains(in.Email, "@") || len(in.Email) < 5 || len(in.Email) > 254 {
		writeErr(w, 400, "invalid_input")
		return
	}
	u, err := a.Repo.GetUser(r.Context(), uid)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	if u.EmailVerifiedAt != nil || time.Since(u.CreatedAt) > fixEmailWindow {
		writeErr(w, 409, "email_locked")
		return
	}
	if in.Email == u.Email {
		writeJSON(w, 200, map[string]any{"ok": true, "email": u.Email})
		return
	}
	if _, err := a.Repo.GetUserByEmail(r.Context(), in.Email); err == nil {
		writeErr(w, 409, "email_taken")
		return
	}
	rawPwd, _, err := auth.RandomToken(9)
	if err != nil {
		writeErr(w, 500, "token_failed")
		return
	}
	password := rawPwd[:14]
	hash, err := auth.HashPassword(password)
	if err != nil {
		writeErr(w, 500, "hash_failed")
		return
	}
	if err := a.Repo.FixUnverifiedEmail(r.Context(), uid, in.Email, hash); err != nil {
		if err == db.ErrNotFound {
			writeErr(w, 409, "email_locked")
			return
		}
		writeErr(w, 409, "email_taken")
		return
	}
	link := a.sendFreeAccessEmail(r.Context(), uid, in.Email, password)
	resp := map[string]any{"ok": true, "email": in.Email}
	if a.Cfg.AppEnv != "production" {
		resp["password_dev"] = password
		resp["verify_link_dev"] = link
	}
	writeJSON(w, 200, resp)
}
