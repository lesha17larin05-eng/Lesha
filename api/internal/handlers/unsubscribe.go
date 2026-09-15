package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/google/uuid"
)

// Отписка от маркетинговых писем.
//
// Ссылка в письме подписана HMAC-SHA256 на JWT-секрете с отдельным префиксом
// (доменное разделение ключа), поэтому чужой адрес отписать нельзя и хранить
// отдельные токены в базе не нужно.
//
// GET  /api/unsubscribe?u=<uuid>&t=<hex> — человек нажал ссылку в письме,
//
//	отписываем и уводим на страницу /unsubscribed.
//
// POST того же URL — «отписка в один клик» по RFC 8058: почтовые клиенты
//
//	(Gmail, Яндекс) дёргают адрес из заголовка List-Unsubscribe сами.
//	CSRF для этого пути отключён в middleware — защищает подпись.
const unsubscribePrefix = "unsubscribe:"

// UnsubscribeToken — подпись для ссылки отписки конкретного пользователя.
func UnsubscribeToken(secret, userID string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsubscribePrefix + userID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *App) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	rawID := r.URL.Query().Get("u")
	token := r.URL.Query().Get("t")

	ok := false
	if id, err := uuid.Parse(rawID); err == nil && token != "" {
		want := UnsubscribeToken(a.Cfg.JWTSecret, id.String())
		if hmac.Equal([]byte(want), []byte(token)) {
			// Повторное нажатие — не ошибка: снимаем согласие идемпотентно.
			if err := a.Repo.ClearMarketingConsent(r.Context(), id); err == nil {
				ok = true
			}
		}
	}

	// One-click из почтового клиента: ответ читает робот, не человек.
	if r.Method == http.MethodPost {
		if !ok {
			writeErr(w, 400, "invalid_token")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": 1})
		return
	}

	dest := "/unsubscribed"
	if !ok {
		dest = "/unsubscribed?error=1"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}
