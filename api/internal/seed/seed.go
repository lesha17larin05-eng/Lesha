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
		Description: "Восемь практик для тех, кто давно хотел начать – в своём темпе, без изнурения и сложных программ.",
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
			ContentMD:   "Алексей рассказывает, как пройти курс. Можно по порядку, а можно сразу к тому, что важно – шея, спина или суставная гимнастика. Расписания нет: вы выбираете темп и порядок сами.",
			DurationSec: 120, SortOrder: 1, IsPreview: true,
		},
		{
			Title: "Суставная гимнастика", Slug: "sustavnaya-gimnastika",
			ContentMD:   "30 минут на всё тело – от шеи и плеч до тазобедренных и стоп. Движения мягкие, в комфортной для вас амплитуде, без боли и рывков. Хорошо заходит утром как зарядка или вечером после рабочего дня – тело становится подвижнее уже после первого раза.",
			DurationSec: 1740, SortOrder: 2, IsPreview: true,
		},
		{
			Title: "Шея после рабочего дня", Slug: "sheya",
			ContentMD:   "Для тех, кто много сидит за компьютером. 7 простых упражнений – наклоны, повороты, втягивание подбородка, работа с лопатками. После такой практики ощущение, будто сходили на сеанс массажа. Подойдёт и тем, у кого шея уже болит.",
			DurationSec: 720, SortOrder: 3,
		},
		{
			Title: "Тренировка для спины (из платного курса)", Slug: "zdorovaya-spina",
			ContentMD:   "Полноценная тренировка из платного курса «Здоровая спина» – 35 минут работы. Внутри: вращения, скрутки, приседания, боковая планка, упражнения на пресс и спину, в конце поза ребёнка. После практики понятно, подходит ли вам формат большого курса.",
			DurationSec: 2160, SortOrder: 4,
		},
		{
			Title: "Стопы", Slug: "stopy",
			ContentMD:   "Про стопы обычно забывают, а от них зависит и осанка, и колени, и тазобедренные суставы. В уроке – упражнения на управление пальцами ног и подъёмы на носки для тонуса икр. Заодно Алексей объясняет, как мышцы голеней помогают сердцу гнать кровь вверх – и зачем подниматься на носки между делом, пока готовится еда.",
			DurationSec: 840, SortOrder: 5,
		},
		{
			Title: "Здоровье таза", Slug: "tazobedrennyye-sustavy",
			ContentMD:   "Силовые упражнения для области таза – приседания, вращения ногами на четвереньках, мягкое растяжение сидя. Они разгоняют кровь и питание там, где сидячая жизнь почти не пускает. В уроке Алексей рассказывает истории своих учениц: одна забеременела после диагноза «функциональное бесплодие», другая восстановилась после четвёртых родов.",
			DurationSec: 1020, SortOrder: 6,
		},
		{
			Title: "Медитация на каждый день", Slug: "meditatsiya",
			ContentMD:   "Простая практика на внимание к дыханию, телу и ощущениям настоящего момента. Делать можно в любом положении: лёжа, сидя, стоя или даже в общественном транспорте. 8 минут, после которых обычно становится спокойнее.",
			DurationSec: 480, SortOrder: 7,
		},
		{
			Title: "Как тренироваться, чтобы не бросить", Slug: "itogovyy",
			ContentMD:   "Главная причина, почему люди бросают тренировки – нет ритма. Алексей делится схемой, по которой тренируется сам и ведёт клиентов: 3 недели работы плюс 1 неделя отдыха, и так три месяца, потом месяц паузы. Простой принцип, который сильно облегчает регулярность.",
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
		Description: "12 уроков в 5 модулях, аудио-практика расслабления в подарок и год доступа в кабинете. Начинаете в любой день, идёте в своём темпе.",
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

	modStart, err := repo.CreateModule(ctx, courseID, "Модуль 1. Старт", 1)
	if err != nil {
		return uuid.Nil, err
	}
	modW1, err := repo.CreateModule(ctx, courseID, "Модуль 2. Неделя 1 – знакомимся с базой", 2)
	if err != nil {
		return uuid.Nil, err
	}
	modW2, err := repo.CreateModule(ctx, courseID, "Модуль 3. Неделя 2 – углубление", 3)
	if err != nil {
		return uuid.Nil, err
	}
	modW3, err := repo.CreateModule(ctx, courseID, "Модуль 4. Неделя 3 – закрепление", 4)
	if err != nil {
		return uuid.Nil, err
	}
	modFinal, err := repo.CreateModule(ctx, courseID, "Модуль 5. Бонус и итог", 5)
	if err != nil {
		return uuid.Nil, err
	}

	lessons := []db.LessonInput{
		// Модуль 1 – Старт
		{Title: "С чего начнём", Slug: "vvodnoe",
			ContentMD:   "Как пройти курс с пользой и удовольствием, чтобы решить свой запрос, с которым вы пришли. Алексей разбирает, что важно сделать параллельно тренировкам и как выбрать свою интенсивность.",
			DurationSec: 840, SortOrder: 1, IsPreview: true, ModuleID: &modStart},

		// Модуль 2 – Неделя 1
		{Title: "Неделя 1 · Основная тренировка", Slug: "nedelya-1-osnovnaya",
			ContentMD:   "Знакомимся с базой: мостики, кошка-корова, дыхание. Алексей подробно разбирает каждое упражнение – чтобы вы прочувствовали технику и не торопились.",
			DurationSec: 2700, SortOrder: 2, ModuleID: &modW1},
		{Title: "Неделя 1 · Короткая тренировка", Slug: "nedelya-1-korotkaya",
			ContentMD:   "Та же база, но без подробных объяснений – делаете за Алексеем. Для будних дней. Минимум 2 раза в неделю, можно больше.",
			DurationSec: 1680, SortOrder: 3, ModuleID: &modW1},
		{Title: "Грыжи и протрузии", Slug: "gryzhi-protruzii",
			ContentMD:   "«По научным исследованиям, боль в спине, которая вызвана грыжами, это около 5%. Чаще всего спина болит из-за мышечных спазмов, из-за малоподвижного образа жизни». Алексей разбирает, что такое грыжи и протрузии – и чего реально стоит бояться.",
			DurationSec: 1140, SortOrder: 4, ModuleID: &modW1},

		// Модуль 3 – Неделя 2
		{Title: "Неделя 2 · Основная тренировка", Slug: "nedelya-2-osnovnaya",
			ContentMD:   "Усложняем базу: боковые мостики, длиннее удержания. Тело уже привыкло к ритму – идём чуть глубже.",
			DurationSec: 2880, SortOrder: 5, ModuleID: &modW2},
		{Title: "Неделя 2 · Короткая тренировка", Slug: "nedelya-2-korotkaya",
			ContentMD:   "Динамичная версия второй недели. Подключайте утром или в перерыв рабочего дня.",
			DurationSec: 2040, SortOrder: 6, ModuleID: &modW2},
		{Title: "Осанка", Slug: "osanka",
			ContentMD:   "Лекция об осанке без команды «сведи лопатки». Алексей показывает, что держит спину на самом деле: лёгкое усилие на раскрытие грудной клетки и внимание к тому, как вы чувствуете себя в моменте.",
			DurationSec: 540, SortOrder: 7, ModuleID: &modW2},

		// Модуль 4 – Неделя 3
		{Title: "Неделя 3 · Основная тренировка", Slug: "nedelya-3-osnovnaya",
			ContentMD:   "Финальная основная тренировка – закрепляем технику. Проверяем диапазон, разбираем точки роста.",
			DurationSec: 2880, SortOrder: 8, ModuleID: &modW3},
		{Title: "Неделя 3 · Короткая тренировка", Slug: "nedelya-3-korotkaya",
			ContentMD:   "Финальная короткая. Этот набор движений остаётся с вами и после курса.",
			DurationSec: 1800, SortOrder: 9, ModuleID: &modW3},
		{Title: "Расслабление и стресс", Slug: "rasslablenie",
			ContentMD:   "«Реакция стресса развивалась эволюционно. Когда у наших предков возникала опасность, плечи уходят, закрывают шею, и люди вот так собираются». Алексей разбирает, как тело защищается стрессом и как из этого состояния выходить.",
			DurationSec: 1860, SortOrder: 10, ModuleID: &modW3},

		// Модуль 5 – Бонус и итог
		{Title: "Бонус-аудио: практика расслабления", Slug: "praktika-rasslableniya",
			ContentMD:   "30-минутная йога-нидра – практика внимания к разным частям тела. Алексей: «Возможно, вы уснёте, такое частенько случается – нормально». Слушайте лёжа в кровати, в транспорте пассажиром или в обеденный перерыв. За рулём не включайте – практика по-настоящему расслабляет.",
			DurationSec: 1800, SortOrder: 11, ModuleID: &modFinal},
		{Title: "Что дальше", Slug: "itog",
			ContentMD:   "«Три недели человек занимается, четвёртую неделю отдых. И так три цикла, потом месяц отдых. Это используют и профессиональные спортсмены, и обычные люди». Поздравление и схема, как продолжать после курса.",
			DurationSec: 240, SortOrder: 12, ModuleID: &modFinal},
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
// курсами intro-zdorovye и transformaciya-90 – больше не нужны.)
