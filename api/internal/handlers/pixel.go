package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"

	"github.com/google/uuid"
)

// Счётчик открытий писем рассылки.
//
// В письмо вшита прозрачная картинка 1×1 с подписанной ссылкой. Почтовый
// клиент загружает её при открытии письма — так мы понимаем, дошло письмо
// до человека или лежит в спаме. Многие клиенты картинки блокируют, поэтому
// цифра открытий — нижняя граница, а не точное число.
//
// GET /api/pixel.gif?u=<uuid>&c=<кампания>&t=<подпись>
//
// Картинка отдаётся всегда, даже при плохой подписи: письмо не должно
// показывать «битую» иконку, а перебор чужих id ничего не даёт.
const pixelPrefix = "pixel:"

// прозрачный GIF 1×1
var pixelGIF, _ = base64.StdEncoding.DecodeString(
	"R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7")

// PixelToken — подпись для счётчика открытий конкретного письма.
func PixelToken(secret, campaign, userID string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(pixelPrefix + campaign + ":" + userID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *App) EmailPixel(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rawID, campaign, token := q.Get("u"), q.Get("c"), q.Get("t")

	if id, err := uuid.Parse(rawID); err == nil && campaign != "" && token != "" {
		want := PixelToken(a.Cfg.JWTSecret, campaign, id.String())
		if hmac.Equal([]byte(want), []byte(token)) {
			// User-Agent помогает отличить человека от прокси почтовика,
			// который подгружает картинки заранее.
			a.Repo.LogEmailOpen(r.Context(), id, campaign, r.UserAgent())
		}
	}

	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pixelGIF)
}

// AdminEmailOpens — сводка по рассылкам для админки.
func (a *App) AdminEmailOpens(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Repo.EmailOpenStats(r.Context())
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, 200, rows)
}
