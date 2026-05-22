package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leshalarin/api/internal/config"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/email"
	"github.com/leshalarin/api/internal/handlers"
	mw "github.com/leshalarin/api/internal/middleware"
	"github.com/leshalarin/api/internal/prodamus"
)

func setup(t *testing.T) (*httptest.Server, *db.Repo, *config.Config) {
	t.Helper()
	dburl := os.Getenv("TEST_DATABASE_URL")
	if dburl == "" {
		dburl = os.Getenv("DATABASE_URL")
	}
	if dburl == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dburl)
	if err != nil {
		t.Fatal(err)
	}
	// reset schema
	mustExec(t, pool, `TRUNCATE users, courses, modules, lessons, videos, enrollments, lesson_progress, orders, payment_webhooks, sessions, email_verification_tokens, password_reset_tokens, audit_log, articles RESTART IDENTITY CASCADE`)
	repo := db.NewRepo(pool)
	cfg := &config.Config{
		AppEnv: "test", AppHost: "http://test",
		JWTSecret: "test-jwt-secret-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		ProdamusSecret: "test-pd-secret",
		ProdamusTestMode: true,
		VideoTokenSecret: "test-video-secret",
		VideoTokenTTL: time.Hour,
		CORSOrigin: "*",
	}
	app := &handlers.App{
		Cfg: cfg, Repo: repo,
		Mail: email.New("", "", "", "", ""),
		Prodamus: prodamus.New("https://test.payform.ru", cfg.ProdamusSecret),
	}
	r := chi.NewRouter()
	r.Use(mw.RequestID, mw.Recover, mw.CORS("*"))
	r.Use(mw.Auth(cfg.JWTSecret, repo))
	r.Use(mw.CSRF)
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":1}`)) })
	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/register", app.Register)
		r.Post("/quick-signup", app.QuickSignup)
		r.Post("/login", app.Login)
		r.Post("/logout", app.Logout)
		r.Post("/refresh", app.Refresh)
		r.Post("/verify-email", app.VerifyEmail)
	})
	r.Get("/api/courses", app.ListCourses)
	r.Get("/api/courses/{slug}", app.GetCourse)
	r.Get("/api/courses/{slug}/lessons/{lesson}", app.GetLesson)
	r.Get("/api/articles", app.ListArticles)
	r.Get("/api/articles/{slug}", app.GetArticle)
	r.Group(func(r chi.Router) {
		r.Use(mw.RequireAuth)
		r.Get("/api/me", app.Me)
		r.Patch("/api/me", app.PatchMe)
		r.Get("/api/me/courses", app.MyCourses)
		r.Post("/api/lessons/{id}/progress", app.PostProgress)
		r.Post("/api/courses/{slug}/enroll-free", app.EnrollFree)
		r.Post("/api/courses/{slug}/checkout", app.Checkout)
		r.Get("/api/courses/{slug}/files/{name}", app.CourseFile)
	})
	r.Post("/api/webhooks/prodamus", app.ProdamusWebhook)
	r.Get("/api/dev/fake-payment", app.FakePayment)
	r.Group(func(r chi.Router) {
		r.Use(mw.RequireAuth, mw.RequireAdmin)
		r.Get("/api/admin/stats", app.AdminStats)
		r.Get("/api/admin/users", app.AdminUsers)
		r.Post("/api/admin/courses", app.AdminCreateCourse)
		r.Get("/api/admin/courses/{id}", app.AdminGetCourse)
		r.Patch("/api/admin/courses/{id}", app.AdminUpdateCourse)
		r.Post("/api/admin/modules", app.AdminCreateModule)
		r.Patch("/api/admin/modules/{id}", app.AdminUpdateModule)
		r.Delete("/api/admin/modules/{id}", app.AdminDeleteModule)
		r.Post("/api/admin/lessons", app.AdminCreateLesson)
		r.Get("/api/admin/lessons/{id}", app.AdminGetLesson)
		r.Patch("/api/admin/lessons/{id}", app.AdminUpdateLesson)
		r.Delete("/api/admin/lessons/{id}", app.AdminDeleteLesson)
		r.Get("/api/admin/articles", app.AdminListArticles)
		r.Post("/api/admin/articles", app.AdminCreateArticle)
		r.Get("/api/admin/articles/{id}", app.AdminGetArticle)
		r.Patch("/api/admin/articles/{id}", app.AdminUpdateArticle)
		r.Delete("/api/admin/articles/{id}", app.AdminDeleteArticle)
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	t.Cleanup(pool.Close)
	return srv, repo, cfg
}

func mustExec(t *testing.T, pool *pgxpool.Pool, q string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), q); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

type client struct {
	srv *httptest.Server
	jar *cookiejar.Jar
	csrf string
}

func newClient(srv *httptest.Server) *client {
	jar, _ := cookiejar.New(nil)
	return &client{srv: srv, jar: jar}
}

func (c *client) http() *http.Client {
	return &http.Client{Jar: c.jar}
}

func (c *client) do(method, path string, body any) (*http.Response, []byte) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if c.csrf == "" {
		// initial GET to set csrf cookie
		warm, _ := c.http().Get(c.srv.URL + "/api/health")
		if warm != nil {
			warm.Body.Close()
		}
		u, _ := url.Parse(c.srv.URL)
		for _, ck := range c.jar.Cookies(u) {
			if ck.Name == "csrf" {
				c.csrf = ck.Value
			}
		}
	}
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func TestHealth(t *testing.T) {
	srv, _, _ := setup(t)
	c := newClient(srv)
	resp, body := c.do("GET", "/api/health", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestRegisterLoginMe(t *testing.T) {
	srv, _, _ := setup(t)
	c := newClient(srv)
	r, body := c.do("POST", "/api/auth/register", map[string]any{
		"email": "a@b.ru", "password": "password123", "name": "A", "consent_pd": true})
	if r.StatusCode != 201 {
		t.Fatalf("register failed: %d %s", r.StatusCode, body)
	}
	r, body = c.do("POST", "/api/auth/login", map[string]string{"email": "a@b.ru", "password": "password123"})
	if r.StatusCode != 200 {
		t.Fatalf("login failed: %d %s", r.StatusCode, body)
	}
	r, body = c.do("GET", "/api/me", nil)
	if r.StatusCode != 200 {
		t.Fatalf("me failed: %d %s", r.StatusCode, body)
	}
	if !strings.Contains(string(body), "a@b.ru") {
		t.Fatalf("me missing email: %s", body)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	srv, _, _ := setup(t)
	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{"email": "x@b.ru", "password": "password123", "consent_pd": true})
	r, _ := c.do("POST", "/api/auth/login", map[string]string{"email": "x@b.ru", "password": "wrongpw"})
	if r.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r.StatusCode)
	}
}

func TestUnauthenticatedMe(t *testing.T) {
	srv, _, _ := setup(t)
	c := newClient(srv)
	r, _ := c.do("GET", "/api/me", nil)
	if r.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r.StatusCode)
	}
}

func TestEnrollFreeFlow(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	price := 0
	_ = price
	cid, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "free1", Title: "Free", Kind: "free", IsPublished: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateLesson(ctx, db.LessonInput{
		CourseID: cid, Title: "L1", Slug: "l1", ContentMD: "hi", SortOrder: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{"email": "u@b.ru", "password": "password123", "consent_pd": true})
	c.do("POST", "/api/auth/login", map[string]string{"email": "u@b.ru", "password": "password123"})
	r, body := c.do("POST", "/api/courses/free1/enroll-free", nil)
	if r.StatusCode != 200 {
		t.Fatalf("enroll: %d %s", r.StatusCode, body)
	}
	r, body = c.do("GET", "/api/me/courses", nil)
	if !strings.Contains(string(body), "free1") {
		t.Fatalf("my courses missing: %s", body)
	}
}

func TestPaidCheckoutRequiresVerification(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	price := 9900
	_, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "paid1", Title: "Paid", Kind: "paid", PriceRub: &price, IsPublished: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{"email": "p@b.ru", "password": "password123", "consent_pd": true})
	c.do("POST", "/api/auth/login", map[string]string{"email": "p@b.ru", "password": "password123"})
	r, _ := c.do("POST", "/api/courses/paid1/checkout", nil)
	if r.StatusCode != 400 {
		t.Fatalf("expected 400 (email_not_verified), got %d", r.StatusCode)
	}
}

func TestPaidCheckoutTestModeReturnsFakeURL(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	price := 9900
	_, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "paid2", Title: "Paid2", Kind: "paid", PriceRub: &price, IsPublished: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{"email": "v@b.ru", "password": "password123", "consent_pd": true})
	// mark verified directly
	u, _ := repo.GetUserByEmail(ctx, "v@b.ru")
	_ = repo.MarkEmailVerified(ctx, u.ID)
	c.do("POST", "/api/auth/login", map[string]string{"email": "v@b.ru", "password": "password123"})
	r, body := c.do("POST", "/api/courses/paid2/checkout", nil)
	if r.StatusCode != 200 {
		t.Fatalf("checkout: %d %s", r.StatusCode, body)
	}
	if !strings.Contains(string(body), "fake-payment") {
		t.Fatalf("expected fake-payment URL, got %s", body)
	}
}

func TestAdminEndpointsRequireAdminRole(t *testing.T) {
	srv, _, _ := setup(t)
	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{"email": "n@b.ru", "password": "password123", "consent_pd": true})
	c.do("POST", "/api/auth/login", map[string]string{"email": "n@b.ru", "password": "password123"})
	r, _ := c.do("GET", "/api/admin/stats", nil)
	if r.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", r.StatusCode)
	}
}

func TestQuickSignupCreatesAndAuthenticates(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	price := 9900
	freeID, err := repo.CreateCourse(ctx, db.CourseInput{Slug: "qs-free", Title: "QF", Kind: "free", IsPublished: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateCourse(ctx, db.CourseInput{Slug: "qs-paid", Title: "QP", Kind: "paid", PriceRub: &price, IsPublished: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateCourse(ctx, db.CourseInput{Slug: "qs-free-draft", Title: "QFD", Kind: "free", IsPublished: false})
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(srv)
	r, body := c.do("POST", "/api/auth/quick-signup", map[string]any{
		"email": "quick@b.ru", "name": "Q", "consent_pd": true})
	if r.StatusCode != 201 {
		t.Fatalf("quick-signup: %d %s", r.StatusCode, body)
	}
	if !strings.Contains(string(body), "\"created\":true") {
		t.Fatalf("missing created flag: %s", body)
	}
	u, err := repo.GetUserByEmail(ctx, "quick@b.ru")
	if err != nil {
		t.Fatalf("user not created: %v", err)
	}
	if u.EmailVerifiedAt == nil {
		t.Fatalf("email should be auto-verified")
	}
	r, body = c.do("GET", "/api/me", nil)
	if r.StatusCode != 200 {
		t.Fatalf("me failed after quick-signup: %d %s", r.StatusCode, body)
	}
	if !strings.Contains(string(body), "quick@b.ru") {
		t.Fatalf("me missing email: %s", body)
	}
	if has, _ := repo.HasEnrollment(ctx, u.ID, freeID); !has {
		t.Fatalf("expected auto-enrollment in published free course")
	}
	r, body = c.do("GET", "/api/me/courses", nil)
	if !strings.Contains(string(body), "qs-free") || strings.Contains(string(body), "qs-paid") || strings.Contains(string(body), "qs-free-draft") {
		t.Fatalf("expected only published free in /api/me/courses: %s", body)
	}
}

func TestQuickSignupExistingEmailReturnsExists(t *testing.T) {
	srv, _, _ := setup(t)
	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{
		"email": "dup@b.ru", "password": "password123", "name": "D", "consent_pd": true})
	c2 := newClient(srv)
	r, body := c2.do("POST", "/api/auth/quick-signup", map[string]any{
		"email": "dup@b.ru", "name": "D2", "consent_pd": true,
	})
	if r.StatusCode != 200 {
		t.Fatalf("expected 200, got %d %s", r.StatusCode, body)
	}
	if !strings.Contains(string(body), "\"exists\":true") {
		t.Fatalf("missing exists flag: %s", body)
	}
	r, _ = c2.do("GET", "/api/me", nil)
	if r.StatusCode != 401 {
		t.Fatalf("expected 401 (no session set), got %d", r.StatusCode)
	}
}

func TestQuickSignupRejectsBadEmail(t *testing.T) {
	srv, _, _ := setup(t)
	c := newClient(srv)
	r, _ := c.do("POST", "/api/auth/quick-signup", map[string]any{"email": "nope", "consent_pd": true})
	if r.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", r.StatusCode)
	}
}

func TestQuickSignupRequiresCSRF(t *testing.T) {
	srv, _, _ := setup(t)
	// don't warm up CSRF — bypass by raw request
	req, _ := http.NewRequest("POST", srv.URL+"/api/auth/quick-signup",
		bytes.NewReader([]byte(`{"email":"x@y.ru"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403 csrf_failed, got %d", resp.StatusCode)
	}
}

func TestAdminCanCreateCourse(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{"email": "ad@b.ru", "password": "password123", "consent_pd": true})
	u, _ := repo.GetUserByEmail(ctx, "ad@b.ru")
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, u.ID)
	c.do("POST", "/api/auth/login", map[string]string{"email": "ad@b.ru", "password": "password123"})
	r, body := c.do("POST", "/api/admin/courses", map[string]any{
		"slug": "n1", "title": "T", "kind": "free", "is_published": true,
	})
	if r.StatusCode != 201 {
		t.Fatalf("create: %d %s", r.StatusCode, body)
	}
}

func TestProgressEndpoint(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	cid, _ := repo.CreateCourse(ctx, db.CourseInput{Slug: "fp", Title: "F", Kind: "free", IsPublished: true})
	lid, _ := repo.CreateLesson(ctx, db.LessonInput{CourseID: cid, Title: "L", Slug: "l", SortOrder: 1})
	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{"email": "pr@b.ru", "password": "password123", "consent_pd": true})
	c.do("POST", "/api/auth/login", map[string]string{"email": "pr@b.ru", "password": "password123"})
	r, body := c.do("POST", "/api/lessons/"+lid.String()+"/progress",
		map[string]any{"completed": true, "last_position_sec": 42})
	if r.StatusCode != 200 {
		t.Fatalf("progress: %d %s", r.StatusCode, body)
	}
	// course progress must reflect 100%
	uid, _ := repo.GetUserByEmail(ctx, "pr@b.ru")
	total, done, _ := repo.CourseProgress(ctx, uid.ID, cid)
	if total != 1 || done != 1 {
		t.Fatalf("expected 1/1 progress, got %d/%d", done, total)
	}
}

func TestProdamusWebhookValidSignature(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()
	uid, _ := repo.CreateUser(ctx, "wh@b.ru", "x", "wh", "user")
	cid, _ := repo.CreateCourse(ctx, db.CourseInput{Slug: "wp", Title: "W", Kind: "paid", PriceRub: ptrInt(100), IsPublished: true})
	o, _ := repo.CreateOrder(ctx, uid, cid, 100)
	body := map[string]any{
		"order_id":       o.ID.String(),
		"payment_status": "success",
	}
	sig, _ := prodamus.Sign(cfg.ProdamusSecret, body)
	form := url.Values{}
	for k, v := range body {
		form.Set(k, v.(string))
	}
	form.Set("signature", sig)
	req, _ := http.NewRequest("POST", srv.URL+"/api/webhooks/prodamus", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sign", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("webhook status %d", resp.StatusCode)
	}
	o2, _ := repo.GetOrder(ctx, o.ID)
	if o2.Status != "paid" {
		t.Fatalf("expected paid, got %s", o2.Status)
	}
	has, _ := repo.HasEnrollment(ctx, uid, cid)
	if !has {
		t.Fatal("expected enrollment after webhook")
	}
}

func TestProdamusWebhookInvalidSignature(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	uid, _ := repo.CreateUser(ctx, "wb@b.ru", "x", "wb", "user")
	cid, _ := repo.CreateCourse(ctx, db.CourseInput{Slug: "wp2", Title: "W2", Kind: "paid", PriceRub: ptrInt(100), IsPublished: true})
	o, _ := repo.CreateOrder(ctx, uid, cid, 100)
	form := url.Values{"order_id": {o.ID.String()}, "payment_status": {"success"}, "signature": {"deadbeef"}}
	req, _ := http.NewRequest("POST", srv.URL+"/api/webhooks/prodamus", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sign", "deadbeef")
	resp, _ := http.DefaultClient.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	o2, _ := repo.GetOrder(ctx, o.ID)
	if o2.Status == "paid" {
		t.Fatal("invalid signature must not pay the order")
	}
}

func TestPublicCoursesHidesPaidContent(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	cid, _ := repo.CreateCourse(ctx, db.CourseInput{Slug: "pub1", Title: "Pub", Kind: "paid", PriceRub: ptrInt(100), IsPublished: true})
	_, _ = repo.CreateLesson(ctx, db.LessonInput{CourseID: cid, Title: "Hidden", Slug: "h", ContentMD: "SECRET", SortOrder: 1})
	c := newClient(srv)
	r, body := c.do("GET", "/api/courses/pub1", nil)
	if r.StatusCode != 200 {
		t.Fatalf("status %d %s", r.StatusCode, body)
	}
	if strings.Contains(string(body), "SECRET") {
		t.Fatal("paid lesson content must not leak to anonymous users")
	}
}

func TestUUIDsFormat(t *testing.T) {
	id := uuid.New()
	if len(id.String()) != 36 {
		t.Fatal("uuid format")
	}
}

func ptrInt(i int) *int { return &i }

func adminClient(t *testing.T, srv *httptest.Server, repo *db.Repo) *client {
	t.Helper()
	ctx := context.Background()
	email := "admin-" + uuid.New().String() + "@b.ru"
	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{"email": email, "password": "password123", "consent_pd": true})
	u, _ := repo.GetUserByEmail(ctx, email)
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, u.ID)
	c.do("POST", "/api/auth/login", map[string]string{"email": email, "password": "password123"})
	return c
}

func TestAdminCRUDLessonsFlow(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	cid, err := repo.CreateCourse(ctx, db.CourseInput{Slug: "edit1", Title: "Edit", Kind: "paid", PriceRub: ptrInt(100), IsPublished: false})
	if err != nil {
		t.Fatal(err)
	}
	c := adminClient(t, srv, repo)

	// GET admin course (full payload, even unpublished)
	r, body := c.do("GET", "/api/admin/courses/"+cid.String(), nil)
	if r.StatusCode != 200 {
		t.Fatalf("get course: %d %s", r.StatusCode, body)
	}
	if !strings.Contains(string(body), "\"course\"") {
		t.Fatalf("missing course key: %s", body)
	}

	// PATCH course
	r, body = c.do("PATCH", "/api/admin/courses/"+cid.String(), map[string]any{
		"slug": "edit1", "title": "Edit-renamed", "kind": "paid", "price_rub": 200,
		"is_published": true, "sort_order": 5,
	})
	if r.StatusCode != 200 {
		t.Fatalf("patch course: %d %s", r.StatusCode, body)
	}
	got, _ := repo.GetCourseByID(ctx, cid)
	if got.Title != "Edit-renamed" || got.PriceRub == nil || *got.PriceRub != 200 || !got.IsPublished {
		t.Fatalf("course not updated: %+v", got)
	}

	// CREATE module
	r, body = c.do("POST", "/api/admin/modules", map[string]any{
		"course_id": cid, "title": "M1", "sort_order": 1,
	})
	if r.StatusCode != 201 {
		t.Fatalf("create module: %d %s", r.StatusCode, body)
	}
	var modResp map[string]string
	_ = json.Unmarshal(body, &modResp)
	modID, _ := uuid.Parse(modResp["id"])

	// PATCH module — rename + reorder
	r, body = c.do("PATCH", "/api/admin/modules/"+modID.String(), map[string]any{
		"title": "M1-renamed", "sort_order": 7,
	})
	if r.StatusCode != 200 {
		t.Fatalf("patch module: %d %s", r.StatusCode, body)
	}
	mods, _ := repo.ListModules(ctx, cid)
	if len(mods) != 1 || mods[0].Title != "M1-renamed" || mods[0].SortOrder != 7 {
		t.Fatalf("module not updated: %+v", mods)
	}
	// PATCH module — empty title rejected
	r, _ = c.do("PATCH", "/api/admin/modules/"+modID.String(), map[string]any{"title": "", "sort_order": 1})
	if r.StatusCode != 400 {
		t.Fatalf("expected 400 for empty title, got %d", r.StatusCode)
	}

	// CREATE lesson
	r, body = c.do("POST", "/api/admin/lessons", map[string]any{
		"course_id": cid, "module_id": modID, "title": "L1", "slug": "l1",
		"content_md": "# hello", "sort_order": 1, "is_preview": true,
	})
	if r.StatusCode != 201 {
		t.Fatalf("create lesson: %d %s", r.StatusCode, body)
	}
	var lessonResp map[string]string
	_ = json.Unmarshal(body, &lessonResp)
	lessonID, _ := uuid.Parse(lessonResp["id"])

	// GET lesson by id
	r, body = c.do("GET", "/api/admin/lessons/"+lessonID.String(), nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "# hello") {
		t.Fatalf("get lesson: %d %s", r.StatusCode, body)
	}

	// PATCH lesson
	r, _ = c.do("PATCH", "/api/admin/lessons/"+lessonID.String(), map[string]any{
		"course_id": cid, "module_id": modID, "title": "L1-edited", "slug": "l1",
		"content_md": "# updated", "sort_order": 2, "is_preview": false,
	})
	if r.StatusCode != 200 {
		t.Fatalf("patch lesson: %d", r.StatusCode)
	}
	updated, _ := repo.GetLessonByID(ctx, lessonID)
	if updated.Title != "L1-edited" || updated.ContentMD != "# updated" || updated.IsPreview {
		t.Fatalf("lesson not updated: %+v", updated)
	}

	// Admin GET course returns updated lesson + module
	r, body = c.do("GET", "/api/admin/courses/"+cid.String(), nil)
	if !strings.Contains(string(body), "L1-edited") || !strings.Contains(string(body), "M1") {
		t.Fatalf("admin course payload missing items: %s", body)
	}

	// DELETE lesson
	r, _ = c.do("DELETE", "/api/admin/lessons/"+lessonID.String(), nil)
	if r.StatusCode != 200 {
		t.Fatalf("delete lesson: %d", r.StatusCode)
	}
	if _, err := repo.GetLessonByID(ctx, lessonID); err == nil {
		t.Fatal("lesson should be gone")
	}

	// DELETE module
	r, _ = c.do("DELETE", "/api/admin/modules/"+modID.String(), nil)
	if r.StatusCode != 200 {
		t.Fatalf("delete module: %d", r.StatusCode)
	}
}

func TestAdminLessonEndpointsForbiddenForUser(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	cid, _ := repo.CreateCourse(ctx, db.CourseInput{Slug: "fbd", Title: "F", Kind: "free", IsPublished: true})
	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{"email": "regular@b.ru", "password": "password123", "consent_pd": true})
	c.do("POST", "/api/auth/login", map[string]string{"email": "regular@b.ru", "password": "password123"})
	r, _ := c.do("GET", "/api/admin/courses/"+cid.String(), nil)
	if r.StatusCode != 403 {
		t.Fatalf("expected 403 for regular user, got %d", r.StatusCode)
	}
	r, _ = c.do("POST", "/api/admin/lessons", map[string]any{"course_id": cid, "title": "x", "slug": "x"})
	if r.StatusCode != 403 {
		t.Fatalf("expected 403 for create lesson, got %d", r.StatusCode)
	}
}

// CourseFile отдаёт PDF-материалы курса только пользователям с enrollment.
// Проверяем: 401 без логина, 403 без enrollment, 200 с enrollment, 404 для
// неизвестного файла. Закрытие /files/*.pdf — критичная safety-правка.
func TestCourseFileRequiresEnrollment(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	price := 3990
	cid, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "zdorovaya-spina", Title: "Здоровая спина", Kind: "paid",
		PriceRub: &price, IsPublished: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Anonymous → 401.
	anon := newClient(srv)
	r, _ := anon.do("GET", "/api/courses/zdorovaya-spina/files/metodichka.pdf", nil)
	if r.StatusCode != 401 {
		t.Fatalf("anon: expected 401, got %d", r.StatusCode)
	}

	// Logged-in but not enrolled → 403.
	c := newClient(srv)
	regResp, regBody := c.do("POST", "/api/auth/register", map[string]any{
		"email": "buyer@b.ru", "password": "password123", "consent_pd": true})
	if regResp.StatusCode != 201 {
		t.Fatalf("register: %d %s", regResp.StatusCode, regBody)
	}
	c.do("POST", "/api/auth/login", map[string]string{
		"email": "buyer@b.ru", "password": "password123",
	})
	r, body := c.do("GET", "/api/courses/zdorovaya-spina/files/metodichka.pdf", nil)
	if r.StatusCode != 403 {
		t.Fatalf("not enrolled: expected 403, got %d body=%s", r.StatusCode, body)
	}

	// Look up user id and grant enrollment directly.
	user, err := repo.GetUserByEmail(ctx, "buyer@b.ru")
	if err != nil || user == nil {
		t.Fatalf("get user: %v", err)
	}
	if err := repo.Grant(ctx, user.ID, cid, "purchase", nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	// Enrolled → 200 + application/pdf.
	r, body = c.do("GET", "/api/courses/zdorovaya-spina/files/metodichka.pdf", nil)
	if r.StatusCode != 200 {
		t.Fatalf("enrolled: expected 200, got %d body=%s", r.StatusCode, body)
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("expected Content-Type application/pdf, got %q", ct)
	}
	if len(body) < 1000 {
		t.Fatalf("pdf body suspiciously short (%d bytes)", len(body))
	}
	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Fatalf("response does not look like a PDF: first bytes = %q", body[:8])
	}

	// Unknown file name → 404, никаких path traversal.
	r, _ = c.do("GET", "/api/courses/zdorovaya-spina/files/secret.pdf", nil)
	if r.StatusCode != 404 {
		t.Fatalf("unknown file: expected 404, got %d", r.StatusCode)
	}
	r, _ = c.do("GET", "/api/courses/nonexistent/files/metodichka.pdf", nil)
	if r.StatusCode != 404 {
		t.Fatalf("unknown slug: expected 404, got %d", r.StatusCode)
	}
}

// 152-ФЗ: без согласия на обработку ПД регистрация невозможна.
// Бэк должен возвращать 400 consent_pd_required, а в БД — никакого user не создавать.
func TestRegisterRequiresConsentPD(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	c := newClient(srv)
	// consent_pd явно false (отсутствие поля = false)
	r, body := c.do("POST", "/api/auth/register", map[string]any{
		"email": "noconsent@b.ru", "password": "password123", "name": "N",
	})
	if r.StatusCode != 400 {
		t.Fatalf("expected 400 without consent, got %d %s", r.StatusCode, body)
	}
	if !strings.Contains(string(body), "consent_pd_required") {
		t.Fatalf("expected consent_pd_required error, got %s", body)
	}
	if _, err := repo.GetUserByEmail(ctx, "noconsent@b.ru"); err == nil {
		t.Fatalf("user should not exist after rejected registration")
	}
}

func TestQuickSignupRequiresConsentPD(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	c := newClient(srv)
	r, body := c.do("POST", "/api/auth/quick-signup", map[string]any{
		"email": "noconsent2@b.ru", "name": "N",
	})
	if r.StatusCode != 400 {
		t.Fatalf("expected 400 without consent, got %d %s", r.StatusCode, body)
	}
	if !strings.Contains(string(body), "consent_pd_required") {
		t.Fatalf("expected consent_pd_required error, got %s", body)
	}
	if _, err := repo.GetUserByEmail(ctx, "noconsent2@b.ru"); err == nil {
		t.Fatalf("user should not exist after rejected quick-signup")
	}
}

// После успешной регистрации в users.consent_pd_at должна стоять отметка времени.
// consent_marketing_at — NULL, если маркетинговое согласие не дано.
// При consent_marketing=true — заполнено.
func TestRegisterSavesConsentTimestamps(t *testing.T) {
	srv, _, _ := setup(t)
	c := newClient(srv)
	r, body := c.do("POST", "/api/auth/register", map[string]any{
		"email": "withconsent@b.ru", "password": "password123", "name": "W",
		"consent_pd": true, "consent_marketing": true,
	})
	if r.StatusCode != 201 {
		t.Fatalf("register failed: %d %s", r.StatusCode, body)
	}
	pool := pgConnFromEnv(t)
	defer pool.Close()
	var pdAt, marketingAt *time.Time
	err := pool.QueryRow(context.Background(),
		`SELECT consent_pd_at, consent_marketing_at FROM users WHERE email=$1`, "withconsent@b.ru").
		Scan(&pdAt, &marketingAt)
	if err != nil {
		t.Fatalf("query consent: %v", err)
	}
	if pdAt == nil {
		t.Fatalf("consent_pd_at should not be NULL after consented registration")
	}
	if marketingAt == nil {
		t.Fatalf("consent_marketing_at should not be NULL when user opted in")
	}

	// Второй пользователь, без маркетингового согласия.
	c2 := newClient(srv)
	r, body = c2.do("POST", "/api/auth/register", map[string]any{
		"email": "noads@b.ru", "password": "password123",
		"consent_pd": true, "consent_marketing": false,
	})
	if r.StatusCode != 201 {
		t.Fatalf("register failed: %d %s", r.StatusCode, body)
	}
	err = pool.QueryRow(context.Background(),
		`SELECT consent_pd_at, consent_marketing_at FROM users WHERE email=$1`, "noads@b.ru").
		Scan(&pdAt, &marketingAt)
	if err != nil {
		t.Fatalf("query consent: %v", err)
	}
	if pdAt == nil {
		t.Fatalf("consent_pd_at should not be NULL")
	}
	if marketingAt != nil {
		t.Fatalf("consent_marketing_at should be NULL when user did not opt in, got %v", *marketingAt)
	}
}

// Helper: открывает второй pgxpool — нужен в тестах, которые делают прямые SELECT'ы
// поверх пула из setup(), который не возвращается наружу.
func pgConnFromEnv(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dburl := os.Getenv("TEST_DATABASE_URL")
	if dburl == "" {
		dburl = os.Getenv("DATABASE_URL")
	}
	if dburl == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dburl)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	return pool
}
