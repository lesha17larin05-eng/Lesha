package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

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
