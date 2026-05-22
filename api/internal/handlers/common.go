package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/leshalarin/api/internal/auth"
	"github.com/leshalarin/api/internal/config"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/email"
	"github.com/leshalarin/api/internal/prodamus"
)

type App struct {
	Cfg      *config.Config
	Repo     *db.Repo
	Mail     *email.Sender
	Prodamus *prodamus.Client
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// IsHTTPSRequest сообщает, пришёл ли запрос по HTTPS — напрямую (r.TLS)
// или через reverse-proxy (заголовок X-Forwarded-Proto, который выставляет
// nginx/Cloudflare/Caddy). Используется, чтобы выставлять Secure-флаг
// только когда соединение реально TLS — иначе браузер отбросит cookie.
func IsHTTPSRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		// возможен список через запятую (CDN-цепочки)
		for _, p := range strings.Split(proto, ",") {
			if strings.EqualFold(strings.TrimSpace(p), "https") {
				return true
			}
		}
	}
	return false
}

func setAuthCookies(w http.ResponseWriter, r *http.Request, accessToken, refreshToken string) {
	secure := IsHTTPSRequest(r)
	http.SetCookie(w, &http.Cookie{
		Name: "access_token", Value: accessToken, Path: "/",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(auth.AccessTTL),
	})
	http.SetCookie(w, &http.Cookie{
		Name: "refresh_token", Value: refreshToken, Path: "/api/auth",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(auth.RefreshTTL),
	})
	// expose role hint for the frontend (not httpOnly — UI only)
	http.SetCookie(w, &http.Cookie{
		Name: "auth", Value: "1", Path: "/",
		Secure: secure, SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(auth.RefreshTTL),
	})
}

func clearAuthCookies(w http.ResponseWriter) {
	for _, n := range []string{"access_token", "refresh_token", "auth"} {
		http.SetCookie(w, &http.Cookie{Name: n, Value: "", Path: "/", MaxAge: -1})
	}
}
