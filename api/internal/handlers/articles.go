package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/middleware"
)

// ---- PUBLIC ----

// ListArticles returns published articles. Excludes content_html for list view.
func (a *App) ListArticles(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	arts, err := a.Repo.ListArticles(r.Context(), true, limit, offset)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	out := make([]map[string]any, 0, len(arts))
	for _, x := range arts {
		out = append(out, map[string]any{
			"id":              x.ID,
			"slug":            x.Slug,
			"title":           x.Title,
			"tag":             x.Tag,
			"excerpt":         x.Excerpt,
			"cover_image_url": x.CoverImageURL,
			"reading_minutes": x.ReadingMinutes,
			"published_at":    x.PublishedAt,
		})
	}
	writeJSON(w, 200, out)
}

func (a *App) GetArticle(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	art, err := a.Repo.GetArticleBySlug(r.Context(), slug)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	if !art.IsPublished {
		writeErr(w, 404, "not_found")
		return
	}
	writeJSON(w, 200, art)
}

// ---- ADMIN ----

func (a *App) AdminListArticles(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	arts, err := a.Repo.ListArticles(r.Context(), false, limit, offset)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	out := make([]map[string]any, 0, len(arts))
	for _, x := range arts {
		out = append(out, map[string]any{
			"id":              x.ID,
			"slug":            x.Slug,
			"title":           x.Title,
			"tag":             x.Tag,
			"excerpt":         x.Excerpt,
			"reading_minutes": x.ReadingMinutes,
			"is_published":    x.IsPublished,
			"published_at":    x.PublishedAt,
			"updated_at":      x.UpdatedAt,
		})
	}
	writeJSON(w, 200, out)
}

func (a *App) AdminGetArticle(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	art, err := a.Repo.GetArticleByID(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	writeJSON(w, 200, art)
}

type articleInputJSON struct {
	Slug           string  `json:"slug"`
	Title          string  `json:"title"`
	Tag            string  `json:"tag"`
	Excerpt        string  `json:"excerpt"`
	CoverImageURL  string  `json:"cover_image_url"`
	ContentHTML    string  `json:"content_html"`
	ReadingMinutes int     `json:"reading_minutes"`
	IsPublished    bool    `json:"is_published"`
	PublishedAt    *string `json:"published_at"`
	SortOrder      int     `json:"sort_order"`
}

func (in articleInputJSON) toRepo(authorID *uuid.UUID) db.ArticleInput {
	var pub *time.Time
	if in.PublishedAt != nil && *in.PublishedAt != "" {
		if t, err := time.Parse(time.RFC3339, *in.PublishedAt); err == nil {
			pub = &t
		}
	}
	if in.IsPublished && pub == nil {
		now := time.Now()
		pub = &now
	}
	return db.ArticleInput{
		Slug:           in.Slug,
		Title:          in.Title,
		Tag:            in.Tag,
		Excerpt:        in.Excerpt,
		CoverImageURL:  in.CoverImageURL,
		ContentHTML:    in.ContentHTML,
		ReadingMinutes: in.ReadingMinutes,
		IsPublished:    in.IsPublished,
		PublishedAt:    pub,
		SortOrder:      in.SortOrder,
		AuthorID:       authorID,
	}
}

func (a *App) AdminCreateArticle(w http.ResponseWriter, r *http.Request) {
	var in articleInputJSON
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	if in.Slug == "" || in.Title == "" {
		writeErr(w, 400, "slug_and_title_required")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	id, err := a.Repo.CreateArticle(r.Context(), in.toRepo(&adminID))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	a.Repo.Audit(r.Context(), adminID, "create", "article", &id, map[string]any{"slug": in.Slug})
	writeJSON(w, 201, map[string]any{"id": id})
}

func (a *App) AdminUpdateArticle(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	var in articleInputJSON
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	if in.Slug == "" || in.Title == "" {
		writeErr(w, 400, "slug_and_title_required")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	if err := a.Repo.UpdateArticle(r.Context(), id, in.toRepo(&adminID)); err != nil {
		writeErr(w, 500, "db")
		return
	}
	a.Repo.Audit(r.Context(), adminID, "update", "article", &id, nil)
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (a *App) AdminDeleteArticle(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	if err := a.Repo.DeleteArticle(r.Context(), id); err != nil {
		writeErr(w, 500, "db")
		return
	}
	adminID, _ := middleware.UserID(r.Context())
	a.Repo.Audit(r.Context(), adminID, "delete", "article", &id, nil)
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

// ─── Счётчик чтения статей ──────────────────────────────────────────────
//
// POST /api/articles/{slug}/view  {"event":"open"|"read"|"cta"}
//
// Публичный, без auth и без CSRF: браузер шлёт его через sendBeacon, когда
// страница уже закрывается, и cookie-заголовки там не гарантированы. Записи
// анонимны (см. миграцию 015), подделка счётчика ничего не даёт, кроме
// испорченной статистики у самого Алексея.
//
// Отвечаем 204 всегда, даже на мусор: это маяк, а не форма — браузеру
// незачем разбирать ошибку, а ботам незачем понимать, сработало ли.
func (a *App) TrackArticleView(w http.ResponseWriter, r *http.Request) {
	defer func() { w.WriteHeader(http.StatusNoContent) }()

	// Роботов не считаем: иначе половина «прочтений» окажется краулерами.
	// User-agent используем только для этой проверки и нигде не сохраняем.
	ua := strings.ToLower(r.UserAgent())
	for _, marker := range []string{"bot", "crawler", "spider", "yandex", "google", "bing", "curl", "wget", "python"} {
		if strings.Contains(ua, marker) {
			return
		}
	}

	var in struct {
		Event string `json:"event"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512)).Decode(&in); err != nil {
		return
	}
	if !db.ArticleEvents[in.Event] {
		return
	}

	art, err := a.Repo.GetArticleBySlug(r.Context(), chi.URLParam(r, "slug"))
	if err != nil || art == nil || !art.IsPublished {
		return
	}
	if err := a.Repo.LogArticleView(r.Context(), art.ID, in.Event); err != nil {
		slog.Warn("article view", "slug", art.Slug, "err", err)
	}
}

// AdminArticleStats — сводка для админки: сколько открыли, дочитали и
// сколько перешли со статьи на платную страницу.
func (a *App) AdminArticleStats(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Repo.ArticleStats(r.Context())
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	writeJSON(w, 200, rows)
}
