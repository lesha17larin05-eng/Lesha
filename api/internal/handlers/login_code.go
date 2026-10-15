package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/auth"
	"github.com/leshalarin/api/internal/db"
)

// Вход без пароля и письмо о доступе.
//
// Как человек попадает к урокам после покупки или выдачи доступа:
//  1. Кнопка «Перейти к урокам» в письме – ссылка входа (login_links),
//     многоразовая, живёт 7 дней. Открывает /auth/link, страница сама входит.
//  2. Логин и пароль из того же письма. Если человек пароля не знает
//     (аккаунт заведён за него и он ни разу не входил) – пароль генерируем
//     и присылаем; сменить его можно в настройках кабинета.
//  3. «Войти по коду из письма» на странице входа – 6 цифр на почту,
//     живут 15 минут, 5 попыток. Выручает, если письмо с паролем потерялось.

const (
	loginLinkTTL      = 7 * 24 * time.Hour
	loginCodeTTL      = 15 * time.Minute
	loginCodeAttempts = 5
	loginCodeCooldown = 60 * time.Second // не чаще одного кода в минуту
	loginCodeHourCap  = 5                // и не больше пяти в час
)

// ---- пароли и коды ----

// Без похожих символов (0/O, 1/l/I), чтобы пароль из письма легко перепечатать.
const pwdAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// newReadablePassword – случайный пароль из 10 символов.
func newReadablePassword() (string, error) {
	var sb strings.Builder
	max := big.NewInt(int64(len(pwdAlphabet)))
	for i := 0; i < 10; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		sb.WriteByte(pwdAlphabet[n.Int64()])
	}
	return sb.String(), nil
}

func newLoginCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	s := n.String()
	for len(s) < 6 {
		s = "0" + s
	}
	return s, nil
}

// loginCodeHash – код короткий, поэтому хешируем с секретом и id человека:
// утечка таблицы не даёт подобрать коды перебором.
func (a *App) loginCodeHash(userID uuid.UUID, code string) string {
	mac := hmac.New(sha256.New, []byte(a.Cfg.JWTSecret))
	mac.Write([]byte("logincode:" + userID.String() + ":" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

// safeNext – только относительный путь на нашем сайте.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, "\\") {
		return "/cabinet"
	}
	return next
}

// loginLinkURL – ссылка «Перейти к урокам» для письма.
func (a *App) loginLinkURL(ctx context.Context, userID uuid.UUID, next string) (string, error) {
	raw, hash, err := auth.RandomToken(32)
	if err != nil {
		return "", err
	}
	if err := a.Repo.CreateLoginLink(ctx, userID, hash, safeNext(next), loginLinkTTL); err != nil {
		return "", err
	}
	return a.Cfg.AppHost + "/auth/link?token=" + raw, nil
}

// ---- письмо о доступе к курсу ----

// accessEmail – данные для письма «доступ открыт» (после оплаты или выдачи
// админом). Если человек не знает пароль, генерирует новый и сохраняет его.
type accessEmail struct {
	Subject string
	Intro   string // первый абзац (HTML), например «Спасибо за оплату…»
	Outro   string // абзацы после данных для входа (HTML)
}

// buildAccessEmail собирает письмо и возвращает его тело. password –
// новый пароль, если он был сгенерирован (для dev-ответов и тестов).
func (a *App) buildAccessEmail(ctx context.Context, u *db.User, courseSlug string, e accessEmail) (body, password string) {
	cabinet := "/cabinet"
	if courseSlug != "" {
		cabinet = "/cabinet/" + courseSlug
	}
	esc := html.EscapeString

	if unknown, err := a.Repo.PasswordUnknown(ctx, u.ID); err == nil && unknown {
		if p, err := newReadablePassword(); err == nil {
			if hash, err := auth.HashPassword(p); err == nil {
				if err := a.Repo.UpdateUserPassword(ctx, u.ID, hash); err == nil {
					password = p
				}
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(`<!doctype html><html lang="ru"><head><meta charset="utf-8"></head>` +
		`<body style="margin:0;padding:0;background:#fdfaf5;">` +
		`<div style="max-width:560px;margin:0 auto;padding:28px 22px;` +
		`font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Arial,sans-serif;` +
		`font-size:16px;line-height:1.65;color:#1a1a1a;">`)
	greeting := "Здравствуйте!"
	if f := strings.Fields(u.Name); len(f) > 0 {
		greeting = "Здравствуйте, " + esc(f[0]) + "!"
	}
	sb.WriteString(`<p style="margin:0 0 16px;">` + greeting + `</p>`)
	sb.WriteString(e.Intro)

	if link, err := a.loginLinkURL(ctx, u.ID, cabinet); err == nil {
		sb.WriteString(`<p style="margin:22px 0 8px;"><a href="` + link + `" style="` + dripBtnStyle + `">` +
			`Перейти к урокам&nbsp;→</a></p>` +
			`<p style="margin:0 0 20px;font-size:13px;color:#777;">Кнопка входит в кабинет без пароля. ` +
			`Работает 7 дней – дальше входите по паролю или по коду из письма.</p>`)
	} else {
		slog.Warn("access email: login link", "err", err)
	}

	loginURL := a.Cfg.AppHost + "/auth/login?email=" + url.QueryEscape(u.Email)
	sb.WriteString(`<div style="background:#fff;border:1px solid #ece8e0;border-radius:14px;padding:16px 20px;margin:0 0 20px;">` +
		`<p style="margin:0 0 6px;"><b>Данные для входа</b></p>` +
		`<p style="margin:0;">Сайт: <a href="` + loginURL + `" style="` + dripLinkStyle + `">leshalarin.ru</a><br>` +
		`Логин: ` + esc(u.Email) + `<br>`)
	if password != "" {
		sb.WriteString(`Пароль: <b style="font-family:Menlo,Consolas,monospace;font-size:17px;letter-spacing:.5px;">` +
			esc(password) + `</b></p>` +
			`<p style="margin:10px 0 0;font-size:14px;color:#555;">Пароль можно сменить в кабинете: «Настройки».</p>`)
	} else {
		sb.WriteString(`Пароль: тот, что вы задавали раньше</p>` +
			`<p style="margin:10px 0 0;font-size:14px;color:#555;">Не помните пароль? На странице входа ` +
			`нажмите «Войти по коду из письма» – пришлю код на эту почту.</p>`)
	}
	sb.WriteString(`</div>`)
	sb.WriteString(`<p style="margin:0 0 16px;">Сохраните это письмо – пригодится, чтобы вернуться к урокам.</p>`)
	sb.WriteString(e.Outro)
	sb.WriteString(a.subscribeOfferHTML(ctx, u.ID))
	sb.WriteString(`</div></body></html>`)
	return sb.String(), password
}

// subscribeOfferHTML – блок «Хочу получать письма» для тех, у кого нет
// согласия на рассылку и кто от неё не отписывался. Нажатие и есть
// согласие (handlers/subscribe.go). Остальным – пустая строка.
func (a *App) subscribeOfferHTML(ctx context.Context, userID uuid.UUID) string {
	if ok, err := a.Repo.SubscribeOfferAllowed(ctx, userID); err != nil || !ok {
		return ""
	}
	return subscribeOfferBlock(a.subscribeURL(userID))
}

func subscribeOfferBlock(link string) string {
	return `<div style="background:#fdf3e8;border:1px solid rgba(232,101,42,0.25);` +
		`border-radius:14px;padding:18px 20px;margin:24px 0 8px;">` +
		`<p style="margin:0 0 8px;font-size:16px;line-height:1.6;"><b>Присылать вам полезные письма?</b></p>` +
		`<p style="margin:0 0 14px;font-size:15px;line-height:1.6;color:#555;">` +
		`Пишу нечасто и по делу: короткие практики, разборы, как не бросить занятия, ` +
		`и первым рассказываю о новых курсах и скидках. Отписаться – одной кнопкой в любом письме.</p>` +
		`<p style="margin:0;"><a href="` + link + `" style="display:inline-block;background:#e8652a;` +
		`color:#fff;text-decoration:none;padding:11px 22px;border-radius:100px;font-size:15px;` +
		`font-weight:600;">Да, присылайте</a></p>` +
		`<p style="margin:10px 0 0;font-size:13px;color:#777;">На доступ к курсу это не влияет.</p>` +
		`</div>`
}

// ---- вход по ссылке ----

// LoginByLink – POST /api/auth/link {token}: вход по кнопке из письма.
// Отвечает {next} – куда вести (курс из письма).
func (a *App) LoginByLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil || in.Token == "" {
		writeErr(w, 400, "bad_json")
		return
	}
	uid, next, err := a.Repo.UseLoginLink(r.Context(), auth.HashToken(in.Token))
	if err != nil {
		writeErr(w, 400, "invalid_token")
		return
	}
	u, err := a.Repo.GetUser(r.Context(), uid)
	if err != nil {
		writeErr(w, 400, "invalid_token")
		return
	}
	// Ссылка пришла на почту – значит, адрес подтверждён.
	_ = a.Repo.MarkEmailVerified(r.Context(), uid)
	if err := a.startSession(w, r, uid, u.Role); err != nil {
		writeErr(w, 500, "session_failed")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": 1, "next": safeNext(next)})
}

// ---- вход по коду ----

// RequestLoginCode – POST /api/auth/code {email}: прислать код на почту.
// Отвечает одинаково, есть такой адрес или нет, – иначе форма превращается
// в способ проверить, кто зарегистрирован.
func (a *App) RequestLoginCode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if !strings.Contains(email, "@") || len(email) < 5 {
		writeErr(w, 400, "bad_email")
		return
	}
	resp := map[string]any{"ok": 1}
	u, err := a.Repo.GetUserByEmail(r.Context(), email)
	if err != nil {
		writeJSON(w, 200, resp)
		return
	}
	now := time.Now()
	if n, err := a.Repo.LoginCodesSince(r.Context(), u.ID, now.Add(-loginCodeCooldown)); err != nil || n > 0 {
		// Код только что ушёл – новый не шлём, предыдущий действует.
		writeJSON(w, 200, resp)
		return
	}
	if n, err := a.Repo.LoginCodesSince(r.Context(), u.ID, now.Add(-time.Hour)); err != nil || n >= loginCodeHourCap {
		writeJSON(w, 200, resp)
		return
	}
	code, err := newLoginCode()
	if err != nil {
		writeErr(w, 500, "code_failed")
		return
	}
	if err := a.Repo.CreateLoginCode(r.Context(), u.ID, a.loginCodeHash(u.ID, code), loginCodeTTL); err != nil {
		writeErr(w, 500, "db")
		return
	}
	a.Mail.Async(u.Email, "Код для входа: "+code,
		`<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Arial,sans-serif;font-size:16px;line-height:1.6;color:#1a1a1a;max-width:520px;">`+
			`<p>Здравствуйте! Ваш код для входа на leshalarin.ru:</p>`+
			`<p style="font-size:32px;font-weight:700;letter-spacing:6px;margin:18px 0;">`+code+`</p>`+
			`<p>Код действует 15 минут. Введите его на странице входа.</p>`+
			`<p style="color:#777;font-size:13px;">Если вы не запрашивали код – просто не обращайте внимания на это письмо, `+
			`без кода в ваш кабинет никто не войдёт.</p></div>`)
	if a.Cfg.AppEnv != "production" {
		resp["code_dev"] = code
	}
	writeJSON(w, 200, resp)
}

// VerifyLoginCode – POST /api/auth/code/verify {email, code}: вход по коду.
func (a *App) VerifyLoginCode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	code := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, in.Code)
	if len(code) != 6 {
		writeErr(w, 400, "invalid_code")
		return
	}
	u, err := a.Repo.GetUserByEmail(r.Context(), email)
	if err != nil {
		writeErr(w, 400, "invalid_code")
		return
	}
	if err := a.Repo.UseLoginCode(r.Context(), u.ID, a.loginCodeHash(u.ID, code), loginCodeAttempts); err != nil {
		if errors.Is(err, db.ErrTooManyAttempts) {
			writeErr(w, 400, "too_many_attempts")
			return
		}
		writeErr(w, 400, "invalid_code")
		return
	}
	_ = a.Repo.MarkEmailVerified(r.Context(), u.ID)
	if err := a.startSession(w, r, u.ID, u.Role); err != nil {
		writeErr(w, 500, "session_failed")
		return
	}
	writeJSON(w, 200, map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role})
}
