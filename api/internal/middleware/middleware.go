package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/auth"
	"github.com/leshalarin/api/internal/db"
)

type ctxKey string

const (
	UserIDKey ctxKey = "uid"
	RoleKey   ctxKey = "role"
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(ww, r)
		slog.Info("http",
			"method", r.Method, "path", r.URL.Path,
			"status", ww.status, "dur_ms", time.Since(start).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(c int) { s.status = c; s.ResponseWriter.WriteHeader(c) }

func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic", "err", rec, "path", r.URL.Path)
				http.Error(w, `{"error":"internal"}`, 500)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func CORS(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,X-CSRF-Token,Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if r.Method == http.MethodOptions {
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Auth parses access_token cookie or Authorization Bearer.
func Auth(secret string, repo *db.Repo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := ""
			if c, err := r.Cookie("access_token"); err == nil {
				tok = c.Value
			} else if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				tok = strings.TrimPrefix(h, "Bearer ")
			}
			if tok != "" {
				claims, err := auth.ParseAccessToken(secret, tok)
				if err == nil {
					ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
					ctx = context.WithValue(ctx, RoleKey, claims.Role)
					if repo != nil {
						go repo.TouchLastSeen(context.Background(), claims.UserID)
					}
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserID(r.Context()); !ok {
			http.Error(w, `{"error":"unauthorized"}`, 401)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Role(r.Context()) != "admin" {
			http.Error(w, `{"error":"forbidden"}`, 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func UserID(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(UserIDKey).(uuid.UUID)
	return v, ok
}

func Role(ctx context.Context) string {
	v, _ := ctx.Value(RoleKey).(string)
	return v
}

// CSRF: double-submit cookie.
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("csrf"); err != nil || c.Value == "" {
			tok := randHex(16)
			// Secure-флаг ставим, если запрос фактически по HTTPS —
			// напрямую или через nginx/Cloudflare (X-Forwarded-Proto).
			secure := r.TLS != nil
			if !secure {
				if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
					for _, p := range strings.Split(proto, ",") {
						if strings.EqualFold(strings.TrimSpace(p), "https") {
							secure = true
							break
						}
					}
				}
			}
			http.SetCookie(w, &http.Cookie{
				Name: "csrf", Value: tok, Path: "/",
				SameSite: http.SameSiteLaxMode, Secure: secure,
				MaxAge: 60 * 60 * 24 * 30, // 30 дней — иначе session-cookie умирает между визитами
			})
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		// allow webhook & internal endpoints to bypass
		if strings.HasPrefix(r.URL.Path, "/api/webhooks/") || strings.HasPrefix(r.URL.Path, "/api/internal/") {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie("csrf")
		if err != nil || c.Value == "" || c.Value != r.Header.Get("X-CSRF-Token") {
			http.Error(w, `{"error":"csrf_failed"}`, 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RateLimit: simple in-memory token-bucket per IP+path.
type rateBucket struct {
	count int
	reset time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	max     int
	window  time.Duration
}

func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{buckets: map[string]*rateBucket{}, max: max, window: window}
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rl.mu.Lock()
		key := clientIP(r) + "|" + r.URL.Path
		b, ok := rl.buckets[key]
		now := time.Now()
		if !ok || now.After(b.reset) {
			rl.buckets[key] = &rateBucket{count: 1, reset: now.Add(rl.window)}
			rl.mu.Unlock()
			next.ServeHTTP(w, r)
			return
		}
		b.count++
		if b.count > rl.max {
			rl.mu.Unlock()
			http.Error(w, `{"error":"rate_limited"}`, 429)
			return
		}
		rl.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func ClientIP(r *http.Request) string { return clientIP(r) }

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	return r.RemoteAddr
}
