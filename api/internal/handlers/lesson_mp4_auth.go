package handlers

import (
	"net/http"
	"strings"

	"github.com/leshalarin/api/internal/middleware"
)

// LessonMp4Auth — auth_request handler for nginx. Called by nginx whenever
// someone hits `/lesson-videos/<course-slug>/<file>` for a paid course.
//
// nginx will:
//   - if we return 2xx → serve the mp4 directly (alias /var/videos/).
//   - if we return 401/403 → fail the request with the corresponding code.
//
// Body of the response is discarded by nginx (auth_request semantics).
//
// We extract the course slug from X-Original-URI, then verify:
//  1. user is authenticated (JWT cookie),
//  2. course exists,
//  3. user has an active enrollment (or is admin).
//
// Free courses do NOT go through this handler — they are served by the
// open `/lesson-videos/` alias.
func (a *App) LessonMp4Auth(w http.ResponseWriter, r *http.Request) {
	orig := r.Header.Get("X-Original-URI")
	if orig == "" {
		orig = r.URL.RequestURI()
	}
	// strip query
	if q := strings.IndexByte(orig, '?'); q >= 0 {
		orig = orig[:q]
	}
	const prefix = "/lesson-videos/"
	if !strings.HasPrefix(orig, prefix) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	rest := orig[len(prefix):]
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	courseSlug := rest[:slash]

	// 1. authenticated?
	uid, authed := middleware.UserID(r.Context())
	if !authed {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// 2. course exists?
	c, err := a.Repo.GetCourseBySlug(r.Context(), courseSlug)
	if err != nil || c == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// 3. admin always OK
	if middleware.Role(r.Context()) == "admin" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 4. enrollment?
	enrolled, _ := a.Repo.HasEnrollment(r.Context(), uid, c.ID)
	if !enrolled {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	w.WriteHeader(http.StatusOK)
}
