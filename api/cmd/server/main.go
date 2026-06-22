package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/leshalarin/api/internal/config"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/email"
	"github.com/leshalarin/api/internal/handlers"
	"github.com/leshalarin/api/internal/middleware"
	"github.com/leshalarin/api/internal/prodamus"
	"github.com/leshalarin/api/internal/seed"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg := config.Load()

	ctx := context.Background()
	pool, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect", "err", err)
		os.Exit(1)
	}
	repo := db.NewRepo(pool)

	if err := seed.Run(ctx, repo, cfg); err != nil {
		slog.Warn("seed warning", "err", err)
	}

	app := &handlers.App{
		Cfg:      cfg,
		Repo:     repo,
		Mail:     email.New(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPFrom),
		Prodamus: prodamus.New(cfg.ProdamusPayformURL, cfg.ProdamusSecret),
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recover)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS(cfg.CORSOrigin))
	r.Use(middleware.Auth(cfg.JWTSecret, repo))
	r.Use(middleware.CSRF)

	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":1}`))
	})

	authRL := middleware.NewRateLimiter(20, 15*time.Minute)
	r.Route("/api/auth", func(r chi.Router) {
		r.Use(authRL.Middleware)
		r.Post("/register", app.Register)
		r.Post("/quick-signup", app.QuickSignup)
		r.Post("/login", app.Login)
		r.Post("/logout", app.Logout)
		r.Post("/refresh", app.Refresh)
		r.Post("/verify-email", app.VerifyEmail)
		r.Post("/forgot-password", app.ForgotPassword)
		r.Post("/reset-password", app.ResetPassword)
	})

	r.Get("/api/courses", app.ListCourses)
	r.Get("/api/courses/{slug}", app.GetCourse)
	r.Get("/api/courses/{slug}/lessons/{lesson}", app.GetLesson)
	r.Get("/api/articles", app.ListArticles)
	r.Get("/api/articles/{slug}", app.GetArticle)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)
		r.Get("/api/me", app.Me)
		r.Patch("/api/me", app.PatchMe)
		r.Get("/api/me/courses", app.MyCourses)
		r.Post("/api/lessons/{id}/progress", app.PostProgress)
		r.Post("/api/courses/{slug}/enroll-free", app.EnrollFree)
		r.Post("/api/courses/{slug}/checkout", app.Checkout)
		r.Get("/api/courses/{slug}/files/{name}", app.CourseFile)
		r.Get("/api/videos/{id}/playback", app.VideoPlayback)
	})

	r.Post("/api/webhooks/prodamus", app.ProdamusWebhook)
	r.Get("/api/pay/{order_id}", app.PayShortcut)
	r.Get("/api/dev/fake-payment", app.FakePayment)
	r.Get("/api/internal/video-auth", app.InternalVideoAuth)
	r.Get("/api/internal/lesson-mp4-auth", app.LessonMp4Auth)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)
		r.Use(middleware.RequireAdmin)
		r.Get("/api/admin/stats", app.AdminStats)
		r.Get("/api/admin/users", app.AdminUsers)
		r.Get("/api/admin/users/{id}", app.AdminUser)
		r.Delete("/api/admin/users/{id}", app.AdminDeleteUser)
		r.Post("/api/admin/users/{id}/enrollments", app.AdminGrant)
		r.Delete("/api/admin/users/{id}/enrollments/{course_id}", app.AdminRevoke)
		r.Post("/api/admin/courses/{slug}/grant-by-email", app.AdminGrantByEmail)
		r.Get("/api/admin/courses", app.AdminListCourses)
		r.Post("/api/admin/courses", app.AdminCreateCourse)
		r.Get("/api/admin/courses/{id}", app.AdminGetCourse)
		r.Patch("/api/admin/courses/{id}", app.AdminUpdateCourse)
		r.Delete("/api/admin/courses/{id}", app.AdminDeleteCourse)
		r.Post("/api/admin/modules", app.AdminCreateModule)
		r.Patch("/api/admin/modules/{id}", app.AdminUpdateModule)
		r.Delete("/api/admin/modules/{id}", app.AdminDeleteModule)
		r.Post("/api/admin/lessons", app.AdminCreateLesson)
		r.Get("/api/admin/lessons/{id}", app.AdminGetLesson)
		r.Patch("/api/admin/lessons/{id}", app.AdminUpdateLesson)
		r.Delete("/api/admin/lessons/{id}", app.AdminDeleteLesson)
		r.Post("/api/admin/videos/upload", app.AdminUploadVideo)
		r.Get("/api/admin/orders", app.AdminOrders)
		r.Get("/api/admin/online-users", app.AdminOnline)
		r.Get("/api/admin/audit-log", app.AdminAuditLog)
		r.Get("/api/admin/articles", app.AdminListArticles)
		r.Post("/api/admin/articles", app.AdminCreateArticle)
		r.Get("/api/admin/articles/{id}", app.AdminGetArticle)
		r.Patch("/api/admin/articles/{id}", app.AdminUpdateArticle)
		r.Delete("/api/admin/articles/{id}", app.AdminDeleteArticle)
	})

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", ":8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	slog.Info("shutting down")
	ctxShut, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctxShut)
}
