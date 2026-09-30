package handlers

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Метка источника трафика приходит в куке `src` (её ставит
// web/src/components/SourceCapture.astro по ?from= / ?utm_source= или referrer).

var sourceRe = regexp.MustCompile(`^[a-z0-9_.:-]{1,48}$`)

// trafficSource – метка из куки, очищенная. Пустая строка – метки нет.
func trafficSource(r *http.Request) string {
	c, err := r.Cookie("src")
	if err != nil {
		return ""
	}
	v, err := url.QueryUnescape(c.Value)
	if err != nil {
		return ""
	}
	v = strings.ToLower(strings.TrimSpace(v))
	if !sourceRe.MatchString(v) {
		return ""
	}
	return v
}

// tagUser – записать источник новому пользователю (если метка есть).
func (a *App) tagUser(ctx context.Context, r *http.Request, uid uuid.UUID) {
	if src := trafficSource(r); src != "" {
		_ = a.Repo.SetUserSource(ctx, uid, src)
	}
}

// tagOrder – записать источник заказу (если метка есть).
func (a *App) tagOrder(ctx context.Context, r *http.Request, orderID uuid.UUID) {
	if src := trafficSource(r); src != "" {
		_ = a.Repo.SetOrderSource(ctx, orderID, src)
	}
}

// AdminSources – GET /api/admin/sources?days=30: откуда пришли люди и оплаты.
func (a *App) AdminSources(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 3650 {
		days = 30
	}
	rows, err := a.Repo.SourceStats(r.Context(), time.Now().AddDate(0, 0, -days))
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	writeJSON(w, 200, map[string]any{"days": days, "rows": rows})
}
