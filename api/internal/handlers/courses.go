package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/middleware"
)

func (a *App) ListCourses(w http.ResponseWriter, r *http.Request) {
	uid, authed := middleware.UserID(r.Context())
	courses, err := a.Repo.ListCourses(r.Context(), true)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	out := make([]map[string]any, 0, len(courses))
	for _, c := range courses {
		m := map[string]any{
			"id": c.ID, "slug": c.Slug, "title": c.Title, "subtitle": c.Subtitle,
			"description": c.Description, "cover_image_url": c.CoverImageURL,
			"kind": c.Kind, "price_rub": c.PriceRub,
		}
		if authed {
			has, _ := a.Repo.HasEnrollment(r.Context(), uid, c.ID)
			m["enrolled"] = has
		}
		out = append(out, m)
	}
	writeJSON(w, 200, out)
}

func (a *App) GetCourse(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	c, err := a.Repo.GetCourseBySlug(r.Context(), slug)
	if err != nil || !c.IsPublished {
		writeErr(w, 404, "not_found")
		return
	}
	uid, authed := middleware.UserID(r.Context())
	enrolled := false
	if authed {
		enrolled, _ = a.Repo.HasEnrollment(r.Context(), uid, c.ID)
	}
	modules, _ := a.Repo.ListModules(r.Context(), c.ID)
	lessons, _ := a.Repo.ListLessons(r.Context(), c.ID)
	// for paid courses without enrollment — strip content_md
	visible := make([]*db.Lesson, 0, len(lessons))
	for _, l := range lessons {
		if c.Kind == "paid" && !enrolled && !l.IsPreview {
			lv := *l
			lv.ContentMD = ""
			lv.VideoID = nil
			visible = append(visible, &lv)
		} else {
			visible = append(visible, l)
		}
	}
	writeJSON(w, 200, map[string]any{
		"course": c, "modules": modules, "lessons": visible, "enrolled": enrolled,
	})
}

func (a *App) GetLesson(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	lessonSlug := chi.URLParam(r, "lesson")
	c, err := a.Repo.GetCourseBySlug(r.Context(), slug)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	l, err := a.Repo.GetLesson(r.Context(), c.ID, lessonSlug)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	uid, authed := middleware.UserID(r.Context())
	enrolled := false
	if authed {
		enrolled, _ = a.Repo.HasEnrollment(r.Context(), uid, c.ID)
	}
	if !enrolled && !l.IsPreview && c.Kind == "paid" {
		writeErr(w, 403, "no_access")
		return
	}
	writeJSON(w, 200, map[string]any{"course": c, "lesson": l, "enrolled": enrolled})
}

type progressReq struct {
	Completed       bool `json:"completed"`
	LastPositionSec int  `json:"last_position_sec"`
}

func (a *App) PostProgress(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	lid, err := uuid.Parse(idStr)
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	var in progressReq
	_ = json.NewDecoder(r.Body).Decode(&in)
	uid, _ := middleware.UserID(r.Context())
	l, err := a.Repo.GetLessonByID(r.Context(), lid)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	c, _ := a.Repo.GetCourseByID(r.Context(), l.CourseID)
	if c.Kind == "paid" {
		has, _ := a.Repo.HasEnrollment(r.Context(), uid, c.ID)
		if !has {
			writeErr(w, 403, "no_access")
			return
		}
	}
	if err := a.Repo.UpsertProgress(r.Context(), uid, lid, in.Completed, in.LastPositionSec); err != nil {
		writeErr(w, 500, "db")
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (a *App) MyCourses(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UserID(r.Context())
	cs, err := a.Repo.UserEnrollments(r.Context(), uid)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		total, done, _ := a.Repo.CourseProgress(r.Context(), uid, c.ID)
		pct := 0
		if total > 0 {
			pct = done * 100 / total
		}
		out = append(out, map[string]any{
			"id": c.ID, "slug": c.Slug, "title": c.Title, "subtitle": c.Subtitle,
			"cover_image_url": c.CoverImageURL, "kind": c.Kind,
			"progress_pct": pct, "lessons_total": total, "lessons_done": done,
		})
	}
	writeJSON(w, 200, out)
}

// EnrollFree — instant enrollment for free courses.
func (a *App) EnrollFree(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	c, err := a.Repo.GetCourseBySlug(r.Context(), slug)
	if err != nil || c.Kind != "free" {
		writeErr(w, 404, "not_found")
		return
	}
	uid, _ := middleware.UserID(r.Context())
	if err := a.Repo.Grant(r.Context(), uid, c.ID, "free", nil); err != nil {
		writeErr(w, 500, "db")
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}
