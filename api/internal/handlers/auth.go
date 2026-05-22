package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/leshalarin/api/internal/auth"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/middleware"
)

type registerReq struct {
	Email, Password, Name string
	ConsentPD             bool `json:"consent_pd"`
	ConsentMarketing      bool `json:"consent_marketing"`
}

func (a *App) Register(w http.ResponseWriter, r *http.Request) {
	var in registerReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	if !strings.Contains(in.Email, "@") || len(in.Password) < 8 {
		writeErr(w, 400, "invalid_input")
		return
	}
	// 152-ФЗ: обработка ПД невозможна без явного согласия субъекта.
	if !in.ConsentPD {
		writeErr(w, 400, "consent_pd_required")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeErr(w, 500, "hash_failed")
		return
	}
	uid, err := a.Repo.CreateUser(r.Context(), in.Email, hash, in.Name, "user")
	if err != nil {
		writeErr(w, 409, "email_taken")
		return
	}
	// Сохраняем факт согласий, чтобы можно было предъявить при жалобе/проверке.
	_ = a.Repo.SaveConsent(r.Context(), uid, true, in.ConsentMarketing)
	raw, hashTok, _ := auth.RandomToken(32)
	_ = a.Repo.CreateEmailToken(r.Context(), uid, hashTok, 24*time.Hour)
	link := a.Cfg.AppHost + "/auth/verify?token=" + raw
	a.Mail.Async(in.Email, "Подтверждение email",
		"<p>Здравствуйте! Подтвердите email: <a href=\""+link+"\">"+link+"</a></p>")
	writeJSON(w, 201, map[string]any{"id": uid, "verify_link_dev": link})
}

type quickSignupReq struct {
	Email, Name, Phone string
	ConsentPD          bool `json:"consent_pd"`
	ConsentMarketing   bool `json:"consent_marketing"`
}

// QuickSignup — одношаговая регистрация по email+имя без пароля.
// Если email уже есть → {exists:true}, фронт перенаправит на /auth/login.
// Если нет → создаём юзера, генерим пароль, шлём его на почту,
// сразу подтверждаем email и ставим auth-cookies → {created:true}, фронт ведёт в кабинет.
func (a *App) QuickSignup(w http.ResponseWriter, r *http.Request) {
	var in quickSignupReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	if !strings.Contains(in.Email, "@") || len(in.Email) < 5 {
		writeErr(w, 400, "invalid_input")
		return
	}
	if !in.ConsentPD {
		writeErr(w, 400, "consent_pd_required")
		return
	}
	if _, err := a.Repo.GetUserByEmail(r.Context(), in.Email); err == nil {
		writeJSON(w, 200, map[string]any{"exists": true, "email": in.Email})
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
	uid, err := a.Repo.CreateUser(r.Context(), in.Email, hash, in.Name, "user")
	if err != nil {
		writeErr(w, 409, "email_taken")
		return
	}
	if err := a.Repo.MarkEmailVerified(r.Context(), uid); err != nil {
		writeErr(w, 500, "db")
		return
	}
	// 152-ФЗ: сохраняем факт согласий (consent_pd обязательное, marketing — опциональное).
	_ = a.Repo.SaveConsent(r.Context(), uid, true, in.ConsentMarketing)
	// Phone — опциональное поле, ошибку логируем, но не валим регистрацию.
	if in.Phone != "" {
		_ = a.Repo.SetUserPhone(r.Context(), uid, in.Phone)
	}
	enrolled := 0
	if courses, err := a.Repo.ListCourses(r.Context(), true); err == nil {
		for _, c := range courses {
			if c.Kind != "free" {
				continue
			}
			if err := a.Repo.Grant(r.Context(), uid, c.ID, "free", nil); err == nil {
				enrolled++
			}
		}
	}
	a.Mail.Async(in.Email, "Доступ в личный кабинет",
		"<p>Здравствуйте! Ваш аккаунт создан.</p>"+
			"<p><b>Логин:</b> "+in.Email+"<br><b>Пароль:</b> "+password+"</p>"+
			"<p>Вход: <a href=\""+a.Cfg.AppHost+"/auth/login\">"+a.Cfg.AppHost+"/auth/login</a></p>")
	access, err := auth.IssueAccessToken(a.Cfg.JWTSecret, uid, "user")
	if err != nil {
		writeErr(w, 500, "token_failed")
		return
	}
	refreshRaw, refreshHash, _ := auth.RandomToken(32)
	if err := a.Repo.CreateSession(r.Context(), uid, refreshHash, r.UserAgent(), middleware.ClientIP(r), auth.RefreshTTL); err != nil {
		writeErr(w, 500, "session_failed")
		return
	}
	setAuthCookies(w, r, access, refreshRaw)
	resp := map[string]any{"created": true, "id": uid, "email": in.Email, "enrolled_free": enrolled}
	if a.Cfg.AppEnv != "production" {
		resp["password_dev"] = password
	}
	writeJSON(w, 201, resp)
}

type loginReq struct{ Email, Password string }

func (a *App) Login(w http.ResponseWriter, r *http.Request) {
	var in loginReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	u, err := a.Repo.GetUserByEmail(r.Context(), in.Email)
	if err != nil {
		writeErr(w, 401, "invalid_credentials")
		return
	}
	ok, _ := auth.VerifyPassword(in.Password, u.PasswordHash)
	if !ok {
		writeErr(w, 401, "invalid_credentials")
		return
	}
	access, err := auth.IssueAccessToken(a.Cfg.JWTSecret, u.ID, u.Role)
	if err != nil {
		writeErr(w, 500, "token_failed")
		return
	}
	refreshRaw, refreshHash, _ := auth.RandomToken(32)
	if err := a.Repo.CreateSession(r.Context(), u.ID, refreshHash, r.UserAgent(), middleware.ClientIP(r), auth.RefreshTTL); err != nil {
		writeErr(w, 500, "session_failed")
		return
	}
	setAuthCookies(w, r, access, refreshRaw)
	writeJSON(w, 200, map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role,
		"email_verified": u.EmailVerifiedAt != nil})
}

func (a *App) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("refresh_token"); err == nil {
		_ = a.Repo.RevokeSession(r.Context(), auth.HashToken(c.Value))
	}
	clearAuthCookies(w)
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (a *App) Refresh(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("refresh_token")
	if err != nil {
		writeErr(w, 401, "no_refresh")
		return
	}
	uid, err := a.Repo.FindSession(r.Context(), auth.HashToken(c.Value))
	if err != nil {
		writeErr(w, 401, "invalid_refresh")
		return
	}
	u, err := a.Repo.GetUser(r.Context(), uid)
	if err != nil {
		writeErr(w, 401, "user_gone")
		return
	}
	access, _ := auth.IssueAccessToken(a.Cfg.JWTSecret, u.ID, u.Role)
	setAuthCookies(w, r, access, c.Value)
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

type verifyReq struct{ Token string }

func (a *App) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var in verifyReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	uid, err := a.Repo.UseEmailToken(r.Context(), auth.HashToken(in.Token))
	if err != nil {
		writeErr(w, 400, "invalid_token")
		return
	}
	if err := a.Repo.MarkEmailVerified(r.Context(), uid); err != nil {
		writeErr(w, 500, "db")
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

type forgotReq struct{ Email string }

func (a *App) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var in forgotReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	u, err := a.Repo.GetUserByEmail(r.Context(), in.Email)
	if err != nil {
		writeJSON(w, 200, map[string]string{"ok": "1"})
		return
	}
	raw, hash, _ := auth.RandomToken(32)
	_ = a.Repo.CreatePasswordResetToken(r.Context(), u.ID, hash, 1*time.Hour)
	link := a.Cfg.AppHost + "/auth/reset?token=" + raw
	a.Mail.Async(u.Email, "Сброс пароля", "<p>Сбросьте пароль: <a href=\""+link+"\">"+link+"</a></p>")
	writeJSON(w, 200, map[string]string{"ok": "1", "reset_link_dev": link})
}

type resetReq struct{ Token, Password string }

func (a *App) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var in resetReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	if len(in.Password) < 8 {
		writeErr(w, 400, "weak_password")
		return
	}
	uid, err := a.Repo.UsePasswordResetToken(r.Context(), auth.HashToken(in.Token))
	if err != nil {
		writeErr(w, 400, "invalid_token")
		return
	}
	hash, _ := auth.HashPassword(in.Password)
	if err := a.Repo.UpdateUserPassword(r.Context(), uid, hash); err != nil {
		writeErr(w, 500, "db")
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (a *App) Me(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UserID(r.Context())
	u, err := a.Repo.GetUser(r.Context(), uid)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	writeJSON(w, 200, map[string]any{
		"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role,
		"email_verified": u.EmailVerifiedAt != nil,
	})
}

type patchMeReq struct {
	Name           string `json:"name"`
	OldPassword    string `json:"old_password"`
	NewPassword    string `json:"new_password"`
}

func (a *App) PatchMe(w http.ResponseWriter, r *http.Request) {
	var in patchMeReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	uid, _ := middleware.UserID(r.Context())
	if in.Name != "" {
		_ = a.Repo.UpdateUserName(r.Context(), uid, in.Name)
	}
	if in.NewPassword != "" {
		u, err := a.Repo.GetUser(r.Context(), uid)
		if err != nil {
			writeErr(w, 404, "not_found")
			return
		}
		ok, _ := auth.VerifyPassword(in.OldPassword, u.PasswordHash)
		if !ok {
			writeErr(w, 400, "wrong_password")
			return
		}
		if len(in.NewPassword) < 8 {
			writeErr(w, 400, "weak_password")
			return
		}
		hash, _ := auth.HashPassword(in.NewPassword)
		_ = a.Repo.UpdateUserPassword(r.Context(), uid, hash)
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

// helper for tests
var _ = errors.New
var _ = db.ErrNotFound
