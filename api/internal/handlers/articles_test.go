package handlers_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/leshalarin/api/internal/db"
)

func makeAdmin(t *testing.T, c *client, repo *db.Repo, email string) {
	t.Helper()
	// consent_pd обязателен после [Юр 5.5]
	c.do("POST", "/api/auth/register", map[string]any{
		"email":       email,
		"password":    "password123",
		"consent_pd":  true,
	})
	u, _ := repo.GetUserByEmail(context.Background(), email)
	if u == nil {
		t.Fatalf("makeAdmin: register did not create user %s", email)
	}
	_, _ = repo.Pool.Exec(context.Background(), `UPDATE users SET role='admin' WHERE id=$1`, u.ID)
	c.do("POST", "/api/auth/login", map[string]string{"email": email, "password": "password123"})
}

func TestListArticlesEmpty(t *testing.T) {
	srv, _, _ := setup(t)
	c := newClient(srv)
	r, body := c.do("GET", "/api/articles", nil)
	if r.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", r.StatusCode, body)
	}
	if !strings.Contains(string(body), "[") {
		t.Fatalf("expected array, got: %s", body)
	}
}

func TestArticleCRUDFlow(t *testing.T) {
	srv, repo, _ := setup(t)
	c := newClient(srv)
	makeAdmin(t, c, repo, "ed@b.ru")

	// create
	r, body := c.do("POST", "/api/admin/articles", map[string]any{
		"slug":            "test-post",
		"title":           "Тестовая статья",
		"tag":             "Тест",
		"excerpt":         "О чём это",
		"content_html":    "<p>Привет, мир</p>",
		"reading_minutes": 3,
		"is_published":    true,
	})
	if r.StatusCode != 201 {
		t.Fatalf("create: %d %s", r.StatusCode, body)
	}

	// public list shows it
	r, body = c.do("GET", "/api/articles", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "test-post") {
		t.Fatalf("list missing slug: %d %s", r.StatusCode, body)
	}
	// list omits content_html
	if strings.Contains(string(body), "Привет, мир") {
		t.Fatalf("list must not include content_html: %s", body)
	}

	// public get by slug
	r, body = c.do("GET", "/api/articles/test-post", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "Привет, мир") {
		t.Fatalf("get: %d %s", r.StatusCode, body)
	}

	// unpublished must 404 publicly
	a, err := repo.GetArticleBySlug(context.Background(), "test-post")
	if err != nil {
		t.Fatal(err)
	}
	r, _ = c.do("PATCH", "/api/admin/articles/"+a.ID.String(), map[string]any{
		"slug":         "test-post",
		"title":        "Тестовая статья",
		"content_html": "<p>скрыто</p>",
		"is_published": false,
	})
	if r.StatusCode != 200 {
		t.Fatalf("patch: %d", r.StatusCode)
	}
	r, _ = c.do("GET", "/api/articles/test-post", nil)
	if r.StatusCode != 404 {
		t.Fatalf("unpublished must 404, got %d", r.StatusCode)
	}

	// admin list shows it
	r, body = c.do("GET", "/api/admin/articles", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "test-post") {
		t.Fatalf("admin list: %d %s", r.StatusCode, body)
	}

	// delete
	r, _ = c.do("DELETE", "/api/admin/articles/"+a.ID.String(), nil)
	if r.StatusCode != 200 {
		t.Fatalf("delete: %d", r.StatusCode)
	}
}

func TestArticleAdminRequiresAdmin(t *testing.T) {
	srv, _, _ := setup(t)
	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{
		"email": "u2@b.ru", "password": "password123", "consent_pd": true,
	})
	c.do("POST", "/api/auth/login", map[string]string{"email": "u2@b.ru", "password": "password123"})
	r, _ := c.do("POST", "/api/admin/articles", map[string]any{
		"slug": "x", "title": "x", "content_html": "<p>x</p>",
	})
	if r.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", r.StatusCode)
	}
}

func TestArticleCreateValidatesRequiredFields(t *testing.T) {
	srv, repo, _ := setup(t)
	c := newClient(srv)
	makeAdmin(t, c, repo, "ed2@b.ru")
	r, _ := c.do("POST", "/api/admin/articles", map[string]any{
		"title":        "no slug",
		"content_html": "<p>x</p>",
	})
	if r.StatusCode != 400 {
		t.Fatalf("expected 400 missing slug, got %d", r.StatusCode)
	}
}

func TestArticleCreateRequiresCSRF(t *testing.T) {
	srv, repo, _ := setup(t)
	c := newClient(srv)
	makeAdmin(t, c, repo, "ed3@b.ru")
	// drop csrf token to simulate request without it
	c.csrf = "wrong"
	r, _ := c.do("POST", "/api/admin/articles", map[string]any{
		"slug": "y", "title": "y", "content_html": "<p>y</p>",
	})
	if r.StatusCode != 403 {
		t.Fatalf("expected 403 csrf, got %d", r.StatusCode)
	}
}

func TestParseArticleHTMLViaRepo(t *testing.T) {
	srv, repo, _ := setup(t)
	c := newClient(srv)
	makeAdmin(t, c, repo, "ed4@b.ru")
	// publish-now logic: empty published_at + is_published=true → server fills now()
	c.do("POST", "/api/admin/articles", map[string]any{
		"slug": "publish-fill", "title": "PF", "content_html": "<p>x</p>",
		"is_published": true,
	})
	a, err := repo.GetArticleBySlug(context.Background(), "publish-fill")
	if err != nil {
		t.Fatal(err)
	}
	if a.PublishedAt == nil {
		t.Fatalf("published_at must be auto-filled")
	}
	if time.Since(*a.PublishedAt) > time.Minute {
		t.Fatalf("published_at must be ~now, got %v", a.PublishedAt)
	}
}
