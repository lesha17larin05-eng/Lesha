package main

import (
	"context"
	"fmt"
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
	"github.com/leshalarin/api/internal/notify"
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

	mail := email.New(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPFrom)
	mail.ReplyTo = cfg.SMTPReplyTo
	if cfg.SMTP2Host != "" {
		mail.Fallback = email.New(cfg.SMTP2Host, cfg.SMTP2Port, cfg.SMTP2User, cfg.SMTP2Password, cfg.SMTP2From)
	}
	// Письмо не ушло ни через один ящик – в очередь повторной отправки.
	mail.OnAsyncFail = func(to, subject, body string, err error) {
		if qerr := repo.EnqueueEmail(context.Background(), to, subject, body, err.Error()); qerr != nil {
			slog.Error("email enqueue failed", "err", qerr, "to", to)
		}
	}

	app := &handlers.App{
		Cfg:      cfg,
		Repo:     repo,
		Mail:     mail,
		Prodamus: prodamus.New(cfg.ProdamusPayformURL, cfg.ProdamusSecret),
		TG:       notify.New(cfg.TelegramRelayURL, cfg.TelegramRelaySecret),
	}

	// Разовая проверка: /app/server tg-test – сообщение в Telegram.
	if len(os.Args) > 1 && os.Args[1] == "tg-test" {
		if err := app.TG.Send("🔔 Проверка: уведомления с сайта leshalarin.ru работают."); err != nil || !app.TG.Enabled() {
			fmt.Println("FAIL", err, "enabled:", app.TG.Enabled())
			os.Exit(1)
		}
		fmt.Println("OK")
		os.Exit(0)
	}

	// Разовые команды: /app/server resend-access a@b.ru [c@d.ru ...]
	if len(os.Args) > 2 && os.Args[1] == "resend-access" {
		code := 0
		for _, addr := range os.Args[2:] {
			if err := app.ResendFreeAccess(ctx, addr); err != nil {
				fmt.Println("FAIL", addr, err)
				code = 1
				continue
			}
			fmt.Println("OK", addr)
		}
		os.Exit(code)
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
		r.With(middleware.Honeypot).Post("/register", app.Register)
		r.With(middleware.Honeypot).Post("/quick-signup", app.QuickSignup)
		r.Post("/login", app.Login)
		r.Post("/logout", app.Logout)
		r.Post("/refresh", app.Refresh)
		r.Post("/verify-email", app.VerifyEmail)
		r.Post("/fix-email", app.FixEmail)
		r.Post("/resend-verification", app.ResendVerification)
		r.Post("/forgot-password", app.ForgotPassword)
		r.Post("/reset-password", app.ResetPassword)
		// Вход без пароля: кнопка из письма о доступе и код на почту.
		r.Post("/link", app.LoginByLink)
		r.Post("/code", app.RequestLoginCode)
		r.Post("/code/verify", app.VerifyLoginCode)
	})

	r.Get("/api/courses", app.ListCourses)
	r.Get("/api/courses/{slug}", app.GetCourse)
	r.Get("/api/courses/{slug}/lessons/{lesson}", app.GetLesson)
	r.Get("/api/settings", app.PublicSettings)
	r.Get("/api/pixel.gif", app.EmailPixel)
	r.Get("/api/c", app.EmailClick) // переход по ссылке из письма
	r.Get("/api/unsubscribe", app.Unsubscribe)
	r.Post("/api/unsubscribe", app.Unsubscribe)
	r.Get("/api/unsubscribe/status", app.UnsubscribeStatus)
	r.Post("/api/unsubscribe/undo", app.UnsubscribeUndo)
	r.Get("/api/anketa", app.GetAnketa)
	r.Post("/api/anketa", app.SubmitAnketa)
	r.Get("/api/subscribe", app.Subscribe)
	r.Post("/api/subscribe", app.Subscribe)
	// Форма «получать письма» в блоге и под отзывами.
	r.With(middleware.Honeypot).Post("/api/newsletter", app.NewsletterSignup)
	// Оплата услуги без регистрации: «Точка перемен».
	r.With(middleware.Honeypot).Post("/api/checkout/service", app.ServiceCheckout)
	r.Get("/api/services/{service}/seats", app.ServiceSeats)
	// Покупка курса в один шаг (имя + почта → оплата), без регистрации.
	r.With(middleware.Honeypot).Post("/api/courses/{slug}/quick-checkout", app.QuickCheckout)
	r.Get("/api/orders/{id}/status", app.OrderStatus)
	r.Get("/api/orders/{id}/enter", app.OrderEnter)
	r.Get("/api/articles", app.ListArticles)
	r.Get("/api/articles/{slug}", app.GetArticle)
	// Маяк счётчика чтения: публичный, без CSRF (см. TrackArticleView).
	r.Post("/api/articles/{slug}/view", app.TrackArticleView)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)
		r.Get("/api/me", app.Me)
		r.Patch("/api/me", app.PatchMe)
		r.Get("/api/me/courses", app.MyCourses)
		r.Get("/api/me/continue", app.MeContinue)
		r.Post("/api/lessons/{id}/progress", app.PostProgress)
		r.Post("/api/courses/{slug}/enroll-free", app.EnrollFree)
		r.Post("/api/courses/{slug}/checkout", app.Checkout)
		r.Get("/api/courses/{slug}/files/{name}", app.CourseFile)
		r.Get("/api/videos/{id}/playback", app.VideoPlayback)
	})

	// Заявки с маркетинговых страниц: rate-limit против спама (CSRF уже глобально).
	leadRL := middleware.NewRateLimiter(10, 15*time.Minute)
	r.With(leadRL.Middleware, middleware.Honeypot).Post("/api/leads", app.CreateLead)

	r.Post("/api/webhooks/prodamus", app.ProdamusWebhook)
	r.Get("/api/pay/{order_id}", app.PayShortcut)
	r.Get("/api/dev/fake-payment", app.FakePayment)
	r.Get("/api/internal/video-auth", app.InternalVideoAuth)
	r.Get("/api/internal/lesson-mp4-auth", app.LessonMp4Auth)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)
		r.Use(middleware.RequireAdmin)
		r.Get("/api/admin/stats", app.AdminStats)
		r.Patch("/api/admin/settings", app.AdminUpdateSettings)
		r.Get("/api/admin/users", app.AdminUsers)
		r.Get("/api/admin/users/export.csv", app.AdminUsersExport)
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
		r.Get("/api/admin/activity", app.AdminActivity)
		r.Get("/api/admin/email-opens", app.AdminEmailOpens)
		r.Get("/api/admin/segments", app.AdminSegments)
		r.Get("/api/admin/campaigns", app.AdminListCampaigns)
		r.Post("/api/admin/campaigns", app.AdminCreateCampaign)
		r.Get("/api/admin/campaigns/{id}", app.AdminGetCampaign)
		r.Post("/api/admin/campaigns/{id}/test", app.AdminTestCampaign)
		r.Post("/api/admin/campaigns/{id}/{action}", app.AdminCampaignAction)
		r.Get("/api/admin/leads", app.AdminLeads)
		r.Get("/api/admin/leads.csv", app.AdminLeadsCSV)
		r.Post("/api/admin/leads", app.AdminCreateLead)
		r.Post("/api/admin/leads/bulk", app.AdminBulkLeads)
		r.Patch("/api/admin/leads/{id}", app.AdminUpdateLead)
		r.Get("/api/admin/orders", app.AdminOrders)
		r.Post("/api/admin/orders/{id}/refund", app.AdminRefundOrder)
		r.Get("/api/admin/online-users", app.AdminOnline)
		r.Get("/api/admin/audit-log", app.AdminAuditLog)
		r.Get("/api/admin/articles", app.AdminListArticles)
		r.Post("/api/admin/articles", app.AdminCreateArticle)
		r.Get("/api/admin/articles/{id}", app.AdminGetArticle)
		r.Patch("/api/admin/articles/{id}", app.AdminUpdateArticle)
		r.Delete("/api/admin/articles/{id}", app.AdminDeleteArticle)
		r.Get("/api/admin/article-stats", app.AdminArticleStats)
		r.Get("/api/admin/drip", app.AdminDrip)
		r.Get("/api/admin/sources", app.AdminSources)
		r.Get("/api/admin/unsubscribes", app.AdminUnsubscribes)
		r.Post("/api/admin/anketa", app.AdminAnketa)
		r.Post("/api/admin/drip/test", app.AdminDripTest)
	})

	// Фоновая отправка рассылок: одно письмо раз в несколько десятков секунд,
	// в пределах дневного лимита кампании (см. handlers/campaigns.go).
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	go handlers.RunCampaignWorker(workerCtx, app)
	// Автоцепочка писем после «Мягкого старта» (handlers/drip.go).
	go handlers.RunDripWorker(workerCtx, app)
	go handlers.RunOutboxWorker(workerCtx, app)
	go handlers.RunMailMonitor(workerCtx, app)
	go handlers.RunStuckPaymentWatcher(workerCtx, app)

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
