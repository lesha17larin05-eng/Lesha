package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Отписка от маркетинговых писем – в два шага.
//
// Ссылка в письме подписана HMAC-SHA256 на JWT-секрете с отдельным префиксом
// (доменное разделение ключа), поэтому чужой адрес отписать нельзя и хранить
// отдельные токены в базе не нужно.
//
// Ссылка «Отписаться» в теле письма ведёт на страницу /unsubscribe?u=&t=:
// там человек выбирает причину и подтверждает кнопкой (случайное нажатие
// ничего не делает), а после отписки может сразу подписаться обратно.
//
// GET  /api/unsubscribe?u=&t=        – старые письма: ничего не меняет, уводит на /unsubscribe.
// GET  /api/unsubscribe/status?u=&t= – подписан ли человек (для страницы).
// POST /api/unsubscribe?u=&t=        – отписать. Тело {reason, comment} необязательно:
//
//	без тела это «отписка в один клик» по RFC 8058 – почтовые клиенты
//	(Gmail, Яндекс) дёргают адрес из заголовка List-Unsubscribe сами.
//	CSRF для этого пути отключён в middleware – защищает подпись.
//
// POST /api/unsubscribe/undo?u=&t=   – «передумал»: вернуть подписку тем же токеном.
const unsubscribePrefix = "unsubscribe:"

// UnsubscribeToken – подпись для ссылки отписки конкретного пользователя.
func UnsubscribeToken(secret, userID string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsubscribePrefix + userID))
	return hex.EncodeToString(mac.Sum(nil))
}

// unsubscribeUser – пользователь из подписанной ссылки (u + t), иначе uuid.Nil.
func (a *App) unsubscribeUser(r *http.Request) uuid.UUID {
	id, err := uuid.Parse(r.URL.Query().Get("u"))
	token := r.URL.Query().Get("t")
	if err != nil || token == "" {
		return uuid.Nil
	}
	want := UnsubscribeToken(a.Cfg.JWTSecret, id.String())
	if !hmac.Equal([]byte(want), []byte(token)) {
		return uuid.Nil
	}
	return id
}

// Причины отписки – белый список (ключ → как показываем в админке).
var unsubscribeReasons = map[string]bool{
	"too_often": true, "not_relevant": true, "already_have": true,
	"never_signed": true, "other": true,
}

func (a *App) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// Старые письма вели сразу сюда. Теперь ничего не меняем, а показываем
		// страницу подтверждения – случайный тап больше не отписывает.
		http.Redirect(w, r, "/unsubscribe?"+r.URL.RawQuery, http.StatusSeeOther)
		return
	}
	id := a.unsubscribeUser(r)
	if id == uuid.Nil {
		writeErr(w, 400, "invalid_token")
		return
	}
	// Повторная отписка – не ошибка: снимаем согласие идемпотентно.
	if err := a.Repo.ClearMarketingConsent(r.Context(), id); err != nil {
		writeErr(w, 500, "db")
		return
	}
	var in struct {
		Reason  string `json:"reason"`
		Comment string `json:"comment"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&in)
	}
	if unsubscribeReasons[in.Reason] {
		comment := strings.TrimSpace(in.Comment)
		if len([]rune(comment)) > 1000 {
			comment = string([]rune(comment)[:1000])
		}
		_ = a.Repo.SaveUnsubscribeReason(r.Context(), id, in.Reason, comment)
	}
	writeJSON(w, 200, map[string]any{"ok": 1})
}

// UnsubscribeStatus – GET /api/unsubscribe/status: подписан ли человек.
func (a *App) UnsubscribeStatus(w http.ResponseWriter, r *http.Request) {
	id := a.unsubscribeUser(r)
	if id == uuid.Nil {
		writeErr(w, 400, "invalid_token")
		return
	}
	sub, err := a.Repo.HasMarketingConsent(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	writeJSON(w, 200, map[string]any{"subscribed": sub})
}

// UnsubscribeUndo – POST /api/unsubscribe/undo: вернуть подписку.
func (a *App) UnsubscribeUndo(w http.ResponseWriter, r *http.Request) {
	id := a.unsubscribeUser(r)
	if id == uuid.Nil {
		writeErr(w, 400, "invalid_token")
		return
	}
	if err := a.Repo.SetMarketingConsent(r.Context(), id); err != nil {
		writeErr(w, 500, "db")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": 1, "subscribed": true})
}

// AdminUnsubscribes – GET /api/admin/unsubscribes?days=30: причины отписок.
func (a *App) AdminUnsubscribes(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 3650 {
		days = 30
	}
	counts, comments, err := a.Repo.UnsubscribeStats(r.Context(), time.Now().AddDate(0, 0, -days))
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	writeJSON(w, 200, map[string]any{"days": days, "reasons": counts, "comments": comments})
}
