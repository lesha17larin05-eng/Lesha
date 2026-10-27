package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// HoneypotField – скрытое поле-ловушка в публичных формах сайта (класс .hp-field).
// Человек его не видит и не заполняет, бот заполняет все поля подряд.
const HoneypotField = "homepage"

// Honeypot тихо отклоняет запрос, если в JSON-теле заполнено поле-ловушка:
// отвечает 200 {"ok":1}, ничего не создаёт и писем не шлёт, чтобы бот
// не понял, что его поймали. Остальные запросы проходят с нетронутым телом.
func Honeypot(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || !strings.Contains(r.Header.Get("Content-Type"), "json") {
			next.ServeHTTP(w, r)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = r.Body.Close()
		if err != nil {
			http.Error(w, `{"error":"bad_body"}`, http.StatusBadRequest)
			return
		}
		var probe map[string]any
		if json.Unmarshal(raw, &probe) == nil {
			if v, ok := probe[HoneypotField].(string); ok && strings.TrimSpace(v) != "" {
				slog.Info("honeypot: bot form submission dropped", "path", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":1}`))
				return
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		r.ContentLength = int64(len(raw))
		next.ServeHTTP(w, r)
	})
}
