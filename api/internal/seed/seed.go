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
	myagkiyID, err := ensureMyagkiyStart(ctx, repo)
	if err != nil {
		return err
	}
	spinaID, err := ensureZdorovayaSpina(ctx, repo)
	if err != nil {
		return err
	}
	// auto-enroll admin и тестового user
	_ = repo.Grant(ctx, adminID, myagkiyID, "admin", &adminID)
	_ = repo.Grant(ctx, adminID, spinaID, "admin", &adminID)
	_ = repo.Grant(ctx, userID, myagkiyID, "free", nil)
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

func ensureMyagkiyStart(ctx context.Context, repo *db.Repo) (uuid.UUID, error) {
	courseID, err := ensureCourse(ctx, repo, db.CourseInput{
		Slug:        "myagkiy-start",
		Title:       "Мягкий старт",
		Subtitle:    "Бесплатный вводный курс суставной гимнастики",
		Description: "Восемь практик для тех, кто давно хотел начать — в своём темпе, без изнурения и сложных программ.",
		Kind:        "free",
		IsPublished: true,
		SortOrder:   0,
	})
	if err != nil {
		return uuid.Nil, err
	}
	existing, _ := repo.ListLessons(ctx, courseID)
	if len(existing) > 0 {
		return courseID, nil
	}
	lessons := []db.LessonInput{
		{
			Title: "С чего начнём", Slug: "vvodnyy",
			ContentMD:   "Алексей рассказывает, как пройти курс. Можно по порядку, а можно сразу к тому, что важно — шея, спина или суставная гимнастика. Расписания нет: вы выбираете темп и порядок сами.",
			DurationSec: 120, SortOrder: 1, IsPreview: true,
		},
		{
			Title: "Суставная гимнастика", Slug: "sustavnaya-gimnastika",
			ContentMD:   "30 минут на всё тело — от шеи и плеч до тазобедренных и стоп. Движения мягкие, в комфортной для вас амплитуде, без боли и рывков. Хорошо заходит утром как зарядка или вечером после рабочего дня — тело становится подвижнее уже после первого раза.",
			DurationSec: 1740, SortOrder: 2, IsPreview: true,
		},
		{
			Title: "Шея после рабочего дня", Slug: "sheya",
			ContentMD:   "Для тех, кто много сидит за компьютером. 7 простых упражнений — наклоны, повороты, втягивание подбородка, работа с лопатками. После такой практики ощущение, будто сходили на сеанс массажа. Подойдёт и тем, у кого шея уже болит.",
			DurationSec: 720, SortOrder: 3,
		},
		{
			Title: "Тренировка для спины (из платного курса)", Slug: "zdorovaya-spina",
			ContentMD:   "Полноценная тренировка из платного курса «Здоровая спина» — 35 минут работы. Внутри: вращения, скрутки, приседания, боковая планка, упражнения на пресс и спину, в конце поза ребёнка. После практики понятно, подходит ли вам формат большого курса.",
			DurationSec: 2160, SortOrder: 4,
		},
		{
			Title: "Стопы", Slug: "stopy",
			ContentMD:   "Про стопы обычно забывают, а от них зависит и осанка, и колени, и тазобедренные суставы. В уроке — упражнения на управление пальцами ног и подъёмы на носки для тонуса икр. Заодно Алексей объясняет, как мышцы голеней помогают сердцу гнать кровь вверх — и зачем подниматься на носки между делом, пока готовится еда.",
			DurationSec: 840, SortOrder: 5,
		},
		{
			Title: "Здоровье таза", Slug: "tazobedrennyye-sustavy",
			ContentMD:   "Силовые упражнения для области таза — приседания, вращения ногами на четвереньках, мягкое растяжение сидя. Они разгоняют кровь и питание там, где сидячая жизнь почти не пускает. В уроке Алексей рассказывает истории своих учениц: одна забеременела после диагноза «функциональное бесплодие», другая восстановилась после четвёртых родов.",
			DurationSec: 1020, SortOrder: 6,
		},
		{
			Title: "Медитация на каждый день", Slug: "meditatsiya",
			ContentMD:   "Простая практика на внимание к дыханию, телу и ощущениям настоящего момента. Делать можно в любом положении: лёжа, сидя, стоя или даже в общественном транспорте. 8 минут, после которых обычно становится спокойнее.",
			DurationSec: 480, SortOrder: 7,
		},
		{
			Title: "Как тренироваться, чтобы не бросить", Slug: "itogovyy",
			ContentMD:   "Главная причина, почему люди бросают тренировки — нет ритма. Алексей делится схемой, по которой тренируется сам и ведёт клиентов: 3 недели работы плюс 1 неделя отдыха, и так три месяца, потом месяц паузы. Простой принцип, который сильно облегчает регулярность.",
			DurationSec: 180, SortOrder: 8,
		},
	}
	for _, l := range lessons {
		l.CourseID = courseID
		if _, err := repo.CreateLesson(ctx, l); err != nil {
			return uuid.Nil, err
		}
	}
	return courseID, nil
}

func ensureZdorovayaSpina(ctx context.Context, repo *db.Repo) (uuid.UUID, error) {
	price := 3990
	courseID, err := ensureCourse(ctx, repo, db.CourseInput{
		Slug:        "zdorovaya-spina",
		Title:       "Здоровая спина",
		Subtitle:    "Курс на 3 недели – от мягкого старта к привычке",
		Description: "11 уроков, 3 недели, ваш темп. Основные тренировки, короткие практики и теория – чтобы выстроить регулярность, которая держится после курса. Начинаете в любой день, доступ навсегда.",
		Kind:        "paid",
		PriceRub:    &price,
		IsPublished: true,
		SortOrder:   1,
	})
	if err != nil {
		return uuid.Nil, err
	}
	existing, _ := repo.ListLessons(ctx, courseID)
	if len(existing) > 0 {
		return courseID, nil
	}

	modIntro, err := repo.CreateModule(ctx, courseID, "Модуль 1. Вводный", 1)
	if err != nil {
		return uuid.Nil, err
	}
	modW1, err := repo.CreateModule(ctx, courseID, "Неделя 1. Запуск", 2)
	if err != nil {
		return uuid.Nil, err
	}
	modW2, err := repo.CreateModule(ctx, courseID, "Неделя 2. Углубление", 3)
	if err != nil {
		return uuid.Nil, err
	}
	modW3, err := repo.CreateModule(ctx, courseID, "Неделя 3. Закрепление", 4)
	if err != nil {
		return uuid.Nil, err
	}
	modFinal, err := repo.CreateModule(ctx, courseID, "Модуль 5. Дальше", 5)
	if err != nil {
		return uuid.Nil, err
	}

	lessons := []db.LessonInput{
		// Модуль 1 – вводный (превью)
		{Title: "Как проходить курс", Slug: "kak-prohodit-kurs",
			ContentMD: "Знакомство с курсом, разметка под ваш уровень и расписание. Алексей рассказывает, как устроены недели, чем основная тренировка отличается от короткой практики и как выбрать интенсивность под свою ситуацию.",
			SortOrder: 1, IsPreview: true, ModuleID: &modIntro},

		// Неделя 1 – Запуск
		{Title: "Неделя 1. Основная тренировка", Slug: "nedelya-1-osnovnaya",
			ContentMD: "Главная тренировка первой недели – ~45 минут. Возвращаем подвижность, мягко прорабатываем спину, убираем страх движения. Подробно разбираю детали и нюансы каждого упражнения.",
			SortOrder: 2, ModuleID: &modW1},
		{Title: "Неделя 1. Короткая практика", Slug: "nedelya-1-korotkaya",
			ContentMD: "Короткая практика 20–25 минут. Последовательно выполняете упражнения за мной. Подходит для тех дней, когда нет времени на полную тренировку – от 2 до 8 раз в неделю.",
			SortOrder: 3, ModuleID: &modW1},
		{Title: "Теория: грыжи и протрузии", Slug: "teoriya-gryzhi",
			ContentMD: "Что такое грыжи и протрузии, почему движение – не враг, а инструмент восстановления. Как ориентироваться на ощущения и где проходит граница «полезного дискомфорта».",
			SortOrder: 4, ModuleID: &modW1},

		// Неделя 2 – Углубление
		{Title: "Неделя 2. Основная тренировка", Slug: "nedelya-2-osnovnaya",
			ContentMD: "Главная тренировка второй недели – добавляем силу, работаем с осанкой. Тело уже привыкло к ритму, можно идти чуть глубже.",
			SortOrder: 5, ModuleID: &modW2},
		{Title: "Неделя 2. Короткая практика", Slug: "nedelya-2-korotkaya",
			ContentMD: "Короткая практика второй недели. Можно подключать в перерывы рабочего дня или утром – небольшая зарядка, которая держит спину в тонусе.",
			SortOrder: 6, ModuleID: &modW2},
		{Title: "Теория: осанка", Slug: "teoriya-osanka",
			ContentMD: "Что такое здоровая осанка, чем нам мешает «правильно сидеть» и что реально работает. Простые принципы, которые встраиваются в обычный день.",
			SortOrder: 7, ModuleID: &modW2},

		// Неделя 3 – Закрепление
		{Title: "Неделя 3. Основная тренировка", Slug: "nedelya-3-osnovnaya",
			ContentMD: "Финальная основная тренировка – закрепляем привычку, проверяем диапазон движения, разбираем точки роста.",
			SortOrder: 8, ModuleID: &modW3},
		{Title: "Неделя 3. Короткая практика", Slug: "nedelya-3-korotkaya",
			ContentMD: "Короткая практика третьей недели. Финальный «набор» движений, который остаётся с вами и после курса.",
			SortOrder: 9, ModuleID: &modW3},
		{Title: "Бонус: расслабление и снятие стресса", Slug: "bonus-rasslablenie",
			ContentMD: "Бонусный урок – практика для снятия стресса и расслабления. Дыхательные техники и мягкие движения, которые возвращают ощущение целостности.",
			SortOrder: 10, ModuleID: &modW3},

		// Модуль 5 – Дальше
		{Title: "Циклы и следующий шаг", Slug: "cikly-i-shag",
			ContentMD: "Как продолжать после курса. Циклы тренировок и периоды отдыха, разметка ритма на месяцы вперёд. Что делать, если выпали из режима – и как вернуться.",
			SortOrder: 11, ModuleID: &modFinal},
	}
	for _, l := range lessons {
		l.CourseID = courseID
		if _, err := repo.CreateLesson(ctx, l); err != nil {
			return uuid.Nil, err
		}
	}
	return courseID, nil
}

// (Функции ensureFreeLessons / ensurePaidLessons удалены вместе с
// курсами intro-zdorovye и transformaciya-90 — больше не нужны.)
