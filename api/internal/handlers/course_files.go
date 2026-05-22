package handlers

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/leshalarin/api/internal/assets"
	"github.com/leshalarin/api/internal/middleware"
)

// allowedCourseFiles — белый список slug курса → набор разрешённых файлов.
// Защита от path traversal и от случайной публикации лишнего файла из embed.
var allowedCourseFiles = map[string]map[string]string{
	"zdorovaya-spina": {
		"metodichka.pdf": "Методичка курса.pdf",
		"kalendar.pdf":   "Календарь занятий.pdf",
		"zamery.pdf":     "Замеры до и после.pdf",
	},
}

// CourseFile отдаёт защищённый файл курса (PDF-методички и т.п.).
// Доступ только при наличии активного enrollment у пользователя.
func (a *App) CourseFile(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	name := chi.URLParam(r, "name")

	// Sanity: только имя файла, без слешей и точек, белый список.
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		writeErr(w, 400, "bad_filename")
		return
	}
	files, ok := allowedCourseFiles[slug]
	if !ok {
		writeErr(w, 404, "not_found")
		return
	}
	prettyName, ok := files[name]
	if !ok {
		writeErr(w, 404, "not_found")
		return
	}

	uid, authed := middleware.UserID(r.Context())
	if !authed {
		writeErr(w, 401, "unauthorized")
		return
	}

	c, err := a.Repo.GetCourseBySlug(r.Context(), slug)
	if err != nil || c == nil {
		writeErr(w, 404, "not_found")
		return
	}
	enrolled, _ := a.Repo.HasEnrollment(r.Context(), uid, c.ID)
	if !enrolled {
		writeErr(w, 403, "no_enrollment")
		return
	}

	data, err := fs.ReadFile(assets.CourseFS(), path.Join(slug, name))
	if err != nil {
		writeErr(w, 404, "file_missing")
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+prettyName+`"`)
	w.Header().Set("Cache-Control", "private, max-age=0, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
