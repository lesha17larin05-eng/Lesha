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
	Email, Password, Name, Phone string
	ConsentPD                    bool `json:"consent_pd"`
	ConsentMarketing             bool `json:"consent_marketing"`
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
	// Опциональный телефон — пригодится для Продамуса и для связи через админку.
	if phone := strings.TrimSpace(in.Phone); phone != "" {
		_ = a.Repo.SetUserPhone(r.Context(), uid, phone)
	}
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

// QuickSignup — регистрация по email+имя за один шаг (для бесплатного курса).
// Создаёт юзера, генерит временный пароль и токен подтверждения email,
// отправляет письмо с ссылкой подтверждения и паролем. Доступ к курсу НЕ
// выдаётся до тех пор, пока пользователь не кликнет по ссылке —
// enrollment в free курсы делает VerifyEmail handler.
// Это защита от опечаток в email и фейковых регистраций.
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
	// 152-ФЗ: сохраняем факт согласий (consent_pd обязательное, marketing — опциональное).
	_ = a.Repo.SaveConsent(r.Context(), uid, true, in.ConsentMarketing)
	if in.Phone != "" {
		_ = a.Repo.SetUserPhone(r.Context(), uid, in.Phone)
	}
	// Токен подтверждения — после клика VerifyEmail выдаст enrollment в free курсы.
	rawTok, hashTok, _ := auth.RandomToken(32)
	_ = a.Repo.CreateEmailToken(r.Context(), uid, hashTok, 24*time.Hour)
	link := a.Cfg.AppHost + "/auth/verify?token=" + rawTok
	a.Mail.Async(in.Email, "Подтвердите почту — доступ к бесплатному курсу",
		"<p>Здравствуйте! Спасибо за регистрацию на сайте Алексея Ларина.</p>"+
			"<p>Чтобы открыть доступ к курсу «Мягкий старт», подтвердите вашу почту: "+
			"<a href=\""+link+"\">"+link+"</a></p>"+
			"<p>Ссылка действительна 24 часа.</p>"+
			"<hr>"+
			"<p><b>Данные для входа в личный кабинет:</b><br>"+
			"Логин: "+in.Email+"<br>"+
			"Пароль: "+password+"</p>"+
			"<p>Сохраните это письмо — пригодится в будущем.</p>")
	resp := map[string]any{"created": true, "id": uid, "email": in.Email, "verify_required": true}
	if a.Cfg.AppEnv != "production" {
		resp["password_dev"] = password
		resp["verify_link_dev"] = link
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
	// Идемпотентно выдаём enrollment во все free-курсы — для тех, кто пришёл
	// через quick-signup на /course (доступ к курсу открывается только после verify).
	// Repo.Grant — INSERT ... ON CONFLICT DO NOTHING, повторный verify не ломает.
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
	// Сразу логиним — после клика юзер попадает прямо в кабинет, без отдельной формы.
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
	writeJSON(w, 200, map[string]any{"ok": "1", "enrolled_free": enrolled})
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

type resendReq struct{ Email string }

// ResendVerification — повторная отправка письма подтверждения email.
// Не раскрывает существование адреса: ответ всегда 200 {ok}.
// Rate-limit — общий для /api/auth/* (см. main.go).
func (a *App) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var in resendReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	email := strings.TrimSpace(strings.ToLower(in.Email))
	if !strings.Contains(email, "@") {
		writeErr(w, 400, "invalid_input")
		return
	}
	u, err := a.Repo.GetUserByEmail(r.Context(), email)
	if err == nil && u.EmailVerifiedAt == nil {
		rawTok, hashTok, tokErr := auth.RandomToken(32)
		if tokErr == nil {
			_ = a.Repo.CreateEmailToken(r.Context(), u.ID, hashTok, 24*time.Hour)
			link := a.Cfg.AppHost + "/auth/verify?token=" + rawTok
			a.Mail.Async(email, "Подтвердите почту — доступ к курсу",
				"<p>Здравствуйте! Вы (или кто-то от вашего имени) запросили повторное письмо подтверждения на сайте Алексея Ларина.</p>"+
					"<p>Чтобы подтвердить почту и открыть доступ, перейдите по ссылке: "+
					"<a href=\""+link+"\">"+link+"</a></p>"+
					"<p>Ссылка действительна 24 часа. Если вы не запрашивали письмо — просто проигнорируйте его.</p>")
		}
	}
	// Всегда ok — не раскрываем, существует ли адрес и подтверждён ли он.
	writeJSON(w, 200, map[string]any{"ok": 1})
}
