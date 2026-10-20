package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leshalarin/api/internal/auth"
	"github.com/leshalarin/api/internal/config"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/email"
	"github.com/leshalarin/api/internal/handlers"
	mw "github.com/leshalarin/api/internal/middleware"
	"github.com/leshalarin/api/internal/notify"
	"github.com/leshalarin/api/internal/prodamus"
)

// Прокладка Telegram для тестов: запоминает тексты уведомлений.
var (
	relayMu   sync.Mutex
	relayMsgs []string
)

func testRelay(t *testing.T) *httptest.Server {
	relayMu.Lock()
	relayMsgs = nil
	relayMu.Unlock()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["secret"] == "relay-secret" {
			relayMu.Lock()
			relayMsgs = append(relayMsgs, in["text"])
			relayMu.Unlock()
		}
		w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// waitRelay ждёт уведомление, содержащее substr (уведомления асинхронные).
func waitRelay(t *testing.T, substr string) string {
	t.Helper()
	for i := 0; i < 100; i++ {
		relayMu.Lock()
		for _, m := range relayMsgs {
			if strings.Contains(m, substr) {
				relayMu.Unlock()
				return m
			}
		}
		relayMu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("telegram: нет уведомления с %q, есть %v", substr, relayMsgs)
	return ""
}

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
	mustExec(t, pool, `TRUNCATE users, courses, modules, lessons, videos, enrollments, lesson_progress, lesson_activity, orders, payment_webhooks, sessions, email_verification_tokens, password_reset_tokens, audit_log, articles, leads, site_settings, email_opens, campaigns, campaign_recipients, article_views, drip_sends, unsubscribe_reasons, questionnaires, email_outbox RESTART IDENTITY CASCADE`)
	repo := db.NewRepo(pool)
	cfg := &config.Config{
		AppEnv: "test", AppHost: "http://test",
		JWTSecret:        "test-jwt-secret-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		ProdamusSecret:   "test-pd-secret",
		ProdamusTestMode: true,
		VideoTokenSecret: "test-video-secret",
		VideoTokenTTL:    time.Hour,
		CORSOrigin:       "*",
	}
	app := &handlers.App{
		Cfg: cfg, Repo: repo,
		Mail:     email.New("", "", "", "", ""),
		Prodamus: prodamus.New("https://test.payform.ru", cfg.ProdamusSecret),
		TG:       notify.New(testRelay(t).URL, "relay-secret"),
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
		r.Post("/fix-email", app.FixEmail)
		r.Post("/resend-verification", app.ResendVerification)
		r.Post("/forgot-password", app.ForgotPassword)
		r.Post("/reset-password", app.ResetPassword)
		r.Post("/link", app.LoginByLink)
		r.Post("/code", app.RequestLoginCode)
		r.Post("/code/verify", app.VerifyLoginCode)
	})
	r.Get("/api/courses", app.ListCourses)
	r.Get("/api/courses/{slug}", app.GetCourse)
	r.Get("/api/courses/{slug}/lessons/{lesson}", app.GetLesson)
	r.Get("/api/articles", app.ListArticles)
	r.Get("/api/articles/{slug}", app.GetArticle)
	r.Post("/api/articles/{slug}/view", app.TrackArticleView)
	r.Post("/api/newsletter", app.NewsletterSignup)
	r.Post("/api/checkout/service", app.ServiceCheckout)
	r.Post("/api/courses/{slug}/quick-checkout", app.QuickCheckout)
	r.Get("/api/orders/{id}/status", app.OrderStatus)
	r.Get("/api/orders/{id}/enter", app.OrderEnter)
	r.Get("/api/settings", app.PublicSettings)
	r.Get("/api/pixel.gif", app.EmailPixel)
	r.Get("/api/c", app.EmailClick)
	r.Get("/api/unsubscribe", app.Unsubscribe)
	r.Post("/api/unsubscribe", app.Unsubscribe)
	r.Get("/api/unsubscribe/status", app.UnsubscribeStatus)
	r.Post("/api/unsubscribe/undo", app.UnsubscribeUndo)
	r.Get("/api/anketa", app.GetAnketa)
	r.Post("/api/anketa", app.SubmitAnketa)
	r.Get("/api/subscribe", app.Subscribe)
	r.Post("/api/subscribe", app.Subscribe)
	r.Group(func(r chi.Router) {
		r.Use(mw.RequireAuth)
		r.Get("/api/me", app.Me)
		r.Patch("/api/me", app.PatchMe)
		r.Get("/api/me/courses", app.MyCourses)
		r.Get("/api/me/continue", app.MeContinue)
		r.Post("/api/lessons/{id}/progress", app.PostProgress)
		r.Post("/api/courses/{slug}/enroll-free", app.EnrollFree)
		r.Post("/api/courses/{slug}/checkout", app.Checkout)
		r.Get("/api/courses/{slug}/files/{name}", app.CourseFile)
	})
	r.Post("/api/leads", app.CreateLead)
	r.Post("/api/webhooks/prodamus", app.ProdamusWebhook)
	r.Get("/api/dev/fake-payment", app.FakePayment)
	r.Group(func(r chi.Router) {
		r.Use(mw.RequireAuth, mw.RequireAdmin)
		r.Get("/api/admin/stats", app.AdminStats)
		r.Get("/api/admin/sources", app.AdminSources)
		r.Post("/api/admin/anketa", app.AdminAnketa)
		r.Patch("/api/admin/settings", app.AdminUpdateSettings)
		r.Get("/api/admin/activity", app.AdminActivity)
		r.Get("/api/admin/email-opens", app.AdminEmailOpens)
		r.Get("/api/admin/segments", app.AdminSegments)
		r.Get("/api/admin/campaigns", app.AdminListCampaigns)
		r.Post("/api/admin/campaigns", app.AdminCreateCampaign)
		r.Get("/api/admin/campaigns/{id}", app.AdminGetCampaign)
		r.Post("/api/admin/campaigns/{id}/{action}", app.AdminCampaignAction)
		r.Get("/api/admin/leads", app.AdminLeads)
		r.Patch("/api/admin/leads/{id}", app.AdminUpdateLead)
		r.Get("/api/admin/users", app.AdminUsers)
		r.Get("/api/admin/users/export.csv", app.AdminUsersExport)
		r.Get("/api/admin/users/{id}", app.AdminUser)
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
		r.Get("/api/admin/article-stats", app.AdminArticleStats)
		r.Get("/api/admin/orders", app.AdminOrders)
		r.Post("/api/admin/orders/{id}/refund", app.AdminRefundOrder)
		r.Post("/api/admin/courses/{slug}/grant-by-email", app.AdminGrantByEmail)
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

// doNoCSRF — POST без заголовка X-CSRF-Token: так ходит sendBeacon,
// у которого нет доступа к cookie-заголовкам страницы.
func (c *client) doNoCSRF(method, path string, body any) (*http.Response, []byte) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, rdr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

type client struct {
	srv  *httptest.Server
	jar  *cookiejar.Jar
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

// TestCheckoutTariffPresets – для курсов с пресетами тарифов (zdorovaya-spina)
// Checkout берёт цену/название из tariffPresets, а не из courses.price_rub.
// Без ?tariff – 400 tariff_required; неизвестный tariff – 400 bad_tariff.
func TestCheckoutTariffPresets(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	price := 3990
	_, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "zdorovaya-spina", Title: "Здоровая спина", Kind: "paid",
		PriceRub: &price, IsPublished: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// helper: создать верифицированного юзера и залогинить
	makeVerified := func(email string) *client {
		c := newClient(srv)
		c.do("POST", "/api/auth/register", map[string]any{
			"email": email, "password": "password123", "consent_pd": true,
		})
		u, _ := repo.GetUserByEmail(ctx, email)
		_ = repo.MarkEmailVerified(ctx, u.ID)
		c.do("POST", "/api/auth/login", map[string]string{
			"email": email, "password": "password123",
		})
		return c
	}

	checkAmount := func(t *testing.T, body []byte, want int) {
		t.Helper()
		var resp struct {
			OrderID string `json:"order_id"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("parse body: %v (%s)", err, body)
		}
		oid, err := uuid.Parse(resp.OrderID)
		if err != nil {
			t.Fatalf("bad order_id: %v", err)
		}
		o, err := repo.GetOrder(ctx, oid)
		if err != nil {
			t.Fatalf("get order: %v", err)
		}
		if o.AmountRub != want {
			t.Fatalf("amount: want %d got %d", want, o.AmountRub)
		}
	}

	// 1) self → 3990
	c1 := makeVerified("t-self@b.ru")
	r, body := c1.do("POST", "/api/courses/zdorovaya-spina/checkout?tariff=self", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "fake-payment") {
		t.Fatalf("self: %d %s", r.StatusCode, body)
	}
	checkAmount(t, body, 3990)

	// 2) support → 12990
	c2 := makeVerified("t-sup@b.ru")
	r, body = c2.do("POST", "/api/courses/zdorovaya-spina/checkout?tariff=support", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "fake-payment") {
		t.Fatalf("support: %d %s", r.StatusCode, body)
	}
	checkAmount(t, body, 12990)

	// 3) без tariff → 400 tariff_required
	c3 := makeVerified("t-no@b.ru")
	r, body = c3.do("POST", "/api/courses/zdorovaya-spina/checkout", nil)
	if r.StatusCode != 400 || !strings.Contains(string(body), "tariff_required") {
		t.Fatalf("no tariff: %d %s", r.StatusCode, body)
	}

	// 4) неизвестный tariff → 400 bad_tariff
	c4 := makeVerified("t-bad@b.ru")
	r, body = c4.do("POST", "/api/courses/zdorovaya-spina/checkout?tariff=premium", nil)
	if r.StatusCode != 400 || !strings.Contains(string(body), "bad_tariff") {
		t.Fatalf("bad tariff: %d %s", r.StatusCode, body)
	}

	// 5) служебный tariff=test10 обычному юзеру → 400 bad_tariff
	c5 := makeVerified("t-test10@b.ru")
	r, body = c5.do("POST", "/api/courses/zdorovaya-spina/checkout?tariff=test10", nil)
	if r.StatusCode != 400 || !strings.Contains(string(body), "bad_tariff") {
		t.Fatalf("test10 as user: %d %s", r.StatusCode, body)
	}

	// 6) tariff=test10 админу → 200 и сумма 50 ₽ (минимум Продамуса)
	c6 := makeVerified("t-admin10@b.ru")
	ua, _ := repo.GetUserByEmail(ctx, "t-admin10@b.ru")
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, ua.ID)
	// перелогин, чтобы токен подхватил новую роль (роль читается из БД в Checkout,
	// но перелогин делает сценарий честным)
	c6.do("POST", "/api/auth/login", map[string]string{"email": "t-admin10@b.ru", "password": "password123"})
	r, body = c6.do("POST", "/api/courses/zdorovaya-spina/checkout?tariff=test10", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "fake-payment") {
		t.Fatalf("test10 as admin: %d %s", r.StatusCode, body)
	}
	checkAmount(t, body, 50)
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
	if !strings.Contains(string(body), "\"created\":true") || !strings.Contains(string(body), "\"access_granted\":true") {
		t.Fatalf("missing created/access_granted flags: %s", body)
	}
	u, err := repo.GetUserByEmail(ctx, "quick@b.ru")
	if err != nil {
		t.Fatalf("user not created: %v", err)
	}
	// Доступ открыт сразу: почта ещё не подтверждена, но курс выдан и сессия есть.
	if u.EmailVerifiedAt != nil {
		t.Fatalf("email must NOT be verified before clicking the link")
	}
	if has, _ := repo.HasEnrollment(ctx, u.ID, freeID); !has {
		t.Fatalf("enrollment in published free course must be granted right away")
	}
	r, body = c.do("GET", "/api/me", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "quick@b.ru") ||
		!strings.Contains(string(body), "\"can_fix_email\":true") {
		t.Fatalf("quick-signup must authenticate: %d %s", r.StatusCode, body)
	}
	r, body = c.do("GET", "/api/me/courses", nil)
	if !strings.Contains(string(body), "qs-free") || strings.Contains(string(body), "qs-paid") || strings.Contains(string(body), "qs-free-draft") {
		t.Fatalf("expected only published free in /api/me/courses: %s", body)
	}

	// Ссылка «Открыть курс» из письма подтверждает почту и логинит (в новом браузере).
	var qsResp struct {
		VerifyLinkDev string `json:"verify_link_dev"`
	}
	r, body = newClient(srv).do("POST", "/api/auth/quick-signup", map[string]any{
		"email": "quick2@b.ru", "name": "Q2", "consent_pd": true})
	if err := json.Unmarshal(body, &qsResp); err != nil || !strings.Contains(qsResp.VerifyLinkDev, "token=") {
		t.Fatalf("verify_link_dev missing: %s", body)
	}
	token := qsResp.VerifyLinkDev[strings.Index(qsResp.VerifyLinkDev, "token=")+len("token="):]
	c3 := newClient(srv)
	r, body = c3.do("POST", "/api/auth/verify-email", map[string]any{"token": token})
	if r.StatusCode != 200 {
		t.Fatalf("verify-email: %d %s", r.StatusCode, body)
	}
	u2, _ := repo.GetUserByEmail(ctx, "quick2@b.ru")
	if u2.EmailVerifiedAt == nil {
		t.Fatalf("email should be verified after verify-email")
	}
	r, body = c3.do("GET", "/api/me", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "quick2@b.ru") ||
		!strings.Contains(string(body), "\"can_fix_email\":false") {
		t.Fatalf("me after verify: %d %s", r.StatusCode, body)
	}
}

func TestFixEmailAfterQuickSignup(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	c := newClient(srv)
	r, body := c.do("POST", "/api/auth/quick-signup", map[string]any{
		"email": "typo@gmial.com", "name": "T", "consent_pd": true})
	if r.StatusCode != 201 {
		t.Fatalf("quick-signup: %d %s", r.StatusCode, body)
	}
	var first struct {
		PasswordDev string `json:"password_dev"`
	}
	_ = json.Unmarshal(body, &first)

	// Анониму нельзя.
	r, _ = newClient(srv).do("POST", "/api/auth/fix-email", map[string]any{"email": "x@gmail.com"})
	if r.StatusCode != 401 {
		t.Fatalf("anon fix-email: expected 401, got %d", r.StatusCode)
	}
	// Кривой адрес.
	r, _ = c.do("POST", "/api/auth/fix-email", map[string]any{"email": "nope"})
	if r.StatusCode != 400 {
		t.Fatalf("bad email: expected 400, got %d", r.StatusCode)
	}
	// Занятый адрес.
	newClient(srv).do("POST", "/api/auth/quick-signup", map[string]any{
		"email": "busy@gmail.com", "name": "B", "consent_pd": true})
	r, _ = c.do("POST", "/api/auth/fix-email", map[string]any{"email": "busy@gmail.com"})
	if r.StatusCode != 409 {
		t.Fatalf("taken email: expected 409, got %d", r.StatusCode)
	}
	// Исправляем.
	r, body = c.do("POST", "/api/auth/fix-email", map[string]any{"email": " Typo@Gmail.com "})
	if r.StatusCode != 200 {
		t.Fatalf("fix-email: %d %s", r.StatusCode, body)
	}
	var fixed struct {
		PasswordDev string `json:"password_dev"`
	}
	_ = json.Unmarshal(body, &fixed)
	if fixed.PasswordDev == "" || fixed.PasswordDev == first.PasswordDev {
		t.Fatalf("new password expected after fix-email: %s", body)
	}
	if _, err := repo.GetUserByEmail(ctx, "typo@gmial.com"); err == nil {
		t.Fatalf("old email must be gone")
	}
	// Вход с новым адресом и новым паролем.
	r, _ = newClient(srv).do("POST", "/api/auth/login", map[string]string{"email": "typo@gmail.com", "password": fixed.PasswordDev})
	if r.StatusCode != 200 {
		t.Fatalf("login with fixed email: %d", r.StatusCode)
	}
	// После подтверждения почты менять адрес нельзя.
	u, _ := repo.GetUserByEmail(ctx, "typo@gmail.com")
	_ = repo.MarkEmailVerified(ctx, u.ID)
	r, _ = c.do("POST", "/api/auth/fix-email", map[string]any{"email": "other@gmail.com"})
	if r.StatusCode != 409 {
		t.Fatalf("verified email must be locked: got %d", r.StatusCode)
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
	// don't warm up CSRF – bypass by raw request
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

	// PATCH module – rename + reorder
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
	// PATCH module – empty title rejected
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
// неизвестного файла. Закрытие /files/*.pdf – критичная safety-правка.
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
// Бэк должен возвращать 400 consent_pd_required, а в БД – никакого user не создавать.
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
// consent_marketing_at – NULL, если маркетинговое согласие не дано.
// При consent_marketing=true – заполнено.
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

// Helper: открывает второй pgxpool – нужен в тестах, которые делают прямые SELECT'ы
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

// TestLeadsFlow – публичная заявка + админский флоу.
// POST /api/leads: валидация (имя/контакт/согласие), 201 при успехе.
// GET/PATCH /api/admin/leads – только для admin; смена статуса пишется.
func TestLeadsFlow(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()

	anon := newClient(srv)

	// без согласия → 400
	r, body := anon.do("POST", "/api/leads", map[string]any{
		"name": "Иван", "contact": "ivan@x.ru", "source": "coaching"})
	if r.StatusCode != 400 || !strings.Contains(string(body), "consent_pd_required") {
		t.Fatalf("no consent: %d %s", r.StatusCode, body)
	}

	// без имени/контакта → 400
	r, body = anon.do("POST", "/api/leads", map[string]any{
		"name": "", "contact": "ivan@x.ru", "consent_pd": true})
	if r.StatusCode != 400 || !strings.Contains(string(body), "invalid_input") {
		t.Fatalf("no name: %d %s", r.StatusCode, body)
	}

	// валидная заявка → 201, неизвестный source нормализуется в other
	r, body = anon.do("POST", "/api/leads", map[string]any{
		"name": "Иван", "contact": "@ivan_tg", "message": "Болит спина",
		"source": "hacker", "consent_pd": true})
	if r.StatusCode != 201 {
		t.Fatalf("create lead: %d %s", r.StatusCode, body)
	}
	leads, err := repo.ListLeads(ctx, 10)
	if err != nil || len(leads) != 1 {
		t.Fatalf("list leads: %v %d", err, len(leads))
	}
	if leads[0].Source != "other" || leads[0].Status != "new" || leads[0].Contact != "@ivan_tg" {
		t.Fatalf("lead fields: %+v", leads[0])
	}

	// заявка со страницы «Точка старта» сохраняет свой источник
	r, body = anon.do("POST", "/api/leads", map[string]any{
		"name": "Пётр", "contact": "petr@x.ru", "source": "start", "consent_pd": true})
	if r.StatusCode != 201 {
		t.Fatalf("create start lead: %d %s", r.StatusCode, body)
	}
	leads, err = repo.ListLeads(ctx, 10)
	if err != nil || len(leads) != 2 {
		t.Fatalf("list leads after start: %v %d", err, len(leads))
	}
	if leads[0].Source != "start" {
		t.Fatalf("start lead source: %+v", leads[0])
	}

	// обычному юзеру админский список недоступен
	user := newClient(srv)
	user.do("POST", "/api/auth/register", map[string]any{
		"email": "lead-user@b.ru", "password": "password123", "consent_pd": true})
	user.do("POST", "/api/auth/login", map[string]string{
		"email": "lead-user@b.ru", "password": "password123"})
	if r, _ = user.do("GET", "/api/admin/leads", nil); r.StatusCode == 200 {
		t.Fatalf("admin leads must be closed for users, got 200")
	}

	// админ: видит заявку и меняет статус
	adm := newClient(srv)
	adm.do("POST", "/api/auth/register", map[string]any{
		"email": "lead-adm@b.ru", "password": "password123", "consent_pd": true})
	ua, _ := repo.GetUserByEmail(ctx, "lead-adm@b.ru")
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, ua.ID)
	adm.do("POST", "/api/auth/login", map[string]string{
		"email": "lead-adm@b.ru", "password": "password123"})
	r, body = adm.do("GET", "/api/admin/leads", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "@ivan_tg") {
		t.Fatalf("admin list: %d %s", r.StatusCode, body)
	}
	r, body = adm.do("PATCH", "/api/admin/leads/"+leads[0].ID.String(), map[string]string{"status": "done"})
	if r.StatusCode != 200 {
		t.Fatalf("patch lead: %d %s", r.StatusCode, body)
	}
	leads, _ = repo.ListLeads(ctx, 10)
	if leads[0].Status != "done" {
		t.Fatalf("status not updated: %+v", leads[0])
	}
	// невалидный статус → 400
	r, _ = adm.do("PATCH", "/api/admin/leads/"+leads[0].ID.String(), map[string]string{"status": "hacked"})
	if r.StatusCode != 400 {
		t.Fatalf("bad status must be 400, got %d", r.StatusCode)
	}
}

// TestAdminUserCard – карточка пользователя в админке: профиль с согласиями,
// доступы с прогрессом по урокам, заказы. Доступна только админу.
func TestAdminUserCard(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()

	// курс с двумя уроками
	cid, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "card-course", Title: "Курс для карточки", Kind: "free", IsPublished: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	l1, _ := repo.CreateLesson(ctx, db.LessonInput{CourseID: cid, Title: "Урок 1", Slug: "cl1", ContentMD: "x", SortOrder: 1})
	_, _ = repo.CreateLesson(ctx, db.LessonInput{CourseID: cid, Title: "Урок 2", Slug: "cl2", ContentMD: "x", SortOrder: 2})

	// пользователь: регистрация, доступ, прогресс по одному уроку, заказ
	uc := newClient(srv)
	uc.do("POST", "/api/auth/register", map[string]any{
		"email": "card@b.ru", "password": "password123", "name": "Карточкин", "consent_pd": true})
	u, _ := repo.GetUserByEmail(ctx, "card@b.ru")
	if err := repo.Grant(ctx, u.ID, cid, "free", nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertProgress(ctx, u.ID, l1, true, 120); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateOrder(ctx, u.ID, cid, 990); err != nil {
		t.Fatal(err)
	}

	// обычному пользователю карточка недоступна
	if r, _ := uc.do("GET", "/api/admin/users/"+u.ID.String(), nil); r.StatusCode == 200 {
		t.Fatalf("user card must be admin-only, got 200")
	}

	// админ видит всё
	adm := newClient(srv)
	adm.do("POST", "/api/auth/register", map[string]any{
		"email": "card-adm@b.ru", "password": "password123", "consent_pd": true})
	ua, _ := repo.GetUserByEmail(ctx, "card-adm@b.ru")
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, ua.ID)
	adm.do("POST", "/api/auth/login", map[string]string{"email": "card-adm@b.ru", "password": "password123"})

	r, body := adm.do("GET", "/api/admin/users/"+u.ID.String(), nil)
	if r.StatusCode != 200 {
		t.Fatalf("admin user card: %d %s", r.StatusCode, body)
	}
	s := string(body)
	for _, want := range []string{
		"card@b.ru", "Карточкин",
		"\"granted_by\":\"free\"",
		"\"lessons_total\":2", "\"lessons_done\":1", "\"progress_pct\":50",
		"Урок 1", "Урок 2",
		"\"order_num\"", "\"amount_rub\":990",
		"consent_pd_at",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("card missing %q: %s", want, s)
		}
	}
	// согласие ПД должно быть заполнено (не null)
	if strings.Contains(s, "\"consent_pd_at\":null") {
		t.Fatalf("consent_pd_at must be set after register: %s", s)
	}
}

// TestAdminUsersFilters – фильтры списка пользователей: по курсу и по
// статусу подтверждения email.
func TestAdminUsersFilters(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()

	cid, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "flt-course", Title: "Фильтр-курс", Kind: "free", IsPublished: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// A: с курсом и подтверждённым email; B: без курса, не подтверждён
	ca := newClient(srv)
	ca.do("POST", "/api/auth/register", map[string]any{"email": "flt-a@b.ru", "password": "password123", "consent_pd": true})
	ua, _ := repo.GetUserByEmail(ctx, "flt-a@b.ru")
	_ = repo.MarkEmailVerified(ctx, ua.ID)
	_ = repo.Grant(ctx, ua.ID, cid, "free", nil)
	cb := newClient(srv)
	cb.do("POST", "/api/auth/register", map[string]any{"email": "flt-b@b.ru", "password": "password123", "consent_pd": true})

	adm := newClient(srv)
	adm.do("POST", "/api/auth/register", map[string]any{"email": "flt-adm@b.ru", "password": "password123", "consent_pd": true})
	uadm, _ := repo.GetUserByEmail(ctx, "flt-adm@b.ru")
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, uadm.ID)
	adm.do("POST", "/api/auth/login", map[string]string{"email": "flt-adm@b.ru", "password": "password123"})

	// по курсу: только A
	r, body := adm.do("GET", "/api/admin/users?course=flt-course", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "flt-a@b.ru") || strings.Contains(string(body), "flt-b@b.ru") {
		t.Fatalf("course filter: %d %s", r.StatusCode, body)
	}
	// не подтверждённые: B есть, A нет
	r, body = adm.do("GET", "/api/admin/users?verified=0", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "flt-b@b.ru") || strings.Contains(string(body), "flt-a@b.ru") {
		t.Fatalf("verified=0 filter: %d %s", r.StatusCode, body)
	}
	// комбинация: verified=1 + course → только A
	r, body = adm.do("GET", "/api/admin/users?verified=1&course=flt-course", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "flt-a@b.ru") || strings.Contains(string(body), "flt-b@b.ru") {
		t.Fatalf("combo filter: %d %s", r.StatusCode, body)
	}
}

// TestAdminUsersExportCSV – CSV-выгрузка для рассылок: только пользователи
// с согласием на маркетинг, admin-only, корректный Content-Type.
func TestAdminUsersExportCSV(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()

	// A согласен на рассылку, B – нет
	ca := newClient(srv)
	ca.do("POST", "/api/auth/register", map[string]any{
		"email": "exp-a@b.ru", "password": "password123", "name": "Экспортов",
		"consent_pd": true, "consent_marketing": true})
	cb := newClient(srv)
	cb.do("POST", "/api/auth/register", map[string]any{
		"email": "exp-b@b.ru", "password": "password123", "consent_pd": true})

	// не-админу закрыто
	ca.do("POST", "/api/auth/login", map[string]string{"email": "exp-a@b.ru", "password": "password123"})
	if r, _ := ca.do("GET", "/api/admin/users/export.csv", nil); r.StatusCode == 200 {
		t.Fatalf("export must be admin-only, got 200")
	}

	adm := newClient(srv)
	adm.do("POST", "/api/auth/register", map[string]any{"email": "exp-adm@b.ru", "password": "password123", "consent_pd": true})
	uadm, _ := repo.GetUserByEmail(ctx, "exp-adm@b.ru")
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, uadm.ID)
	adm.do("POST", "/api/auth/login", map[string]string{"email": "exp-adm@b.ru", "password": "password123"})

	r, body := adm.do("GET", "/api/admin/users/export.csv", nil)
	if r.StatusCode != 200 {
		t.Fatalf("export: %d %s", r.StatusCode, body)
	}
	if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("content-type: %s", ct)
	}
	s := string(body)
	if !strings.Contains(s, "exp-a@b.ru") || !strings.Contains(s, "Экспортов") {
		t.Fatalf("csv missing subscriber: %s", s)
	}
	if strings.Contains(s, "exp-b@b.ru") {
		t.Fatalf("csv must contain ONLY marketing-consented users: %s", s)
	}
}

// TestAdminActivity – журнал занятий: admin-only, показывает касание урока
// с email/названиями, фильтр по курсу работает.
func TestAdminActivity(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()

	cid, _ := repo.CreateCourse(ctx, db.CourseInput{Slug: "act-c", Title: "Журнальный курс", Kind: "free", IsPublished: true})
	lid, _ := repo.CreateLesson(ctx, db.LessonInput{CourseID: cid, Title: "Журнальный урок", Slug: "act-l", ContentMD: "x", SortOrder: 1})

	uc := newClient(srv)
	uc.do("POST", "/api/auth/register", map[string]any{"email": "act@b.ru", "password": "password123", "name": "Журналов", "consent_pd": true})
	uc.do("POST", "/api/auth/login", map[string]string{"email": "act@b.ru", "password": "password123"})
	// просмотр через реальный эндпоинт – он пишет и прогресс, и журнал сессий
	r0, body0 := uc.do("POST", "/api/lessons/"+lid.String()+"/progress",
		map[string]any{"completed": true, "last_position_sec": 300})
	if r0.StatusCode != 200 {
		t.Fatalf("post progress: %d %s", r0.StatusCode, body0)
	}

	// не-админу журнал закрыт
	if r, _ := uc.do("GET", "/api/admin/activity", nil); r.StatusCode == 200 {
		t.Fatalf("activity must be admin-only")
	}

	adm := newClient(srv)
	adm.do("POST", "/api/auth/register", map[string]any{"email": "act-adm@b.ru", "password": "password123", "consent_pd": true})
	ua, _ := repo.GetUserByEmail(ctx, "act-adm@b.ru")
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, ua.ID)
	adm.do("POST", "/api/auth/login", map[string]string{"email": "act-adm@b.ru", "password": "password123"})

	r, body := adm.do("GET", "/api/admin/activity?course=act-c", nil)
	s := string(body)
	if r.StatusCode != 200 || !strings.Contains(s, "act@b.ru") || !strings.Contains(s, "Журнальный урок") ||
		!strings.Contains(s, "\"completed\":true") || !strings.Contains(s, "started_at") {
		t.Fatalf("activity: %d %s", r.StatusCode, s)
	}
	// фильтр по несуществующему курсу – пусто
	r, body = adm.do("GET", "/api/admin/activity?course=no-such", nil)
	if r.StatusCode != 200 || strings.Contains(string(body), "act@b.ru") {
		t.Fatalf("activity filter: %d %s", r.StatusCode, body)
	}
}

// TestResendVerification – повторное письмо подтверждения: 200 и новый токен
// для неподтверждённого; для неизвестного email тоже 200 (не раскрываем базу).
func TestResendVerification(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()

	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{
		"email": "rv@b.ru", "password": "password123", "consent_pd": true})
	u, _ := repo.GetUserByEmail(ctx, "rv@b.ru")

	var before int
	_ = repo.Pool.QueryRow(ctx, `SELECT count(*) FROM email_verification_tokens WHERE user_id=$1`, u.ID).Scan(&before)

	r, body := c.do("POST", "/api/auth/resend-verification", map[string]string{"email": "rv@b.ru"})
	if r.StatusCode != 200 {
		t.Fatalf("resend: %d %s", r.StatusCode, body)
	}
	var after int
	_ = repo.Pool.QueryRow(ctx, `SELECT count(*) FROM email_verification_tokens WHERE user_id=$1`, u.ID).Scan(&after)
	if after != before+1 {
		t.Fatalf("token not created: before=%d after=%d", before, after)
	}

	// неизвестный email → всё равно 200
	r, _ = c.do("POST", "/api/auth/resend-verification", map[string]string{"email": "ghost@b.ru"})
	if r.StatusCode != 200 {
		t.Fatalf("resend unknown: %d", r.StatusCode)
	}
}

// TestSiteSettings – флаги сайта (salut_visible): публичное чтение,
// изменение только админом, белый список ключей, запись в audit_log.
func TestSiteSettings(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()

	// публичное чтение без авторизации, дефолт – выключено
	c := newClient(srv)
	r, body := c.do("GET", "/api/settings", nil)
	if r.StatusCode != 200 {
		t.Fatalf("public settings: %d %s", r.StatusCode, body)
	}
	var got map[string]bool
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("bad json: %s", body)
	}
	if v, ok := got["salut_visible"]; !ok || v {
		t.Fatalf("default salut_visible must be present and false: %s", body)
	}

	// обычный пользователь менять не может
	c.do("POST", "/api/auth/register", map[string]any{
		"email": "st-u@b.ru", "password": "password123", "consent_pd": true})
	c.do("POST", "/api/auth/login", map[string]string{"email": "st-u@b.ru", "password": "password123"})
	if r, _ := c.do("PATCH", "/api/admin/settings", map[string]bool{"salut_visible": true}); r.StatusCode == 200 {
		t.Fatalf("settings must be admin-only, got 200")
	}

	adm := newClient(srv)
	adm.do("POST", "/api/auth/register", map[string]any{
		"email": "st-adm@b.ru", "password": "password123", "consent_pd": true})
	uadm, _ := repo.GetUserByEmail(ctx, "st-adm@b.ru")
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, uadm.ID)
	adm.do("POST", "/api/auth/login", map[string]string{"email": "st-adm@b.ru", "password": "password123"})

	// неизвестный ключ отклоняется
	if r, body := adm.do("PATCH", "/api/admin/settings", map[string]bool{"whatever": true}); r.StatusCode != 400 {
		t.Fatalf("unknown key must be 400: %d %s", r.StatusCode, body)
	}
	// пустое тело отклоняется
	if r, _ := adm.do("PATCH", "/api/admin/settings", map[string]bool{}); r.StatusCode != 400 {
		t.Fatalf("empty body must be 400: %d", r.StatusCode)
	}

	// включаем
	r, body = adm.do("PATCH", "/api/admin/settings", map[string]bool{"salut_visible": true})
	if r.StatusCode != 200 || !strings.Contains(string(body), `"salut_visible":true`) {
		t.Fatalf("enable: %d %s", r.StatusCode, body)
	}
	// публичный эндпоинт отражает изменение
	r, body = c.do("GET", "/api/settings", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), `"salut_visible":true`) {
		t.Fatalf("public read after enable: %d %s", r.StatusCode, body)
	}
	// действие записано в аудит
	var n int
	_ = repo.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='settings_update'`).Scan(&n)
	if n != 1 {
		t.Fatalf("audit_log entries = %d, want 1", n)
	}

	// выключаем обратно
	r, body = adm.do("PATCH", "/api/admin/settings", map[string]bool{"salut_visible": false})
	if r.StatusCode != 200 || !strings.Contains(string(body), `"salut_visible":false`) {
		t.Fatalf("disable: %d %s", r.StatusCode, body)
	}
}

// TestUnsubscribe – отписка по ссылке из письма: подпись обязательна,
// согласие на маркетинг снимается, согласие на обработку ПД остаётся,
// one-click POST работает без CSRF-заголовка (его шлёт почтовый клиент).
func TestUnsubscribe(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()

	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{
		"email": "unsub@b.ru", "password": "password123",
		"consent_pd": true, "consent_marketing": true})
	u, err := repo.GetUserByEmail(ctx, "unsub@b.ru")
	if err != nil {
		t.Fatal(err)
	}

	marketing := func() bool {
		var ts *time.Time
		_ = repo.Pool.QueryRow(ctx, `SELECT consent_marketing_at FROM users WHERE id=$1`, u.ID).Scan(&ts)
		return ts != nil
	}
	if !marketing() {
		t.Fatal("согласие на маркетинг должно стоять после регистрации")
	}

	// редиректы не проходим – проверяем сам ответ 303
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	get := func(url string) *http.Response {
		resp, err := noRedirect.Get(srv.URL + url)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}

	token := handlers.UnsubscribeToken(cfg.JWTSecret, u.ID.String())
	q := "?u=" + u.ID.String() + "&t=" + token

	// GET по ссылке из старых писем ничего не меняет – ведёт на страницу подтверждения
	r := get("/api/unsubscribe" + q)
	if r.StatusCode != 303 || r.Header.Get("Location") != "/unsubscribe"+q {
		t.Fatalf("GET должен вести на /unsubscribe: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	if !marketing() {
		t.Fatal("GET не должен отписывать – только страница подтверждения")
	}

	c2 := newClient(srv)
	// статус: подделанная подпись – 400, валидная – подписан
	if r, _ := c2.do("GET", "/api/unsubscribe/status?u="+u.ID.String()+"&t=deadbeef", nil); r.StatusCode != 400 {
		t.Fatalf("status с плохой подписью: %d", r.StatusCode)
	}
	if r, body := c2.do("GET", "/api/unsubscribe/status"+q, nil); r.StatusCode != 200 || !strings.Contains(string(body), `"subscribed":true`) {
		t.Fatalf("status: %d %s", r.StatusCode, body)
	}

	// подтверждение со страницы: причина + комментарий
	r, body := c2.do("POST", "/api/unsubscribe"+q, map[string]string{"reason": "too_often", "comment": "  много писем  "})
	if r.StatusCode != 200 {
		t.Fatalf("отписка со страницы: %d %s", r.StatusCode, body)
	}
	if marketing() {
		t.Fatal("согласие на маркетинг должно быть снято")
	}
	var reason, comment string
	_ = repo.Pool.QueryRow(ctx, `SELECT reason, comment FROM unsubscribe_reasons WHERE user_id=$1`, u.ID).Scan(&reason, &comment)
	if reason != "too_often" || comment != "много писем" {
		t.Fatalf("причина не сохранилась: %q %q", reason, comment)
	}
	// неизвестная причина не пишется
	c2.do("POST", "/api/unsubscribe"+q, map[string]string{"reason": "<script>"})
	var n int
	_ = repo.Pool.QueryRow(ctx, `SELECT count(*) FROM unsubscribe_reasons`).Scan(&n)
	if n != 1 {
		t.Fatalf("неизвестная причина записалась: %d", n)
	}

	// «передумал» – подписка возвращается тем же токеном
	if r, _ := c2.do("POST", "/api/unsubscribe/undo?u="+u.ID.String()+"&t=nope", nil); r.StatusCode != 400 {
		t.Fatalf("undo с плохой подписью: %d", r.StatusCode)
	}
	if r, _ := c2.do("POST", "/api/unsubscribe/undo"+q, nil); r.StatusCode != 200 || !marketing() {
		t.Fatalf("undo должен вернуть подписку: %d", r.StatusCode)
	}
	c2.do("POST", "/api/unsubscribe"+q, nil)
	if marketing() {
		t.Fatal("повторная отписка должна сработать")
	}

	// согласие на обработку ПД трогать нельзя
	var pd *time.Time
	_ = repo.Pool.QueryRow(ctx, `SELECT consent_pd_at FROM users WHERE id=$1`, u.ID).Scan(&pd)
	if pd == nil {
		t.Fatal("consent_pd_at не должен сниматься при отписке")
	}

	// one-click POST из почтового клиента: без X-CSRF-Token
	req, _ := http.NewRequest("POST", srv.URL+"/api/unsubscribe?u="+u.ID.String()+"&t="+token, nil)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("one-click POST без CSRF должен проходить, получил %d", resp.StatusCode)
	}

	// one-click с плохой подписью – 400
	req, _ = http.NewRequest("POST", srv.URL+"/api/unsubscribe?u="+u.ID.String()+"&t=nope", nil)
	resp2, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 400 {
		t.Fatalf("ожидал 400 на плохую подпись, получил %d", resp2.StatusCode)
	}
}

// TestEmailPixel – счётчик открытий писем: картинка отдаётся всегда,
// но открытие засчитывается только с валидной подписью.
func TestEmailPixel(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()

	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{
		"email": "pix@b.ru", "password": "password123", "consent_pd": true})
	u, err := repo.GetUserByEmail(ctx, "pix@b.ru")
	if err != nil {
		t.Fatal(err)
	}
	opens := func() int {
		var n int
		_ = repo.Pool.QueryRow(ctx, `SELECT count(*) FROM email_opens WHERE user_id=$1`, u.ID).Scan(&n)
		return n
	}

	// плохая подпись: картинка есть, открытие не засчитано
	r, body := c.do("GET", "/api/pixel.gif?u="+u.ID.String()+"&c=test&t=bad", nil)
	if r.StatusCode != 200 {
		t.Fatalf("пиксель должен отдаваться всегда: %d", r.StatusCode)
	}
	if ct := r.Header.Get("Content-Type"); ct != "image/gif" {
		t.Fatalf("content-type: %s", ct)
	}
	if len(body) < 30 || string(body[:3]) != "GIF" {
		t.Fatalf("это не GIF: %d байт", len(body))
	}
	if opens() != 0 {
		t.Fatal("подделанная подпись не должна засчитываться")
	}

	// валидная подпись
	tok := handlers.PixelToken(cfg.JWTSecret, "test", u.ID.String())
	if r, _ = c.do("GET", "/api/pixel.gif?u="+u.ID.String()+"&c=test&t="+tok, nil); r.StatusCode != 200 {
		t.Fatalf("валидный пиксель: %d", r.StatusCode)
	}
	if opens() != 1 {
		t.Fatalf("открытие должно быть записано, в базе %d", opens())
	}
	// повторное открытие – вторая строка (перечитал письмо)
	c.do("GET", "/api/pixel.gif?u="+u.ID.String()+"&c=test&t="+tok, nil)
	if opens() != 2 {
		t.Fatalf("повторное открытие должно писаться отдельно, в базе %d", opens())
	}

	// сводка – только админу
	if r, _ := c.do("GET", "/api/admin/email-opens", nil); r.StatusCode == 200 {
		t.Fatal("сводка должна быть закрыта от обычного пользователя")
	}
	adm := newClient(srv)
	adm.do("POST", "/api/auth/register", map[string]any{
		"email": "pix-adm@b.ru", "password": "password123", "consent_pd": true})
	ua, _ := repo.GetUserByEmail(ctx, "pix-adm@b.ru")
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, ua.ID)
	adm.do("POST", "/api/auth/login", map[string]string{"email": "pix-adm@b.ru", "password": "password123"})
	r, body = adm.do("GET", "/api/admin/email-opens", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), `"people":1`) {
		t.Fatalf("сводка: %d %s", r.StatusCode, body)
	}
}

// TestProdamusWebhookOrderNumCarriesUUID – реальный формат Продамуса:
// в `order_id` приходит ЕГО внутренний номер, а наш идентификатор заказа
// возвращается в `order_num`. Из-за этого оплаты не привязывались к заказам
// и доступ приходилось выдавать вручную (баг найден 17.09.2026).
func TestProdamusWebhookOrderNumCarriesUUID(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()
	uid, _ := repo.CreateUser(ctx, "swap@b.ru", "x", "Swap", "user")
	cid, _ := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "swap-course", Title: "Swap", Kind: "paid", PriceRub: ptrInt(3990), IsPublished: true})
	o, _ := repo.CreateOrder(ctx, uid, cid, 3990)

	body := map[string]any{
		"order_id":       "48479470", // внутренний номер Продамуса
		"order_num":      o.ID.String(),
		"payment_status": "success",
		"sum":            "3990.00",
	}
	sig, _ := prodamus.Sign(cfg.ProdamusSecret, body)
	form := url.Values{}
	for k, v := range body {
		form.Set(k, v.(string))
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/webhooks/prodamus", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sign", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	o2, _ := repo.GetOrder(ctx, o.ID)
	if o2.Status != "paid" {
		t.Fatalf("заказ должен стать paid, а он %s", o2.Status)
	}
	if has, _ := repo.HasEnrollment(ctx, uid, cid); !has {
		t.Fatal("доступ к курсу должен выдаться автоматически")
	}
}

// TestProdamusWebhookHumanOrderNum – второй формат: наш человекочитаемый
// номер заказа (orders.order_num) в поле order_num.
func TestProdamusWebhookHumanOrderNum(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()
	uid, _ := repo.CreateUser(ctx, "num@b.ru", "x", "Num", "user")
	cid, _ := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "num-course", Title: "Num", Kind: "paid", PriceRub: ptrInt(100), IsPublished: true})
	o, _ := repo.CreateOrder(ctx, uid, cid, 100)

	body := map[string]any{
		"order_id":       "99999999",
		"order_num":      strconv.FormatInt(o.OrderNum, 10),
		"payment_status": "success",
	}
	sig, _ := prodamus.Sign(cfg.ProdamusSecret, body)
	form := url.Values{}
	for k, v := range body {
		form.Set(k, v.(string))
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/webhooks/prodamus", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sign", sig)
	resp, _ := http.DefaultClient.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	o2, _ := repo.GetOrder(ctx, o.ID)
	if o2.Status != "paid" {
		t.Fatalf("заказ по человекочитаемому номеру должен стать paid, а он %s", o2.Status)
	}
}

// TestProdamusWebhookNoOrderStaysSafe – оплата по ссылке, выставленной вручную:
// заказа нет, ничего не выдаём, вебхук помечаем как no_order.
func TestProdamusWebhookNoOrderStaysSafe(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()

	body := map[string]any{
		"order_id":       "48479999",
		"order_num":      "Светлана Иванова",
		"customer_email": "manual@b.ru",
		"payment_status": "success",
		"sum":            "6500.00",
	}
	sig, _ := prodamus.Sign(cfg.ProdamusSecret, body)
	form := url.Values{}
	for k, v := range body {
		form.Set(k, v.(string))
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/webhooks/prodamus", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sign", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("Продамусу всё равно отвечаем 200, а ответили %d", resp.StatusCode)
	}
	var errText string
	_ = repo.Pool.QueryRow(ctx,
		`SELECT coalesce(processing_error,'') FROM payment_webhooks ORDER BY received_at DESC LIMIT 1`).Scan(&errText)
	if errText != "no_order" {
		t.Fatalf("ожидал пометку no_order, получил %q", errText)
	}
}

// TestCampaignsFlow – рассылки из админки: группы считаются только по
// согласившимся, черновик фиксирует получателей, старт/пауза меняют статус,
// а отписавшийся после старта в очередь не попадает.
func TestCampaignsFlow(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()

	// A и B согласны на рассылку, C – нет
	for _, e := range []string{"c-a@b.ru", "c-b@b.ru"} {
		c := newClient(srv)
		c.do("POST", "/api/auth/register", map[string]any{
			"email": e, "password": "password123", "name": "Аня Тест",
			"consent_pd": true, "consent_marketing": true})
	}
	cc := newClient(srv)
	cc.do("POST", "/api/auth/register", map[string]any{
		"email": "c-c@b.ru", "password": "password123", "consent_pd": true})

	adm := newClient(srv)
	adm.do("POST", "/api/auth/register", map[string]any{
		"email": "c-adm@b.ru", "password": "password123", "consent_pd": true})
	uadm, _ := repo.GetUserByEmail(ctx, "c-adm@b.ru")
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, uadm.ID)
	adm.do("POST", "/api/auth/login", map[string]string{"email": "c-adm@b.ru", "password": "password123"})

	// группы: в «все подписанные» ровно двое
	r, body := adm.do("GET", "/api/admin/segments", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), `"key":"all"`) {
		t.Fatalf("segments: %d %s", r.StatusCode, body)
	}
	var segResp struct {
		Segments []struct {
			Key   string `json:"key"`
			Count int    `json:"count"`
		} `json:"segments"`
	}
	_ = json.Unmarshal(body, &segResp)
	for _, s := range segResp.Segments {
		if s.Key == "all" && s.Count != 2 {
			t.Fatalf("в группе «все подписанные» должно быть 2, а не %d", s.Count)
		}
	}

	// не-админу создавать рассылку нельзя
	if r, _ := cc.do("POST", "/api/admin/campaigns", map[string]any{
		"name": "x", "subject": "x", "body": "x", "segment": "all"}); r.StatusCode == 201 {
		t.Fatal("рассылки должны быть закрыты от обычного пользователя")
	}

	// плохая группа → 400
	if r, _ := adm.do("POST", "/api/admin/campaigns", map[string]any{
		"name": "x", "subject": "x", "body": "x", "segment": "нет-такой"}); r.StatusCode != 400 {
		t.Fatalf("неизвестная группа должна давать 400, а дала %d", r.StatusCode)
	}
	// пустые поля → 400
	if r, _ := adm.do("POST", "/api/admin/campaigns", map[string]any{
		"name": "", "subject": "x", "body": "x", "segment": "all"}); r.StatusCode != 400 {
		t.Fatalf("пустое имя должно давать 400")
	}

	// создаём
	r, body = adm.do("POST", "/api/admin/campaigns", map[string]any{
		"name": "Тестовая", "subject": "Привет", "body": "Первый абзац.\n\nВторой абзац.",
		"segment": "all", "daily_limit": 10})
	if r.StatusCode != 201 {
		t.Fatalf("создание: %d %s", r.StatusCode, body)
	}
	var created struct {
		ID    string `json:"id"`
		Total int    `json:"total"`
	}
	_ = json.Unmarshal(body, &created)
	if created.Total != 2 {
		t.Fatalf("в рассылку должно попасть 2 адресата, попало %d", created.Total)
	}

	// слишком короткий интервал подтягивается к минимуму (30 секунд)
	r, body = adm.do("POST", "/api/admin/campaigns", map[string]any{
		"name": "Частая", "subject": "x", "body": "x", "segment": "all", "pause_sec": 1})
	if r.StatusCode != 201 {
		t.Fatalf("создание с коротким интервалом: %d %s", r.StatusCode, body)
	}
	var fast struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &fast)
	fastID, _ := uuid.Parse(fast.ID)
	fc, err := repo.GetCampaign(ctx, fastID)
	if err != nil {
		t.Fatal(err)
	}
	if fc.PauseSec != 30 {
		t.Fatalf("интервал должен подтянуться к 30 секундам, а стал %d", fc.PauseSec)
	}

	// карточка рассылки
	r, body = adm.do("GET", "/api/admin/campaigns/"+created.ID, nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), "c-a@b.ru") {
		t.Fatalf("карточка: %d %s", r.StatusCode, body)
	}

	// старт и пауза
	if r, body := adm.do("POST", "/api/admin/campaigns/"+created.ID+"/start", nil); r.StatusCode != 200 {
		t.Fatalf("старт: %d %s", r.StatusCode, body)
	}
	if r, _ := adm.do("POST", "/api/admin/campaigns/"+created.ID+"/pause", nil); r.StatusCode != 200 {
		t.Fatal("пауза должна работать")
	}
	if r, _ := adm.do("POST", "/api/admin/campaigns/"+created.ID+"/выключить", nil); r.StatusCode != 400 {
		t.Fatal("неизвестное действие должно давать 400")
	}

	// отписавшийся после старта пропускается
	ua, _ := repo.GetUserByEmail(ctx, "c-a@b.ru")
	_ = repo.ClearMarketingConsent(ctx, ua.ID)
	cid, _ := uuid.Parse(created.ID)
	rec, err := repo.NextRecipient(ctx, cid)
	if err != nil {
		t.Fatalf("очередь: %v", err)
	}
	if rec.Email == "c-a@b.ru" {
		t.Fatal("отписавшийся не должен попадать в очередь отправки")
	}
}

// TestSubscribeFlow – согласие на рассылку, которого раньше не было:
// по ссылке из письма и галочкой в кабинете. Проверяем, что чужой адрес
// подписать нельзя и что дата первого согласия не переписывается.
func TestSubscribeFlow(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()

	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{
		"email": "sub@b.ru", "password": "password123", "consent_pd": true})
	u, err := repo.GetUserByEmail(ctx, "sub@b.ru")
	if err != nil {
		t.Fatal(err)
	}
	consent := func() bool {
		ok, _ := repo.HasMarketingConsent(ctx, u.ID)
		return ok
	}
	if consent() {
		t.Fatal("после регистрации без галочки согласия быть не должно")
	}

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	get := func(url string) *http.Response {
		resp, err := noRedirect.Get(srv.URL + url)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}

	// подделанная подпись не подписывает
	r := get("/api/subscribe?u=" + u.ID.String() + "&t=deadbeef")
	if r.StatusCode != 303 || r.Header.Get("Location") != "/subscribed?error=1" {
		t.Fatalf("плохая подпись: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	if consent() {
		t.Fatal("подделанная ссылка не должна подписывать")
	}

	// валидная ссылка из письма
	tok := handlers.SubscribeToken(cfg.JWTSecret, u.ID.String())
	r = get("/api/subscribe?u=" + u.ID.String() + "&t=" + tok)
	if r.StatusCode != 303 || r.Header.Get("Location") != "/subscribed" {
		t.Fatalf("подписка: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	if !consent() {
		t.Fatal("согласие должно быть записано")
	}

	// повторное нажатие не двигает дату первого согласия
	var first time.Time
	_ = repo.Pool.QueryRow(ctx, `SELECT consent_marketing_at FROM users WHERE id=$1`, u.ID).Scan(&first)
	get("/api/subscribe?u=" + u.ID.String() + "&t=" + tok)
	var second time.Time
	_ = repo.Pool.QueryRow(ctx, `SELECT consent_marketing_at FROM users WHERE id=$1`, u.ID).Scan(&second)
	if !first.Equal(second) {
		t.Fatalf("дата первого согласия не должна меняться: было %v, стало %v", first, second)
	}

	// галочка в кабинете снимает и возвращает согласие
	c.do("POST", "/api/auth/login", map[string]string{"email": "sub@b.ru", "password": "password123"})
	if r, body := c.do("PATCH", "/api/me", map[string]any{"consent_marketing": false}); r.StatusCode != 200 {
		t.Fatalf("снятие галочки: %d %s", r.StatusCode, body)
	}
	if consent() {
		t.Fatal("галочка должна была снять согласие")
	}
	if r, _ := c.do("PATCH", "/api/me", map[string]any{"consent_marketing": true}); r.StatusCode != 200 {
		t.Fatal("возврат галочки должен работать")
	}
	if !consent() {
		t.Fatal("галочка должна была вернуть согласие")
	}
	// /api/me отдаёт текущее состояние
	if _, body := c.do("GET", "/api/me", nil); !strings.Contains(string(body), `"consent_marketing":true`) {
		t.Fatalf("/api/me должен отдавать consent_marketing: %s", body)
	}
}

// TestServiceSegment – сервисная группа: люди без согласия, но с платным
// курсом. Их можно поставить в очередь (письмо про их курс), а в обычные
// группы они не попадают.
func TestServiceSegment(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()

	cid, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "srv-course", Title: "Платный", Kind: "paid", PriceRub: ptrInt(100), IsPublished: true})
	if err != nil {
		t.Fatal(err)
	}

	// человек с курсом, но без согласия
	cu := newClient(srv)
	cu.do("POST", "/api/auth/register", map[string]any{
		"email": "srv@b.ru", "password": "password123", "consent_pd": true})
	u, _ := repo.GetUserByEmail(ctx, "srv@b.ru")
	_ = repo.MarkEmailVerified(ctx, u.ID)
	_ = repo.Grant(ctx, u.ID, cid, "admin", nil)

	adm := newClient(srv)
	adm.do("POST", "/api/auth/register", map[string]any{
		"email": "srv-adm@b.ru", "password": "password123", "consent_pd": true})
	ua, _ := repo.GetUserByEmail(ctx, "srv-adm@b.ru")
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, ua.ID)
	adm.do("POST", "/api/auth/login", map[string]string{"email": "srv-adm@b.ru", "password": "password123"})

	// в обычной группе его нет, в сервисной – есть
	_, body := adm.do("GET", "/api/admin/segments", nil)
	var resp struct {
		Segments []struct {
			Key         string `json:"key"`
			Count       int    `json:"count"`
			ServiceOnly bool   `json:"service_only"`
		} `json:"segments"`
	}
	_ = json.Unmarshal(body, &resp)
	for _, s := range resp.Segments {
		switch s.Key {
		case "all":
			if s.Count != 0 {
				t.Fatalf("в «все подписанные» не должно быть никого, а там %d", s.Count)
			}
		case "service_paid":
			if s.Count != 1 || !s.ServiceOnly {
				t.Fatalf("сервисная группа: count=%d service_only=%v", s.Count, s.ServiceOnly)
			}
		}
	}

	// рассылку по сервисной группе создать можно, человек попадает в очередь
	r, body := adm.do("POST", "/api/admin/campaigns", map[string]any{
		"name": "Сервисное", "subject": "Ваш курс", "body": "Текст про курс.",
		"segment": "service_paid"})
	if r.StatusCode != 201 {
		t.Fatalf("создание сервисной рассылки: %d %s", r.StatusCode, body)
	}
	var created struct {
		ID    string `json:"id"`
		Total int    `json:"total"`
	}
	_ = json.Unmarshal(body, &created)
	if created.Total != 1 {
		t.Fatalf("в сервисной рассылке должен быть 1 адресат, а не %d", created.Total)
	}
	cidCamp, _ := uuid.Parse(created.ID)
	rec, err := repo.NextRecipient(ctx, cidCamp)
	if err != nil {
		t.Fatalf("очередь сервисной рассылки пуста: %v", err)
	}
	if rec.Email != "srv@b.ru" {
		t.Fatalf("не тот адресат: %s", rec.Email)
	}
	if rec.Subscribed {
		t.Fatal("человек не подписан – в письме должна быть кнопка подписки, а не отписки")
	}
}

// TestProdamusRefundClosesAccess — возврат денег: заказ помечается
// возвращённым, доступ, выданный за покупку, закрывается, а подаренный
// доступ к другому курсу остаётся.
func TestProdamusRefundClosesAccess(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()

	uid, _ := repo.CreateUser(ctx, "ref@b.ru", "x", "Ref", "user")
	bought, _ := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "ref-paid", Title: "Куплен", Kind: "paid", PriceRub: ptrInt(50), IsPublished: true})
	gifted, _ := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "ref-gift", Title: "Подарен", Kind: "paid", PriceRub: ptrInt(50), IsPublished: true})
	o, _ := repo.CreateOrder(ctx, uid, bought, 50)
	_ = repo.Grant(ctx, uid, bought, "purchase", nil)
	_ = repo.Grant(ctx, uid, gifted, "admin", nil)
	_ = repo.MarkOrderPaid(ctx, o.ID, "48906087")

	body := map[string]any{
		"order_id":       "48906087",
		"order_num":      o.ID.String(),
		"payment_status": "refund",
		"sum":            "50.00",
	}
	sig, _ := prodamus.Sign(cfg.ProdamusSecret, body)
	form := url.Values{}
	for k, v := range body {
		form.Set(k, v.(string))
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/webhooks/prodamus", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sign", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	o2, _ := repo.GetOrder(ctx, o.ID)
	if o2.Status != "refunded" {
		t.Fatalf("заказ должен стать refunded, а он %s", o2.Status)
	}
	if has, _ := repo.HasEnrollment(ctx, uid, bought); has {
		t.Fatal("доступ за покупку должен закрыться")
	}
	if has, _ := repo.HasEnrollment(ctx, uid, gifted); !has {
		t.Fatal("подаренный доступ трогать нельзя")
	}
}

// TestArticleViewCounter — счётчик чтения статей: публичный маяк пишет
// события без CSRF, мусор игнорируется, сводка закрыта от обычного юзера.
func TestArticleViewCounter(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	admin := adminClient(t, srv, repo)

	// Статья, которую будем «читать»
	r, body := admin.do("POST", "/api/admin/articles", map[string]any{
		"slug": "kak-nachat", "title": "Как начать", "content_html": "<p>Текст</p>",
		"is_published": true})
	if r.StatusCode != 201 {
		t.Fatalf("create article: %d %s", r.StatusCode, body)
	}

	anon := newClient(srv)
	// Маяк работает без X-CSRF-Token — его шлёт sendBeacon, cookie там нет.
	for _, ev := range []string{"open", "read", "open", "cta"} {
		r, _ = anon.doNoCSRF("POST", "/api/articles/kak-nachat/view", map[string]any{"event": ev})
		if r.StatusCode != 204 {
			t.Fatalf("track %s: %d", ev, r.StatusCode)
		}
	}
	// Мусор не должен ломать эндпоинт и не должен считаться.
	r, _ = anon.doNoCSRF("POST", "/api/articles/kak-nachat/view", map[string]any{"event": "drop table"})
	if r.StatusCode != 204 {
		t.Fatalf("bad event: %d", r.StatusCode)
	}
	// Несуществующая статья — тоже 204 и ноль записей.
	r, _ = anon.doNoCSRF("POST", "/api/articles/net-takoy/view", map[string]any{"event": "open"})
	if r.StatusCode != 204 {
		t.Fatalf("unknown slug: %d", r.StatusCode)
	}

	// Сводка закрыта для не-админа
	user := newClient(srv)
	user.do("POST", "/api/auth/register", map[string]any{
		"email": "views-user@b.ru", "password": "password123", "consent_pd": true})
	user.do("POST", "/api/auth/login", map[string]string{
		"email": "views-user@b.ru", "password": "password123"})
	if r, _ = user.do("GET", "/api/admin/article-stats", nil); r.StatusCode == 200 {
		t.Fatalf("article stats must be admin-only, got 200")
	}

	// Админ видит цифры
	stats, err := repo.ArticleStats(ctx)
	if err != nil || len(stats) != 1 {
		t.Fatalf("stats: %v %d", err, len(stats))
	}
	s := stats[0]
	if s.Opens != 2 || s.Reads != 1 || s.CTA != 1 {
		t.Fatalf("counts: opens=%d reads=%d cta=%d", s.Opens, s.Reads, s.CTA)
	}
	if s.Opens30 != 2 || s.LastOpen == nil {
		t.Fatalf("30d window: %d, last=%v", s.Opens30, s.LastOpen)
	}
}

// TestNewsletterSignup — подписка с сайта: форма только заводит адрес и
// шлёт письмо, само согласие ставится по ссылке из письма.
func TestNewsletterSignup(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()
	c := newClient(srv)

	// Без галочки про персональные данные — отказ
	r, _ := c.do("POST", "/api/newsletter", map[string]any{"email": "nl@b.ru"})
	if r.StatusCode != 400 {
		t.Fatalf("без согласия ПД ждём 400, получили %d", r.StatusCode)
	}
	// Кривой адрес — отказ
	r, _ = c.do("POST", "/api/newsletter", map[string]any{"email": "не-почта", "consent_pd": true})
	if r.StatusCode != 400 {
		t.Fatalf("кривой адрес: %d", r.StatusCode)
	}

	// Валидная заявка: пользователь заводится, ПД зафиксировано,
	// а на рассылку согласия ещё НЕТ — только после ссылки из письма.
	r, body := c.do("POST", "/api/newsletter", map[string]any{"email": "NL@B.ru", "consent_pd": true})
	if r.StatusCode != 200 {
		t.Fatalf("подписка: %d %s", r.StatusCode, body)
	}
	u, err := repo.GetUserByEmail(ctx, "nl@b.ru")
	if err != nil {
		t.Fatalf("пользователь не заведён: %v", err)
	}
	pd, marketing, err := repo.UserConsents(ctx, u.ID)
	if err != nil {
		t.Fatalf("consents: %v", err)
	}
	if pd == nil {
		t.Fatalf("согласие на ПД должно быть зафиксировано сразу")
	}
	if marketing != nil {
		t.Fatalf("согласие на рассылку не должно ставиться до подтверждения")
	}

	// Ссылка из письма включает рассылку. Редиректы не следуем: /subscribed –
	// страница Astro, на тестовом сервере её нет.
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	tok := handlers.SubscribeToken(cfg.JWTSecret, u.ID.String())
	resp, err := noRedirect.Get(srv.URL + "/api/subscribe?u=" + u.ID.String() + "&t=" + tok)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 303 || resp.Header.Get("Location") != "/subscribed" {
		t.Fatalf("подтверждение: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, marketing, _ = repo.UserConsents(ctx, u.ID)
	if marketing == nil {
		t.Fatalf("после подтверждения согласие на рассылку должно стоять")
	}

	// Повторная отправка на тот же адрес не плодит пользователей
	r, _ = c.do("POST", "/api/newsletter", map[string]any{"email": "nl@b.ru", "consent_pd": true})
	if r.StatusCode != 200 {
		t.Fatalf("повтор: %d", r.StatusCode)
	}
	var n int
	if err := repo.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE email='nl@b.ru'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("дубль пользователя: %v %d", err, n)
	}
}

// TestServiceCheckout — оплата «Точки перемен» без регистрации: заказ
// создаётся без курса, после оплаты доступ к курсам не выдаётся.
func TestServiceCheckout(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()
	c := newClient(srv)

	// Проверки формы
	for _, bad := range []map[string]any{
		{"service": "start", "email": "a@b.ru", "name": "Иван"},                     // нет согласия
		{"service": "start", "email": "кривая", "name": "Иван", "consent_pd": true}, // адрес
		{"service": "start", "email": "a@b.ru", "name": "", "consent_pd": true},     // имя
		{"service": "чужая", "email": "a@b.ru", "name": "Иван", "consent_pd": true}, // услуга
	} {
		r, _ := c.do("POST", "/api/checkout/service", bad)
		if r.StatusCode != 400 {
			t.Fatalf("ждём 400 на %v, получили %d", bad, r.StatusCode)
		}
	}

	// Валидная оплата: пользователь заводится, заказ без курса на 2990
	r, body := c.do("POST", "/api/checkout/service", map[string]any{
		"service": "start", "email": "Buyer@B.ru", "name": "Иван", "consent_pd": true})
	if r.StatusCode != 200 {
		t.Fatalf("checkout: %d %s", r.StatusCode, body)
	}
	var out struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(body, &out)
	if out.URL == "" {
		t.Fatalf("нет ссылки на оплату: %s", body)
	}

	u, err := repo.GetUserByEmail(ctx, "buyer@b.ru")
	if err != nil {
		t.Fatalf("покупатель не заведён: %v", err)
	}
	if u.Name != "Иван" {
		t.Fatalf("имя не сохранилось: %q", u.Name)
	}
	orders, err := repo.ListOrders(ctx, "", nil, nil, 10, 0)
	if err != nil || len(orders) != 1 {
		t.Fatalf("заказы: %v %d", err, len(orders))
	}
	o := orders[0]
	if o.CourseID != uuid.Nil {
		t.Fatalf("заказ на услугу не должен ссылаться на курс: %v", o.CourseID)
	}
	if o.Service != "start" || o.AmountRub != 2990 || o.Status != "pending" {
		t.Fatalf("заказ: service=%q amount=%d status=%q", o.Service, o.AmountRub, o.Status)
	}

	// Оплата через вебхук: заказ становится paid, доступов к курсам не появляется
	pay := map[string]any{
		"order_id":       "48479471", // внутренний номер Продамуса
		"order_num":      o.ID.String(),
		"payment_status": "success",
		"sum":            "2990.00",
	}
	sig, _ := prodamus.Sign(cfg.ProdamusSecret, pay)
	form := url.Values{}
	for k, v := range pay {
		form.Set(k, v.(string))
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/webhooks/prodamus", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sign", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	o2, err := repo.GetOrder(ctx, o.ID)
	if err != nil || o2.Status != "paid" {
		t.Fatalf("после вебхука заказ должен быть paid: %v %+v", err, o2)
	}
	var enrollments int
	if err := repo.Pool.QueryRow(ctx, `SELECT count(*) FROM enrollments WHERE user_id=$1`, u.ID).Scan(&enrollments); err != nil {
		t.Fatal(err)
	}
	if enrollments != 0 {
		t.Fatalf("оплата услуги не должна открывать курсы, а открыла %d", enrollments)
	}
}

// Курс жонглирования: тарифы 1490/3990, черновик курса не продаётся
// обычным пользователям.
func TestZhonglirovanieTariffsAndDraft(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	price := 1490
	cid, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "zhonglirovanie", Title: "Жонглирование", Kind: "paid",
		PriceRub: &price, IsPublished: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	makeVerified := func(email string) *client {
		c := newClient(srv)
		c.do("POST", "/api/auth/register", map[string]any{
			"email": email, "password": "password123", "consent_pd": true,
		})
		u, _ := repo.GetUserByEmail(ctx, email)
		_ = repo.MarkEmailVerified(ctx, u.ID)
		c.do("POST", "/api/auth/login", map[string]string{"email": email, "password": "password123"})
		return c
	}
	amount := func(t *testing.T, body []byte) int {
		t.Helper()
		var resp struct {
			OrderID string `json:"order_id"`
		}
		_ = json.Unmarshal(body, &resp)
		oid, err := uuid.Parse(resp.OrderID)
		if err != nil {
			t.Fatalf("bad order_id: %s", body)
		}
		o, err := repo.GetOrder(ctx, oid)
		if err != nil {
			t.Fatal(err)
		}
		return o.AmountRub
	}

	// 1) черновик: обычному пользователю – 404, курс в API не виден
	c1 := makeVerified("tm-draft@b.ru")
	r, body := c1.do("POST", "/api/courses/zhonglirovanie/checkout?tariff=self", nil)
	if r.StatusCode != 404 {
		t.Fatalf("draft checkout: want 404, got %d %s", r.StatusCode, body)
	}
	if r, _ := c1.do("GET", "/api/courses/zhonglirovanie", nil); r.StatusCode != 404 {
		t.Fatalf("draft course visible: %d", r.StatusCode)
	}

	// 2) после публикации – оба тарифа с правильной суммой (после стартовой цены)
	defer handlers.SetNowForTest(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))()
	_, _ = repo.Pool.Exec(ctx, `UPDATE courses SET is_published=true WHERE id=$1`, cid)
	r, body = c1.do("POST", "/api/courses/zhonglirovanie/checkout?tariff=self", nil)
	if r.StatusCode != 200 || amount(t, body) != 1490 {
		t.Fatalf("self: %d %s", r.StatusCode, body)
	}
	c2 := makeVerified("tm-sup@b.ru")
	r, body = c2.do("POST", "/api/courses/zhonglirovanie/checkout?tariff=support", nil)
	if r.StatusCode != 200 || amount(t, body) != 3990 {
		t.Fatalf("support: %d %s", r.StatusCode, body)
	}
	if r, body := c2.do("POST", "/api/courses/zhonglirovanie/checkout", nil); r.StatusCode != 400 || !strings.Contains(string(body), "tariff_required") {
		t.Fatalf("no tariff: %d %s", r.StatusCode, body)
	}
}

// TestNoSecretLinksInProduction — ответы API не должны содержать ссылок
// восстановления и подтверждения. Раньше /auth/forgot-password отдавал
// готовую ссылку сброса всем подряд: зная чужой email, можно было забрать
// аккаунт, не имея доступа к почте.
func TestNoSecretLinksInProduction(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()
	_, _ = repo.CreateUser(ctx, "prod-leak@b.ru", "x", "Leak", "user")

	cfg.AppEnv = "production" // App держит тот же указатель на конфиг
	c := newClient(srv)

	r, body := c.do("POST", "/api/auth/forgot-password", map[string]string{"email": "prod-leak@b.ru"})
	if r.StatusCode != 200 {
		t.Fatalf("forgot: %d %s", r.StatusCode, body)
	}
	if strings.Contains(string(body), "reset_link_dev") || strings.Contains(string(body), "/auth/reset?token=") {
		t.Fatalf("ссылка сброса утекла в ответ: %s", body)
	}

	r, body = c.do("POST", "/api/auth/register", map[string]any{
		"email": "prod-reg@b.ru", "password": "password123", "consent_pd": true})
	if r.StatusCode != 201 {
		t.Fatalf("register: %d %s", r.StatusCode, body)
	}
	if strings.Contains(string(body), "verify_link_dev") || strings.Contains(string(body), "/auth/verify?token=") {
		t.Fatalf("ссылка подтверждения утекла в ответ: %s", body)
	}

	// Токен сброса при этом создаётся – письмо человек получит.
	var n int
	if err := repo.Pool.QueryRow(ctx,
		`SELECT count(*) FROM password_reset_tokens t JOIN users u ON u.id=t.user_id
		  WHERE u.email='prod-leak@b.ru'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("токен сброса должен быть создан: %v %d", err, n)
	}
}

// TestAdminRefundOrder — ручной возврат: Продамус присылает уведомление о
// возврате не всегда, поэтому админ закрывает доступ сам.
func TestAdminRefundOrder(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	uid, _ := repo.CreateUser(ctx, "refund-me@b.ru", "x", "Возврат", "user")
	cid, _ := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "refund-course", Title: "Курс", Kind: "paid", PriceRub: ptrInt(3990), IsPublished: true})
	o, _ := repo.CreateOrder(ctx, uid, cid, 3990)
	_ = repo.MarkOrderPaid(ctx, o.ID, "P-1")
	_ = repo.Grant(ctx, uid, cid, "purchase", nil)

	// Обычному пользователю – нельзя
	user := newClient(srv)
	user.do("POST", "/api/auth/register", map[string]any{
		"email": "not-admin@b.ru", "password": "password123", "consent_pd": true})
	user.do("POST", "/api/auth/login", map[string]string{"email": "not-admin@b.ru", "password": "password123"})
	if r, _ := user.do("POST", "/api/admin/orders/"+o.ID.String()+"/refund", nil); r.StatusCode == 200 {
		t.Fatal("возврат должен быть доступен только админу")
	}

	admin := adminClient(t, srv, repo)
	r, body := admin.do("POST", "/api/admin/orders/"+o.ID.String()+"/refund", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), `"access_revoked":true`) {
		t.Fatalf("возврат: %d %s", r.StatusCode, body)
	}
	o2, _ := repo.GetOrder(ctx, o.ID)
	if o2.Status != "refunded" {
		t.Fatalf("заказ должен стать refunded, а он %s", o2.Status)
	}
	if has, _ := repo.HasEnrollment(ctx, uid, cid); has {
		t.Fatal("купленный доступ должен закрыться")
	}
	// Повторно – нельзя: заказ уже не оплачен
	if r, _ = admin.do("POST", "/api/admin/orders/"+o.ID.String()+"/refund", nil); r.StatusCode != 400 {
		t.Fatalf("повторный возврат: ждём 400, получили %d", r.StatusCode)
	}
	// В журнале есть запись
	var n int
	_ = repo.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='refund' AND target_id=$1`, o.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("запись в журнале: %d", n)
	}

	// Заказы на услуги видны в списке оплат (раньше выпадали из-за пустого course_id)
	so, _ := repo.CreateServiceOrder(ctx, uid, "start", 2990)
	r, body = admin.do("GET", "/api/admin/orders", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), so.ID.String()) {
		t.Fatalf("заказ на услугу не попал в список: %d %s", r.StatusCode, body)
	}
}

// TestDripChain – автоцепочка после «Мягкого старта»: кому, когда и что уходит.
func TestDripChain(t *testing.T) {
	srv, repo, cfg := setup(t)
	_ = srv
	ctx := context.Background()
	app := &handlers.App{Cfg: cfg, Repo: repo, Mail: email.New("", "", "", "", "")}
	freeID, err := repo.CreateCourse(ctx, db.CourseInput{Slug: "myagkiy-start", Title: "MS", Kind: "free", IsPublished: true})
	if err != nil {
		t.Fatal(err)
	}
	price := 3990
	zsID, err := repo.CreateCourse(ctx, db.CourseInput{Slug: "zdorovaya-spina", Title: "ZS", Kind: "paid", PriceRub: &price, IsPublished: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSetting(ctx, "drip_start_at", "2026-09-30T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSetting(ctx, "drip_nosub_start_at", "2026-09-30T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// Регистрация 1 октября 12:00 по Москве.
	reg := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	mk := func(email string, consent bool, created time.Time) uuid.UUID {
		id, err := repo.CreateUser(ctx, email, "x", "Анна", "user")
		if err != nil {
			t.Fatal(err)
		}
		_ = repo.SaveConsent(ctx, id, true, consent)
		_ = repo.Grant(ctx, id, freeID, "free", nil)
		if _, err := repo.Pool.Exec(ctx, `UPDATE users SET created_at=$1 WHERE id=$2`, created, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	a := mk("a@b.ru", true, reg)
	nomail := mk("nomail@b.ru", false, reg)      // без согласия: только письма 1–3
	unsub := mk("unsub@b.ru", true, reg)         // отписался: ничего
	mk("old@b.ru", true, reg.AddDate(0, 0, -30)) // до запуска цепочки
	if err := repo.ClearMarketingConsent(ctx, unsub); err != nil {
		t.Fatal(err)
	}

	msk := func(day, hour int) time.Time { // день после регистрации, час по Москве
		return time.Date(2026, 10, 1+day, hour-3, 0, 0, 0, time.UTC)
	}
	statusOf := func(id uuid.UUID, step int) string {
		var s string
		_ = repo.Pool.QueryRow(ctx, `SELECT status FROM drip_sends WHERE user_id=$1 AND step=$2`, id, step).Scan(&s)
		return s
	}
	status := func(step int) string { return statusOf(a, step) }
	// sendAll – отправщик шлёт по одному письму за тик: прогоняем, пока есть кому.
	sendAll := func(now time.Time) {
		for i := 0; i < 10; i++ {
			if err := app.SendOneDripEmailForTest(ctx, now); err != nil {
				return
			}
		}
	}
	total := func() int {
		var n int
		_ = repo.Pool.QueryRow(ctx, `SELECT count(*) FROM drip_sends`).Scan(&n)
		return n
	}

	// Сразу после регистрации ничего: письмо 1 – не раньше чем через 2 часа.
	if err := app.SendOneDripEmailForTest(ctx, msk(0, 13)); err != db.ErrNotFound {
		t.Fatalf("day 0, 1h after signup: expected nothing, got %v", err)
	}
	// День 0, 15:00 – письмо 1 новичкам: подписанному и без согласия
	// (это подсказка по курсу), но не отписавшемуся.
	sendAll(msk(0, 15))
	if status(1) != "sent" || statusOf(nomail, 1) != "sent" || statusOf(unsub, 1) != "" || total() != 2 {
		t.Fatalf("step 1 must be sent to a@ and nomail@ only, total=%d", total())
	}
	if err := app.SendOneDripEmailForTest(ctx, msk(0, 16)); err != db.ErrNotFound {
		t.Fatalf("second send same day: %v", err)
	}
	// Ночью не шлём даже когда пора.
	back0 := total()
	_, _ = repo.Pool.Exec(ctx, `UPDATE drip_sends SET sent_at = $1`, reg)
	_ = app.SendOneDripEmailForTest(ctx, msk(3, 7))
	if total() != back0 {
		t.Fatalf("must not send before 9:00 MSK")
	}
	// День 2 – ещё рано для письма 2.
	if err := app.SendOneDripEmailForTest(ctx, msk(2, 10)); err != db.ErrNotFound {
		t.Fatalf("day 2: %v", err)
	}
	// День 3 и 5 – письма 2 и 3 (время отправки в тесте – реальное «сейчас» БД,
	// поэтому ставим прошлым отправкам дату регистрации, чтобы не сработал
	// «не чаще раза в день» – независимо от того, когда запускают тест).
	back := func() { _, _ = repo.Pool.Exec(ctx, `UPDATE drip_sends SET sent_at = $1`, reg) }
	back()
	sendAll(msk(3, 10))
	if status(2) != "sent" || statusOf(nomail, 2) != "sent" {
		t.Fatalf("step 2 must be sent on day 3")
	}
	back()
	sendAll(msk(5, 10))
	if status(3) != "sent" || statusOf(nomail, 3) != "sent" {
		t.Fatalf("step 3 must be sent on day 5")
	}
	// Купила «Здоровую спину» – письма с предложением пропускаются.
	_ = repo.Grant(ctx, a, zsID, "purchase", nil)
	back()
	_ = app.SendOneDripEmailForTest(ctx, msk(7, 10))
	if status(4) != "skipped" {
		t.Fatalf("step 4 must be skipped for a buyer, got %q", status(4))
	}
	// Письма с предложением без согласия не уходят.
	back()
	sendAll(msk(7, 12))
	if statusOf(nomail, 4) != "" {
		t.Fatalf("offer must not go without consent, got %q", statusOf(nomail, 4))
	}
	// Нажал «Да, присылайте» – предложение уходит.
	_ = repo.SetMarketingConsent(ctx, nomail)
	back()
	sendAll(msk(7, 13))
	if statusOf(nomail, 4) != "sent" {
		t.Fatalf("offer must go after consent, got %q", statusOf(nomail, 4))
	}
	// Выключили в админке – ничего не уходит.
	_ = repo.SetSetting(ctx, "drip_enabled", "false")
	_ = app.SendOneDripEmailForTest(ctx, msk(10, 10))
	if status(5) != "" {
		t.Fatalf("disabled chain must not send")
	}
	_ = repo.SetSetting(ctx, "drip_enabled", "true")
	_ = app.SendOneDripEmailForTest(ctx, msk(10, 10))
	if status(5) != "skipped" {
		t.Fatalf("step 5 must be skipped for a buyer, got %q", status(5))
	}
}

// TestTrafficSource – метка ?from= (кука src) попадает в users.source при
// регистрации, чистится от мусора и видна в сводке админки.
func TestTrafficSource(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	if _, err := repo.CreateCourse(ctx, db.CourseInput{Slug: "myagkiy-start", Title: "MS", Kind: "free", IsPublished: true}); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(srv.URL)
	signup := func(email, src string) {
		c := newClient(srv)
		if src != "" {
			c.jar.SetCookies(u, []*http.Cookie{{Name: "src", Value: src, Path: "/"}})
		}
		r, body := c.do("POST", "/api/auth/quick-signup", map[string]any{
			"email": email, "name": "T", "consent_pd": true, "consent_marketing": true})
		if r.StatusCode != 201 {
			t.Fatalf("signup %s: %d %s", email, r.StatusCode, body)
		}
	}
	signup("i1@b.ru", "insta")
	signup("i2@b.ru", "ref%3Ainstagram") // как кодирует браузер ref:instagram
	signup("x@b.ru", "<script>")         // мусор – не пишем
	signup("n@b.ru", "")
	src := func(email string) string {
		var s *string
		_ = repo.Pool.QueryRow(ctx, `SELECT source FROM users WHERE email=$1`, email).Scan(&s)
		if s == nil {
			return ""
		}
		return *s
	}
	if src("i1@b.ru") != "insta" || src("i2@b.ru") != "ref:instagram" || src("x@b.ru") != "" || src("n@b.ru") != "" {
		t.Fatalf("sources: %q %q %q %q", src("i1@b.ru"), src("i2@b.ru"), src("x@b.ru"), src("n@b.ru"))
	}
	adm := adminClient(t, srv, repo)
	r, body := adm.do("GET", "/api/admin/sources?days=7", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), `"source":"insta","signups":1,"subscribed":1`) {
		t.Fatalf("admin sources: %d %s", r.StatusCode, body)
	}
	if r, _ := newClient(srv).do("GET", "/api/admin/sources", nil); r.StatusCode != 401 && r.StatusCode != 403 {
		t.Fatalf("anon must not see sources: %d", r.StatusCode)
	}
}

// TestAnketa – анкета «Точки перемен»: одна на человека, только по подписанной
// ссылке, с согласием на данные о здоровье и обязательными полями; админ
// получает ссылку для любого человека (в т.ч. нового – по email).
func TestAnketa(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()
	uid, err := repo.CreateUser(ctx, "anketa@b.ru", "x", "Мария Иванова", "user")
	if err != nil {
		t.Fatal(err)
	}
	q := "?u=" + uid.String() + "&t=" + handlers.AnketaToken(cfg.JWTSecret, uid.String())
	c := newClient(srv)

	if r, _ := c.do("GET", "/api/anketa?u="+uid.String()+"&t=deadbeef", nil); r.StatusCode != 400 {
		t.Fatalf("bad token: %d", r.StatusCode)
	}
	r, body := c.do("GET", "/api/anketa"+q, nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), `"name":"Мария"`) || !strings.Contains(string(body), `"submitted":false`) {
		t.Fatalf("get: %d %s", r.StatusCode, body)
	}
	full := map[string]string{"goal": "спина", "activity": "почти ничего", "time": "3 раза по 30 минут", "slots": "пн вечер", "limits": "нет"}
	// без согласия – нельзя, и сразу видно, что ещё не заполнено
	if r, body := c.do("POST", "/api/anketa"+q, map[string]any{"answers": map[string]string{"goal": "x"}}); r.StatusCode != 400 ||
		!strings.Contains(string(body), "consent_health_required") || !strings.Contains(string(body), `"slots"`) {
		t.Fatalf("no consent: %d %s", r.StatusCode, body)
	}
	if r, body := c.do("POST", "/api/anketa"+q, map[string]any{"answers": map[string]string{"goal": "x"}, "consent_health": true}); r.StatusCode != 400 || !strings.Contains(string(body), `"slots"`) {
		t.Fatalf("missing: %d %s", r.StatusCode, body)
	}
	full["unknown"] = "не сохраняем"
	if r, body := c.do("POST", "/api/anketa"+q, map[string]any{"answers": full, "consent_health": true}); r.StatusCode != 200 {
		t.Fatalf("submit: %d %s", r.StatusCode, body)
	}
	saved, err := repo.GetQuestionnaire(ctx, uid)
	if err != nil || saved.Answers["goal"] != "спина" || saved.Answers["unknown"] != "" {
		t.Fatalf("saved: %+v %v", saved, err)
	}
	full["goal"] = "вес"
	c.do("POST", "/api/anketa"+q, map[string]any{"answers": full, "consent_health": true})
	_, body = c.do("GET", "/api/anketa"+q, nil)
	if !strings.Contains(string(body), `"goal":"вес"`) || !strings.Contains(string(body), `"submitted":true`) {
		t.Fatalf("resubmit: %s", body)
	}

	// админка: ссылка для любого человека
	if r, _ := newClient(srv).do("POST", "/api/admin/anketa", map[string]any{"email": "new@b.ru"}); r.StatusCode != 401 && r.StatusCode != 403 {
		t.Fatalf("anon admin anketa: %d", r.StatusCode)
	}
	adm := adminClient(t, srv, repo)
	r, body = adm.do("POST", "/api/admin/anketa", map[string]any{"email": " New@B.ru ", "name": "Пётр"})
	if r.StatusCode != 200 || !strings.Contains(string(body), "/anketa?u=") {
		t.Fatalf("admin by email: %d %s", r.StatusCode, body)
	}
	nu, err := repo.GetUserByEmail(ctx, "new@b.ru")
	if err != nil || nu.Name != "Пётр" {
		t.Fatalf("new user not created: %v", err)
	}
	r, body = adm.do("POST", "/api/admin/anketa", map[string]any{"user_id": uid.String(), "send": true})
	if r.StatusCode != 200 || !strings.Contains(string(body), handlers.AnketaToken(cfg.JWTSecret, uid.String())) {
		t.Fatalf("admin by id: %d %s", r.StatusCode, body)
	}
	if r, _ := adm.do("POST", "/api/admin/anketa", map[string]any{"email": "nope"}); r.StatusCode != 400 {
		t.Fatalf("bad email: %d", r.StatusCode)
	}
}

// Покупка курса в один шаг: имя + почта → заказ и ссылка на оплату, без
// пароля и подтверждения почты. Стартовая цена «Самостоятельного» действует
// до 19.10.2026 00:00 МСК, после – обычная.
func TestQuickCheckoutAndPromo(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	price := 1490
	cid, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "zhonglirovanie", Title: "Жонглирование", Kind: "paid",
		PriceRub: &price, IsPublished: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	orderAmount := func(t *testing.T, body []byte) int {
		t.Helper()
		var resp struct {
			OrderID    string `json:"order_id"`
			PaymentURL string `json:"payment_url"`
		}
		_ = json.Unmarshal(body, &resp)
		oid, err := uuid.Parse(resp.OrderID)
		if err != nil || !strings.Contains(resp.PaymentURL, "fake-payment") {
			t.Fatalf("bad response: %s", body)
		}
		o, err := repo.GetOrder(ctx, oid)
		if err != nil {
			t.Fatal(err)
		}
		return o.AmountRub
	}
	buy := map[string]any{"email": "New@Buyer.ru", "name": "Анна", "tariff": "self", "consent_pd": true}

	// 1) во время акции – 990, аккаунт создаётся по почте (в нижнем регистре)
	undo := handlers.SetNowForTest(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	c := newClient(srv)
	r, body := c.do("POST", "/api/courses/zhonglirovanie/quick-checkout", buy)
	if r.StatusCode != 200 || orderAmount(t, body) != 990 {
		t.Fatalf("promo: %d %s", r.StatusCode, body)
	}
	u, err := repo.GetUserByEmail(ctx, "new@buyer.ru")
	if err != nil || u.Name != "Анна" {
		t.Fatalf("user not created: %v", err)
	}
	// цены для лендинга: self – 990 и срок акции, support – без акции
	_, body = c.do("GET", "/api/courses/zhonglirovanie", nil)
	var cr struct {
		Tariffs map[string]struct {
			Price      int     `json:"price"`
			BasePrice  int     `json:"base_price"`
			PromoUntil *string `json:"promo_until"`
		} `json:"tariffs"`
	}
	_ = json.Unmarshal(body, &cr)
	if cr.Tariffs["self"].Price != 990 || cr.Tariffs["self"].BasePrice != 1490 || cr.Tariffs["self"].PromoUntil == nil ||
		cr.Tariffs["support"].Price != 3990 || cr.Tariffs["support"].PromoUntil != nil {
		t.Fatalf("tariffs: %s", body)
	}
	undo()

	// 2) после срока – обычная цена; повторная покупка того же адреса – тот же аккаунт
	undo = handlers.SetNowForTest(time.Date(2026, 10, 18, 21, 0, 0, 0, time.UTC)) // 19.10 00:00 МСК
	r, body = newClient(srv).do("POST", "/api/courses/zhonglirovanie/quick-checkout", buy)
	if r.StatusCode != 200 || orderAmount(t, body) != 1490 {
		t.Fatalf("after promo: %d %s", r.StatusCode, body)
	}
	undo()

	// 3) ошибки ввода
	for _, tc := range []struct {
		body map[string]any
		want string
	}{
		{map[string]any{"email": "a@b.ru", "name": "А", "tariff": "self"}, "consent_pd_required"},
		{map[string]any{"email": "nope", "name": "А", "tariff": "self", "consent_pd": true}, "bad_email"},
		{map[string]any{"email": "a@b.ru", "name": " ", "tariff": "self", "consent_pd": true}, "name_required"},
		{map[string]any{"email": "a@b.ru", "name": "А", "tariff": "premium", "consent_pd": true}, "bad_tariff"},
	} {
		r, body := newClient(srv).do("POST", "/api/courses/zhonglirovanie/quick-checkout", tc.body)
		if r.StatusCode != 400 || !strings.Contains(string(body), tc.want) {
			t.Fatalf("want %s, got %d %s", tc.want, r.StatusCode, body)
		}
	}
	// без CSRF – отказ
	if r, _ := newClient(srv).doNoCSRF("POST", "/api/courses/zhonglirovanie/quick-checkout", buy); r.StatusCode != 403 {
		t.Fatalf("csrf: %d", r.StatusCode)
	}

	// 4) у адреса уже есть доступ – 409, нового заказа нет
	_ = repo.Grant(ctx, u.ID, cid, "purchase", nil)
	r, body = newClient(srv).do("POST", "/api/courses/zhonglirovanie/quick-checkout", buy)
	if r.StatusCode != 409 || !strings.Contains(string(body), "already_enrolled") {
		t.Fatalf("enrolled: %d %s", r.StatusCode, body)
	}

	// 5) черновик курса анониму не продаётся
	_, _ = repo.Pool.Exec(ctx, `UPDATE courses SET is_published=false WHERE id=$1`, cid)
	b2 := map[string]any{"email": "other@buyer.ru", "name": "Б", "tariff": "self", "consent_pd": true}
	if r, _ := newClient(srv).do("POST", "/api/courses/zhonglirovanie/quick-checkout", b2); r.StatusCode != 404 {
		t.Fatalf("draft: %d", r.StatusCode)
	}
}

// Сброс пароля по ссылке из письма сразу логинит и подтверждает почту –
// так покупатель из QuickCheckout попадает в кабинет одним кликом.
func TestResetPasswordLogsIn(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	c := newClient(srv)
	c.do("POST", "/api/auth/register", map[string]any{"email": "rs@b.ru", "password": "password123", "consent_pd": true})
	u, _ := repo.GetUserByEmail(ctx, "rs@b.ru")
	raw, hash, _ := auth.RandomToken(32)
	if err := repo.CreatePasswordResetToken(ctx, u.ID, hash, time.Hour); err != nil {
		t.Fatal(err)
	}
	fresh := newClient(srv)
	if r, _ := fresh.do("GET", "/api/me", nil); r.StatusCode != 401 {
		t.Fatalf("must be anonymous first: %d", r.StatusCode)
	}
	r, body := fresh.do("POST", "/api/auth/reset-password", map[string]any{"Token": raw, "Password": "newpassword1"})
	if r.StatusCode != 200 {
		t.Fatalf("reset: %d %s", r.StatusCode, body)
	}
	r, body = fresh.do("GET", "/api/me", nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), `"email_verified":true`) {
		t.Fatalf("me after reset: %d %s", r.StatusCode, body)
	}
}

// TestEmailRetry – письма, не ушедшие из-за почты, отправляются повторно:
// шаг цепочки со статусом failed и письмо из очереди email_outbox.
func TestEmailRetry(t *testing.T) {
	_, repo, cfg := setup(t)
	ctx := context.Background()
	// Пустой SMTP_HOST – отправка «успешна» без сети.
	app := &handlers.App{Cfg: cfg, Repo: repo, Mail: email.New("", "", "", "", "")}
	freeID, err := repo.CreateCourse(ctx, db.CourseInput{Slug: "myagkiy-start", Title: "MS", Kind: "free", IsPublished: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = repo.SetSetting(ctx, "drip_start_at", "2026-09-30T00:00:00Z")
	_ = repo.SetSetting(ctx, "drip_enabled", "true")
	id, err := repo.CreateUser(ctx, "late@b.ru", "x", "Анна", "user")
	if err != nil {
		t.Fatal(err)
	}
	_ = repo.SaveConsent(ctx, id, true, true)
	_ = repo.Grant(ctx, id, freeID, "free", nil)
	_, _ = repo.Pool.Exec(ctx, `UPDATE users SET created_at='2026-10-10T09:00:00Z' WHERE id=$1`, id)
	// Письмо 1 не ушло 5 минут назад – повтор ещё рано.
	now := time.Now()
	if h := now.UTC().Add(3 * time.Hour).Hour(); h < 9 || h >= 21 {
		now = time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.UTC) // 12:00 МСК
	}
	// Время отправки считаем от того же now, что передаём воркеру, – иначе
	// вечером (now подменён на 12:00) «5 минут назад» оказывается в будущем.
	_, _ = repo.Pool.Exec(ctx, `INSERT INTO drip_sends(user_id, step, status, error, sent_at)
		VALUES ($1, 1, 'failed', 'timeout', $2)`, id, now.Add(-5*time.Minute))
	if err := app.SendOneDripEmailForTest(ctx, now); err != db.ErrNotFound {
		t.Fatalf("retry too early: %v", err)
	}
	// Через 40 минут – повтор уходит, строка становится sent, попытка №2.
	_, _ = repo.Pool.Exec(ctx, `UPDATE drip_sends SET sent_at = $1`, now.Add(-40*time.Minute))
	if err := app.SendOneDripEmailForTest(ctx, now); err != nil {
		t.Fatalf("retry: %v", err)
	}
	var st string
	var attempts int
	_ = repo.Pool.QueryRow(ctx, `SELECT status, attempts FROM drip_sends WHERE user_id=$1 AND step=1`, id).Scan(&st, &attempts)
	if st != "sent" || attempts != 2 {
		t.Fatalf("after retry: status=%q attempts=%d", st, attempts)
	}
	// Отправленный шаг не перезаписывается.
	_ = repo.RecordDrip(ctx, id, 1, "failed", "x")
	_ = repo.Pool.QueryRow(ctx, `SELECT status FROM drip_sends WHERE user_id=$1 AND step=1`, id).Scan(&st)
	if st != "sent" {
		t.Fatalf("sent step must stay sent, got %q", st)
	}

	// Очередь разовых писем: письмо ждёт 5 минут, потом уходит.
	if err := repo.EnqueueEmail(ctx, "late@b.ru", "Доступ", "<p>x</p>", "timeout"); err != nil {
		t.Fatal(err)
	}
	if n, _ := repo.OutboxPending(ctx); n != 1 {
		t.Fatalf("pending=%d", n)
	}
	_ = app.RetryOutboxForTest(ctx, time.Now())
	if n, _ := repo.OutboxPending(ctx); n != 1 {
		t.Fatalf("must wait before first retry, pending=%d", n)
	}
	_ = app.RetryOutboxForTest(ctx, time.Now().Add(6*time.Minute))
	if n, _ := repo.OutboxPending(ctx); n != 0 {
		t.Fatalf("must be sent after retry, pending=%d", n)
	}
	// Старше 3 суток – не пытаемся.
	_ = repo.EnqueueEmail(ctx, "late@b.ru", "Старое", "x", "")
	_, _ = repo.Pool.Exec(ctx, `UPDATE email_outbox SET created_at = now() - interval '4 days' WHERE sent_at IS NULL`)
	if n, _ := repo.OutboxPending(ctx); n != 0 {
		t.Fatalf("expired letters must not be pending, got %d", n)
	}
}

// TestTelegramNotifications – новая подписка и заявка приходят в Telegram.
func TestTelegramNotifications(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	if _, err := repo.CreateCourse(ctx, db.CourseInput{Slug: "myagkiy-start", Title: "MS", Kind: "free", IsPublished: true}); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(srv.URL)
	c := newClient(srv)
	c.jar.SetCookies(u, []*http.Cookie{{Name: "src", Value: "reels", Path: "/"}})
	r, body := c.do("POST", "/api/auth/quick-signup", map[string]any{
		"email": "tg@b.ru", "name": "Ольга", "consent_pd": true, "consent_marketing": false})
	if r.StatusCode != 201 {
		t.Fatalf("signup: %d %s", r.StatusCode, body)
	}
	msg := waitRelay(t, "Подписка на «Мягкий старт»")
	if !strings.Contains(msg, "Ольга tg@b.ru") || !strings.Contains(msg, "Откуда: reels") || !strings.Contains(msg, "Без согласия") {
		t.Fatalf("signup message: %q", msg)
	}
	r, body = newClient(srv).do("POST", "/api/leads", map[string]any{
		"name": "Иван <b>", "contact": "@ivan", "source": "coaching", "message": "хочу", "consent_pd": true})
	if r.StatusCode >= 300 {
		t.Fatalf("lead: %d %s", r.StatusCode, body)
	}
	msg = waitRelay(t, "Новая заявка: Личное ведение")
	if !strings.Contains(msg, "Имя: Иван <b>") || !strings.Contains(msg, "@ivan") {
		t.Fatalf("lead message: %q", msg)
	}
}

// Страница после оплаты опрашивает статус заказа: pending → paid,
// почта отдаётся замаскированной; чужой/кривой id – 404.
func TestOrderStatus(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	price := 1490
	if _, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "zhonglirovanie", Title: "Жонглирование", Kind: "paid",
		PriceRub: &price, IsPublished: true,
	}); err != nil {
		t.Fatal(err)
	}
	c := newClient(srv)
	r, body := c.do("POST", "/api/courses/zhonglirovanie/quick-checkout",
		map[string]any{"email": "great-happy@buyer.ru", "name": "Анна", "tariff": "self", "consent_pd": true})
	if r.StatusCode != 200 {
		t.Fatalf("checkout: %d %s", r.StatusCode, body)
	}
	var resp struct {
		OrderID string `json:"order_id"`
	}
	_ = json.Unmarshal(body, &resp)
	oid := uuid.MustParse(resp.OrderID)

	type st struct {
		Status    string `json:"status"`
		EmailHint string `json:"email_hint"`
	}
	get := func() st {
		t.Helper()
		r, body := newClient(srv).do("GET", "/api/orders/"+oid.String()+"/status", nil)
		if r.StatusCode != 200 {
			t.Fatalf("status: %d %s", r.StatusCode, body)
		}
		var s st
		_ = json.Unmarshal(body, &s)
		return s
	}
	if s := get(); s.Status != "pending" || s.EmailHint != "gre•••@buyer.ru" {
		t.Fatalf("pending: %+v", s)
	}
	if err := repo.MarkOrderPaid(ctx, oid, "123"); err != nil {
		t.Fatal(err)
	}
	if s := get(); s.Status != "paid" {
		t.Fatalf("paid: %+v", s)
	}
	for _, id := range []string{"not-a-uuid", uuid.NewString()} {
		if r, _ := newClient(srv).do("GET", "/api/orders/"+id+"/status", nil); r.StatusCode != 404 {
			t.Fatalf("%s: want 404, got %d", id, r.StatusCode)
		}
	}
}

// Зависшая оплата: покупатель вернулся «успешно», подтверждения от Продамуса
// нет 10 минут → одно уведомление Алексею (без повторов). Когда подтверждение
// всё-таки приходит – в сообщении об оплате пометка «пришло с опозданием».
func TestStuckPaymentAlert(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()
	price := 1490
	if _, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "zhonglirovanie", Title: "Жонглирование", Kind: "paid",
		PriceRub: &price, IsPublished: true,
	}); err != nil {
		t.Fatal(err)
	}
	r, body := newClient(srv).do("POST", "/api/courses/zhonglirovanie/quick-checkout",
		map[string]any{"email": "stuck@buyer.ru", "name": "Пётр", "tariff": "self", "consent_pd": true})
	if r.StatusCode != 200 {
		t.Fatalf("checkout: %d %s", r.StatusCode, body)
	}
	var resp struct {
		OrderID string `json:"order_id"`
	}
	_ = json.Unmarshal(body, &resp)
	oid := uuid.MustParse(resp.OrderID)
	if r, _ := newClient(srv).do("GET", "/api/orders/"+oid.String()+"/status?returned=1", nil); r.StatusCode != 200 {
		t.Fatalf("status: %d", r.StatusCode)
	}
	app := &handlers.App{Cfg: cfg, Repo: repo, Mail: email.New("", "", "", "", ""),
		TG: notify.New(testRelay(t).URL, "relay-secret")}
	now := time.Now()
	// 5 минут – ещё рано
	if err := app.CheckStuckPaymentsForTest(ctx, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if repo.WasStuckAlerted(ctx, oid) {
		t.Fatal("alerted too early")
	}
	// 11 минут – уведомление, один раз
	for i := 0; i < 2; i++ {
		if err := app.CheckStuckPaymentsForTest(ctx, now.Add(11*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	msg := waitRelay(t, "Оплата без подтверждения")
	if !strings.Contains(msg, "stuck@buyer.ru") || !strings.Contains(msg, "Жонглирование") || !strings.Contains(msg, "0 ₽") {
		t.Fatalf("stuck message: %q", msg)
	}
	relayMu.Lock()
	n := 0
	for _, m := range relayMsgs {
		if strings.Contains(m, "stuck@buyer.ru") && strings.Contains(m, "Оплата без подтверждения") {
			n++
		}
	}
	relayMu.Unlock()
	if n != 1 {
		t.Fatalf("want 1 stuck alert, got %d", n)
	}
	// подтверждение пришло с опозданием
	wh := map[string]any{"order_id": oid.String(), "payment_status": "success"}
	sig, _ := prodamus.Sign(cfg.ProdamusSecret, wh)
	form := url.Values{}
	for k, v := range wh {
		form.Set(k, v.(string))
	}
	form.Set("signature", sig)
	req, _ := http.NewRequest("POST", srv.URL+"/api/webhooks/prodamus", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sign", sig)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	msg = waitRelay(t, "Подтверждение пришло с опозданием")
	if !strings.Contains(msg, "stuck@buyer.ru") {
		t.Fatalf("late message: %q", msg)
	}
}

// Брошенная оплата: перешёл к оплате и не оплатил за час → одно уведомление.
// Не тревожим, если человек потом попробовал ещё раз (уведомление только по
// последнему заказу) или вернулся с оплаты (это «зависшая» оплата).
func TestAbandonedPaymentAlert(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()
	price := 1490
	if _, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "zhonglirovanie", Title: "Жонглирование", Kind: "paid",
		PriceRub: &price, IsPublished: true,
	}); err != nil {
		t.Fatal(err)
	}
	buy := func(email string) string {
		t.Helper()
		r, body := newClient(srv).do("POST", "/api/courses/zhonglirovanie/quick-checkout",
			map[string]any{"email": email, "name": "Покупатель", "tariff": "self", "consent_pd": true})
		if r.StatusCode != 200 {
			t.Fatalf("checkout %s: %d %s", email, r.StatusCode, body)
		}
		var resp struct {
			OrderID string `json:"order_id"`
		}
		_ = json.Unmarshal(body, &resp)
		return resp.OrderID
	}
	buy("gone@buyer.ru")
	buy("retry@buyer.ru")
	retryLast := buy("retry@buyer.ru")
	back := buy("back@buyer.ru")
	if r, _ := newClient(srv).do("GET", "/api/orders/"+back+"/status?returned=1", nil); r.StatusCode != 200 {
		t.Fatal("status")
	}
	app := &handlers.App{Cfg: cfg, Repo: repo, Mail: email.New("", "", "", "", ""),
		TG: notify.New(testRelay(t).URL, "relay-secret")}
	now := time.Now()
	count := func(substr string) int {
		relayMu.Lock()
		defer relayMu.Unlock()
		n := 0
		for _, m := range relayMsgs {
			if strings.Contains(m, "Не завершил оплату") && strings.Contains(m, substr) {
				n++
			}
		}
		return n
	}
	if err := app.CheckAbandonedPaymentsForTest(ctx, now.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if count("@buyer.ru") != 0 {
		t.Fatal("alerted too early")
	}
	for i := 0; i < 2; i++ {
		if err := app.CheckAbandonedPaymentsForTest(ctx, now.Add(61*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	msg := waitRelay(t, "gone@buyer.ru")
	if !strings.Contains(msg, "Жонглирование") || !strings.Contains(msg, "0 ₽") {
		t.Fatalf("abandoned message: %q", msg)
	}
	waitRelay(t, "retry@buyer.ru")
	time.Sleep(100 * time.Millisecond)
	if count("gone@buyer.ru") != 1 || count("retry@buyer.ru") != 1 || count("back@buyer.ru") != 0 {
		t.Fatalf("counts: gone=%d retry=%d back=%d", count("gone@buyer.ru"), count("retry@buyer.ru"), count("back@buyer.ru"))
	}
	o, _ := repo.GetOrder(ctx, uuid.MustParse(retryLast))
	if !strings.Contains(waitRelay(t, "retry@buyer.ru"), "№"+strconv.FormatInt(o.OrderNum, 10)) {
		t.Fatal("retry alert should be about the latest order")
	}
	// через сутки старые заказы уже не поднимаем
	before := count("@buyer.ru")
	if err := app.CheckAbandonedPaymentsForTest(ctx, now.Add(26*time.Hour)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if count("@buyer.ru") != before {
		t.Fatal("old orders must not be alerted")
	}
}

// Вход по коду из письма: код приходит только на существующий адрес, не чаще
// раза в минуту, неверный код не пускает, после 5 ошибок код сгорает.
func TestLoginByCode(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	if _, err := repo.CreateUser(ctx, "code@user.ru", "!unusable!x", "Ира", "user"); err != nil {
		t.Fatal(err)
	}
	c := newClient(srv)
	// Неизвестный адрес – тот же ответ, кода нет.
	r, body := c.do("POST", "/api/auth/code", map[string]any{"email": "nobody@user.ru"})
	if r.StatusCode != 200 || strings.Contains(string(body), "code_dev") {
		t.Fatalf("unknown email: %d %s", r.StatusCode, body)
	}
	r, body = c.do("POST", "/api/auth/code", map[string]any{"email": "Code@User.ru "})
	var resp struct {
		Code string `json:"code_dev"`
	}
	_ = json.Unmarshal(body, &resp)
	if r.StatusCode != 200 || len(resp.Code) != 6 {
		t.Fatalf("request code: %d %s", r.StatusCode, body)
	}
	// Повтор в течение минуты – новый код не создаётся.
	_, body = c.do("POST", "/api/auth/code", map[string]any{"email": "code@user.ru"})
	if strings.Contains(string(body), "code_dev") {
		t.Fatalf("second code within a minute: %s", body)
	}
	wrong := "000000"
	if resp.Code == wrong {
		wrong = "111111"
	}
	if r, _ := c.do("POST", "/api/auth/code/verify", map[string]any{"email": "code@user.ru", "code": wrong}); r.StatusCode != 400 {
		t.Fatalf("wrong code: %d", r.StatusCode)
	}
	if r, _ := c.do("GET", "/api/me", nil); r.StatusCode != 401 {
		t.Fatalf("must not be logged in after wrong code: %d", r.StatusCode)
	}
	r, body = c.do("POST", "/api/auth/code/verify", map[string]any{"email": "code@user.ru", "code": resp.Code[:3] + " " + resp.Code[3:]})
	if r.StatusCode != 200 {
		t.Fatalf("right code: %d %s", r.StatusCode, body)
	}
	if r, _ := c.do("GET", "/api/me", nil); r.StatusCode != 200 {
		t.Fatalf("me after code login: %d", r.StatusCode)
	}
	// Использованный код второй раз не работает.
	c2 := newClient(srv)
	if r, _ := c2.do("POST", "/api/auth/code/verify", map[string]any{"email": "code@user.ru", "code": resp.Code}); r.StatusCode != 400 {
		t.Fatalf("reused code: %d", r.StatusCode)
	}
	// Пять неверных попыток – код сгорает, даже верный больше не пускает.
	if _, err := repo.Pool.Exec(ctx, `UPDATE login_codes SET created_at = now() - interval '2 minutes'`); err != nil {
		t.Fatal(err)
	}
	_, body = c2.do("POST", "/api/auth/code", map[string]any{"email": "code@user.ru"})
	_ = json.Unmarshal(body, &resp)
	wrong = "000000"
	if resp.Code == wrong {
		wrong = "111111"
	}
	for i := 0; i < 5; i++ {
		c2.do("POST", "/api/auth/code/verify", map[string]any{"email": "code@user.ru", "code": wrong})
	}
	r, body = c2.do("POST", "/api/auth/code/verify", map[string]any{"email": "code@user.ru", "code": resp.Code})
	if r.StatusCode != 400 || !strings.Contains(string(body), "too_many_attempts") {
		t.Fatalf("after 5 wrong attempts: %d %s", r.StatusCode, body)
	}
	// Без CSRF – отказ.
	if r, _ := c2.doNoCSRF("POST", "/api/auth/code", map[string]any{"email": "code@user.ru"}); r.StatusCode != 403 {
		t.Fatalf("no csrf: %d", r.StatusCode)
	}
}

// Письмо о доступе: человеку, который не знает пароль, генерируется пароль
// и кнопка входа без пароля (многоразовая, ведёт к курсу). Тому, кто уже
// входил, пароль не меняем.
func TestAccessEmailPasswordAndLink(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()
	app := &handlers.App{Cfg: cfg, Repo: repo, Mail: email.New("", "", "", "", "")}
	uid, err := repo.CreateUser(ctx, "buyer@mail.ru", "!unusable!x", "Оля", "user")
	if err != nil {
		t.Fatal(err)
	}
	body, pwd, err := app.BuildAccessEmailForTest(ctx, uid, "zhonglirovanie")
	if err != nil {
		t.Fatal(err)
	}
	if len(pwd) != 10 || !strings.Contains(body, pwd) || !strings.Contains(body, "buyer@mail.ru") {
		t.Fatalf("password must be generated and shown: %q", pwd)
	}
	if !strings.Contains(body, "Хочу получать") && !strings.Contains(body, "Да, присылайте") {
		t.Fatal("no subscribe offer for a person without consent")
	}
	i := strings.Index(body, "/auth/link?token=")
	if i < 0 {
		t.Fatal("no login link")
	}
	token := body[i+len("/auth/link?token="):]
	token = token[:strings.IndexByte(token, '"')]

	// Пароль из письма подходит.
	c := newClient(srv)
	if r, b := c.do("POST", "/api/auth/login", map[string]any{"email": "buyer@mail.ru", "password": pwd}); r.StatusCode != 200 {
		t.Fatalf("login with emailed password: %d %s", r.StatusCode, b)
	}
	// Кнопка из письма входит и ведёт к курсу – и не один раз.
	for k := 0; k < 2; k++ {
		c2 := newClient(srv)
		r, b := c2.do("POST", "/api/auth/link", map[string]any{"token": token})
		if r.StatusCode != 200 || !strings.Contains(string(b), `"/cabinet/zhonglirovanie"`) {
			t.Fatalf("link login #%d: %d %s", k, r.StatusCode, b)
		}
		if r, _ := c2.do("GET", "/api/me", nil); r.StatusCode != 200 {
			t.Fatalf("me after link: %d", r.StatusCode)
		}
	}
	if r, _ := newClient(srv).do("POST", "/api/auth/link", map[string]any{"token": "deadbeef"}); r.StatusCode != 400 {
		t.Fatalf("bad token: %d", r.StatusCode)
	}
	// Просроченная ссылка не работает.
	_, _ = repo.Pool.Exec(ctx, `UPDATE login_links SET expires_at = now() - interval '1 minute'`)
	if r, _ := newClient(srv).do("POST", "/api/auth/link", map[string]any{"token": token}); r.StatusCode != 400 {
		t.Fatalf("expired link: %d", r.StatusCode)
	}
	// Человек уже входил – второе письмо пароль не меняет.
	body, pwd2, _ := app.BuildAccessEmailForTest(ctx, uid, "zhonglirovanie")
	if pwd2 != "" || strings.Contains(body, pwd) {
		t.Fatalf("password must not change for a person who logged in: %q", pwd2)
	}
	if r, _ := newClient(srv).do("POST", "/api/auth/login", map[string]any{"email": "buyer@mail.ru", "password": pwd}); r.StatusCode != 200 {
		t.Fatalf("old password must still work: %d", r.StatusCode)
	}
}

// Вход сразу после оплаты: браузер, оформивший заказ, попадает к урокам без
// почты; чужой браузер и аккаунт, в который уже входили, – нет.
func TestBuyerEntersAfterPayment(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()
	price := 1490
	if _, err := repo.CreateCourse(ctx, db.CourseInput{
		Slug: "zhonglirovanie", Title: "Жонглирование", Kind: "paid",
		PriceRub: &price, IsPublished: true,
	}); err != nil {
		t.Fatal(err)
	}
	buyer := newClient(srv)
	r, body := buyer.do("POST", "/api/courses/zhonglirovanie/quick-checkout",
		map[string]any{"email": "enter@buyer.ru", "name": "Анна", "tariff": "self", "consent_pd": true, "consent_marketing": true})
	if r.StatusCode != 200 {
		t.Fatalf("checkout: %d %s", r.StatusCode, body)
	}
	var resp struct {
		OrderID string `json:"order_id"`
	}
	_ = json.Unmarshal(body, &resp)
	u, _ := repo.GetUserByEmail(ctx, "enter@buyer.ru")
	if ok, _ := repo.HasMarketingConsent(ctx, u.ID); !ok {
		t.Fatal("checkbox in the checkout form must save marketing consent")
	}
	noRedirect := func(c *client) *http.Client {
		h := c.http()
		h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		return h
	}
	enter := func(c *client) string {
		t.Helper()
		res, err := noRedirect(c).Get(srv.URL + "/api/orders/" + resp.OrderID + "/enter")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.Header.Get("Location")
	}
	canEnter := func(c *client) bool {
		_, b := c.do("GET", "/api/orders/"+resp.OrderID+"/status", nil)
		return strings.Contains(string(b), `"can_enter":true`)
	}
	// Не оплачено – никуда не пускаем.
	if loc := enter(buyer); !strings.HasPrefix(loc, "/auth/login") {
		t.Fatalf("unpaid: %q", loc)
	}
	// Подтверждение оплаты.
	wh := map[string]any{"order_id": resp.OrderID, "payment_status": "success"}
	sig, _ := prodamus.Sign(cfg.ProdamusSecret, wh)
	form := url.Values{}
	for k, v := range wh {
		form.Set(k, v.(string))
	}
	form.Set("signature", sig)
	req, _ := http.NewRequest("POST", srv.URL+"/api/webhooks/prodamus", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sign", sig)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	other := newClient(srv)
	if canEnter(other) || !canEnter(buyer) {
		t.Fatal("can_enter must be true only in the buyer's browser")
	}
	if loc := enter(other); !strings.HasPrefix(loc, "/auth/login?email=enter%40buyer.ru") {
		t.Fatalf("other browser: %q", loc)
	}
	if loc := enter(buyer); loc != "/cabinet/zhonglirovanie" {
		t.Fatalf("buyer: %q", loc)
	}
	if r, _ := buyer.do("GET", "/api/me", nil); r.StatusCode != 200 {
		t.Fatalf("buyer must be logged in: %d", r.StatusCode)
	}
	// Подделанная кука не пускает.
	forged := newClient(srv)
	su, _ := url.Parse(srv.URL)
	forged.jar.SetCookies(su, []*http.Cookie{{Name: "buyer_order", Value: resp.OrderID + ".00ff", Path: "/"}})
	if loc := enter(forged); !strings.HasPrefix(loc, "/auth/login") {
		t.Fatalf("forged cookie: %q", loc)
	}
}

// Отписка запоминается: отписавшемуся не предлагаем подписаться и не шлём
// сервисных рассылок; кнопка «Да, присылайте» возвращает всё как было.
func TestUnsubscribedIsRemembered(t *testing.T) {
	srv, repo, _ := setup(t)
	ctx := context.Background()
	freeID, _ := repo.CreateCourse(ctx, db.CourseInput{Slug: "myagkiy-start", Title: "MS", Kind: "free", IsPublished: true})
	mk := func(email string, consent bool) uuid.UUID {
		id, err := repo.CreateUser(ctx, email, "x", "", "user")
		if err != nil {
			t.Fatal(err)
		}
		_ = repo.SaveConsent(ctx, id, true, consent)
		_ = repo.Grant(ctx, id, freeID, "free", nil)
		return id
	}
	never := mk("never@b.ru", false)
	left := mk("left@b.ru", true)
	_ = repo.ClearMarketingConsent(ctx, left)
	if ok, _ := repo.SubscribeOfferAllowed(ctx, never); !ok {
		t.Fatal("never-subscribed person must get the offer")
	}
	if ok, _ := repo.SubscribeOfferAllowed(ctx, left); ok {
		t.Fatal("unsubscribed person must not get the offer")
	}
	admin := adminClient(t, srv, repo)
	_, body := admin.do("GET", "/api/admin/segments", nil)
	var segs struct {
		Segments []db.Segment `json:"segments"`
	}
	_ = json.Unmarshal(body, &segs)
	found := false
	for _, s := range segs.Segments {
		if s.Key == "service_all" {
			found = true
			if s.Count != 1 || !s.ServiceOnly {
				t.Fatalf("service_all must hold only never@: %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("no service_all segment")
	}
	_ = repo.SetMarketingConsent(ctx, left)
	var unsub *time.Time
	_ = repo.Pool.QueryRow(ctx, `SELECT unsubscribed_at FROM users WHERE id=$1`, left).Scan(&unsub)
	if unsub != nil {
		t.Fatal("subscribing again must clear unsubscribed_at")
	}
}

// Письма цепочки 1–3 без согласия: без рекламных блоков, с кнопкой подписки.
func TestDripLetterWithoutConsent(t *testing.T) {
	app := &handlers.App{Cfg: &config.Config{AppHost: "http://test", JWTSecret: "s"}}
	sub := app.DripHTMLForTest(3, true)
	nosub := app.DripHTMLForTest(3, false)
	if !strings.Contains(sub, "%2Fresults") || strings.Contains(sub, "[[promo]]") {
		t.Fatal("subscribed letter must keep the reviews block")
	}
	if strings.Contains(nosub, "results") || !strings.Contains(nosub, "/api/subscribe?u=") {
		t.Fatal("letter without consent: no promo, with subscribe button")
	}
	if strings.Contains(sub, "/api/subscribe?u=") {
		t.Fatal("subscribed letter must not offer subscribing")
	}
}

// Переходы по ссылкам из писем: подписанная ссылка записывает переход (и
// открытие) и ведёт на страницу; с чужой подписью – на главную, без записи.
func TestEmailClick(t *testing.T) {
	srv, repo, cfg := setup(t)
	ctx := context.Background()
	app := &handlers.App{Cfg: cfg, Repo: repo, Mail: email.New("", "", "", "", "")}
	uid, err := repo.CreateUser(ctx, "click@b.ru", "x", "", "user")
	if err != nil {
		t.Fatal(err)
	}
	h := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(u string) string {
		t.Helper()
		res, err := h.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 302 {
			t.Fatalf("want 302, got %d", res.StatusCode)
		}
		return res.Header.Get("Location")
	}
	count := func(table string) int {
		var n int
		_ = repo.Pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE user_id=$1 AND campaign='drip-1'`, uid).Scan(&n)
		return n
	}
	link := "https://leshalarin.ru/cabinet/myagkiy-start#itogovyy"
	tracked := app.TrackURLForTest("drip-1", uid, link)
	path := strings.TrimPrefix(tracked, cfg.AppHost)
	if loc := get(srv.URL + path); loc != link {
		t.Fatalf("redirect: %q", loc)
	}
	if count("email_clicks") != 1 || count("email_opens") != 1 {
		t.Fatal("click and open must be logged")
	}
	// Подменили ссылку – подпись не сходится: на главную, без записи.
	evil := strings.Replace(path, url.QueryEscape(link), url.QueryEscape("https://evil.example/"), 1)
	if loc := get(srv.URL + evil); loc != "/" {
		t.Fatalf("forged link: %q", loc)
	}
	if count("email_clicks") != 1 {
		t.Fatal("forged click must not be logged")
	}
	// Ссылки в письме цепочки ведут через счётчик и подписаны для человека.
	body := app.DripHTMLForUserTest(1, uid)
	if !strings.Contains(body, "/api/c?u="+uid.String()+"&amp;c=drip-1&amp;l=https%3A%2F%2Fleshalarin.ru%2Fcabinet%2Fmyagkiy-start") {
		t.Fatal("drip letter links must be tracked")
	}
	if strings.Contains(body, `href="https://leshalarin.ru/cabinet/myagkiy-start"`) {
		t.Fatal("untracked link left in the letter")
	}
}
