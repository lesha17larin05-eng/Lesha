package seed

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/auth"
	"github.com/leshalarin/api/internal/config"
	"github.com/leshalarin/api/internal/db"
)

func Run(ctx context.Context, repo *db.Repo, cfg *config.Config) error {
	adminID, err := ensureUser(ctx, repo, cfg.AdminEmail, cfg.AdminPassword, "Алексей Ларин", "admin", true)
	if err != nil {
		return err
	}
	userID, err := ensureUser(ctx, repo, cfg.UserEmail, cfg.UserPassword, "Тестовый Ученик", "user", true)
	if err != nil {
		return err
	}
	_ = userID
	freeID, err := ensureCourse(ctx, repo, db.CourseInput{
		Slug:        "intro-zdorovye",
		Title:       "Знакомство со здоровьем — бесплатный курс",
		Subtitle:    "5 уроков о фундаменте крепкого здоровья",
		Description: "Бесплатный вводный курс: основы сна, дыхания, движения и питания. Отличная отправная точка перед платными программами.",
		Kind:        "free",
		IsPublished: true,
		SortOrder:   1,
	})
	if err != nil {
		return err
	}
	paidPrice := 9900
	paidID, err := ensureCourse(ctx, repo, db.CourseInput{
		Slug:        "transformaciya-90",
		Title:       "Трансформация за 90 дней",
		Subtitle:    "Платная программа с поддержкой тренера",
		Description: "Глубокая программа: система привычек, тренировки, восстановление, нутриентная поддержка. 12 модулей, видео, чек-листы.",
		Kind:        "paid",
		PriceRub:    &paidPrice,
		IsPublished: true,
		SortOrder:   2,
	})
	if err != nil {
		return err
	}
	if err := ensureFreeLessons(ctx, repo, freeID); err != nil {
		return err
	}
	if err := ensurePaidLessons(ctx, repo, paidID); err != nil {
		return err
	}
	// auto-enroll admin into both, user into free
	_ = repo.Grant(ctx, adminID, freeID, "admin", &adminID)
	_ = repo.Grant(ctx, adminID, paidID, "admin", &adminID)
	_ = repo.Grant(ctx, userID, freeID, "free", nil)
	if err := SeedArticles(ctx, repo, adminID); err != nil {
		slog.Warn("articles seed", "err", err)
	}
	slog.Info("seed ok", "admin", cfg.AdminEmail, "user", cfg.UserEmail)
	return nil
}

func ensureUser(ctx context.Context, repo *db.Repo, email, password, name, role string, verified bool) (uuid.UUID, error) {
	if u, err := repo.GetUserByEmail(ctx, email); err == nil {
		return u.ID, nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := repo.CreateUser(ctx, email, hash, name, role)
	if err != nil {
		return uuid.Nil, err
	}
	if verified {
		_ = repo.MarkEmailVerified(ctx, id)
	}
	return id, nil
}

func ensureCourse(ctx context.Context, repo *db.Repo, in db.CourseInput) (uuid.UUID, error) {
	if c, err := repo.GetCourseBySlug(ctx, in.Slug); err == nil {
		return c.ID, nil
	}
	return repo.CreateCourse(ctx, in)
}

func ensureFreeLessons(ctx context.Context, repo *db.Repo, courseID uuid.UUID) error {
	existing, _ := repo.ListLessons(ctx, courseID)
	if len(existing) > 0 {
		return nil
	}
	lessons := []db.LessonInput{
		{Title: "Зачем нужен системный подход", Slug: "why", ContentMD: "## Введение\n\nЗдоровье — это система. В этом уроке мы разберём, почему точечные практики работают плохо без рамки.", SortOrder: 1, IsPreview: true},
		{Title: "Сон как фундамент", Slug: "sleep", ContentMD: "## Сон\n\n- Циркадные ритмы\n- Что делает свет\n- Как ложиться вовремя", SortOrder: 2},
		{Title: "Дыхание", Slug: "breath", ContentMD: "## Дыхание\n\nНосовое дыхание, диафрагма, простые упражнения.", SortOrder: 3},
		{Title: "Движение", Slug: "move", ContentMD: "## Движение\n\nMVP-программа на каждый день.", SortOrder: 4},
		{Title: "Что дальше", Slug: "next", ContentMD: "## Финал\n\nКуда двигаться после вводного курса.", SortOrder: 5},
	}
	for _, l := range lessons {
		l.CourseID = courseID
		if _, err := repo.CreateLesson(ctx, l); err != nil {
			return err
		}
	}
	return nil
}

func ensurePaidLessons(ctx context.Context, repo *db.Repo, courseID uuid.UUID) error {
	existing, _ := repo.ListLessons(ctx, courseID)
	if len(existing) > 0 {
		return nil
	}
	mod1, err := repo.CreateModule(ctx, courseID, "Модуль 1. Диагностика", 1)
	if err != nil {
		return err
	}
	mod2, err := repo.CreateModule(ctx, courseID, "Модуль 2. Трансформация", 2)
	if err != nil {
		return err
	}
	lessons := []db.LessonInput{
		{Title: "С чего начать (превью)", Slug: "start", ContentMD: "## С чего начать\n\nЭто бесплатный пробный урок.", SortOrder: 1, IsPreview: true, ModuleID: &mod1},
		{Title: "Анализ привычек", Slug: "habits", ContentMD: "## Привычки\n\nДневник + чек-листы.", SortOrder: 2, ModuleID: &mod1},
		{Title: "Целевые показатели", Slug: "metrics", ContentMD: "## Метрики\n\nЧто и как измеряем.", SortOrder: 3, ModuleID: &mod1},
		{Title: "Дизайн программы", Slug: "design", ContentMD: "## Программа\n\n90 дней расписаны по неделям.", SortOrder: 4, ModuleID: &mod2},
		{Title: "Ритм недели", Slug: "rhythm", ContentMD: "## Ритм\n\nРабота, восстановление, тренировка.", SortOrder: 5, ModuleID: &mod2},
		{Title: "Поддержка тренера", Slug: "support", ContentMD: "## Поддержка\n\nКак мы работаем вместе.", SortOrder: 6, ModuleID: &mod2},
	}
	for _, l := range lessons {
		l.CourseID = courseID
		if _, err := repo.CreateLesson(ctx, l); err != nil {
			return err
		}
	}
	return nil
}
