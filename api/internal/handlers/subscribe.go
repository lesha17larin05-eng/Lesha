package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// Подписка на письма по ссылке – зеркало отписки.
//
// Зачем: людям, которым доступ к курсу выдали вручную, согласие на рассылку
// проставить за них нельзя – это должно быть их действие. Поэтому в сервисных
// письмах (о выдаче доступа, о самом курсе) есть кнопка «Хочу получать письма»:
// нажатие и есть согласие, момент фиксируется в users.consent_marketing_at.
//
// Ссылка подписана HMAC на JWT-секрете с собственным префиксом, поэтому
// подписать чужой адрес нельзя.
const subscribePrefix = "subscribe:"

// SubscribeToken – подпись для ссылки подписки конкретного пользователя.
func SubscribeToken(secret, userID string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(subscribePrefix + userID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *App) Subscribe(w http.ResponseWriter, r *http.Request) {
	rawID := r.URL.Query().Get("u")
	token := r.URL.Query().Get("t")

	ok := false
	if id, err := uuid.Parse(rawID); err == nil && token != "" {
		want := SubscribeToken(a.Cfg.JWTSecret, id.String())
		if hmac.Equal([]byte(want), []byte(token)) {
			// Повторное нажатие не двигает дату первого согласия.
			if err := a.Repo.SetMarketingConsent(r.Context(), id); err == nil {
				ok = true
			}
		}
	}

	if r.Method == http.MethodPost {
		if !ok {
			writeErr(w, 400, "invalid_token")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": 1})
		return
	}

	dest := "/subscribed"
	if !ok {
		dest = "/subscribed?error=1"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// ─── Подписка с сайта ───────────────────────────────────────────────────
//
// POST /api/newsletter {"email": "...", "consent_pd": true}
//
// Форма стоит в конце статей и под отзывами: там человек уже прогрет, но
// покупать не готов – почта единственный способ не потерять его совсем.
//
// Подтверждение в два шага (double opt-in): сначала письмо со ссылкой,
// согласие фиксируется только после нажатия. Так надёжнее по 152-ФЗ
// (человек подтвердил, что почта его) и бережёт репутацию домена: чужие
// адреса, вписанные из вредности, в рассылку не попадут.
//
// Отвечаем одинаково и новому адресу, и уже подписанному: иначе форма
// превращается в способ проверить, есть ли человек в базе.
func (a *App) NewsletterSignup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email     string `json:"email"`
		ConsentPD bool   `json:"consent_pd"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") || len(email) < 6 {
		writeErr(w, 400, "bad_email")
		return
	}
	if !in.ConsentPD {
		writeErr(w, 400, "consent_pd_required")
		return
	}

	// Заводим «пустой» аккаунт, если его ещё нет: пароль случайный, войти по
	// нему нельзя – человек восстановит его обычным способом, если захочет.
	uid, err := a.findOrCreateUser(r.Context(), email, "")
	if err != nil {
		writeErr(w, 500, "db")
		return
	}

	// Согласие на обработку ПД фиксируем сразу (галочка в форме),
	// согласие на рассылку – только после нажатия ссылки в письме.
	if err := a.Repo.SaveConsentPD(r.Context(), uid); err != nil {
		slog.Warn("newsletter consent", "err", err)
	}

	link := a.Cfg.AppHost + "/api/subscribe?u=" + uid.String() +
		"&t=" + SubscribeToken(a.Cfg.JWTSecret, uid.String())
	a.Mail.Async(email, "Подтвердите подписку на письма",
		`<p>Вы оставили эту почту на сайте leshalarin.ru, чтобы получать мои письма.</p>`+
			`<p>Нажмите, чтобы подтвердить: <a href="`+link+`">`+link+`</a></p>`+
			`<p>Если это были не вы – просто не отвечайте, без нажатия ничего не произойдёт.</p>`+
			`<p>Алексей Ларин</p>`)

	writeJSON(w, 200, map[string]any{"ok": 1})
}
