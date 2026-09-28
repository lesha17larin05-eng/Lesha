package seed

import (
	"context"

	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/db"
)

// ensureZhonglirovanie – платный курс «Жонглирование: у вас получится».
//
// Видео уроков – mp4 на сервере: /root/data/videos/zhonglirovanie/<slug>.mp4
// (загружаются вручную, как у «Здоровой спины»). Пока файла нет, кабинет
// показывает на месте плеера заглушку «Видео скоро появится».
//
// ContentMD – короткое описание урока (тема и результат), как у других курсов.
func ensureZhonglirovanie(ctx context.Context, repo *db.Repo) (uuid.UUID, error) {
	price := 1490
	courseID, err := ensureCourse(ctx, repo, db.CourseInput{
		Slug:        "zhonglirovanie",
		Title:       "Жонглирование: у вас получится",
		Subtitle:    "Научитесь жонглировать тремя мячами с нуля – шаг за шагом",
		Description: "8 видеоуроков: от одного мяча к фонтану из трёх. В каждом упражнении понятный критерий, когда переходить дальше.",
		Kind:        "paid",
		PriceRub:    &price,
		IsPublished: true,
		SortOrder:   2,
	})
	if err != nil {
		return uuid.Nil, err
	}
	existing, _ := repo.ListLessons(ctx, courseID)
	if len(existing) > 0 {
		return courseID, nil
	}
	lessons := []db.LessonInput{
		{Title: "Знакомство с курсом", Slug: "znakomstvo", SortOrder: 1,
			ContentMD: "Как устроен курс и как по нему заниматься: последовательные упражнения, в каждом – понятный критерий перехода. Что подготовить к первому занятию. «Если вы ещё хотя бы чуть-чуть сомневаетесь, что у вас получится, – открывайте первое занятие и занимайтесь»."},
		{Title: "Фундамент – один мяч", Slug: "odin-myach", SortOrder: 2,
			ContentMD: "Точный бросок одного мяча – основа, на которой строится жонглирование тремя. Исходное положение, бросок через центр в другую руку и четыре частые ошибки. Переходите дальше, когда 8 бросков из 10 точные."},
		{Title: "Два мяча", Slug: "dva-myacha", SortOrder: 3,
			ContentMD: "Добавляем второй мяч: оба броска вверх, оба мяча ловим в боковых точках. Отрабатываем в обе стороны. Переходите дальше, когда 8 пар бросков из 10 точные – и с правой руки, и с левой."},
		{Title: "Два мяча в одной руке", Slug: "dva-v-odnoy-ruke", SortOrder: 4,
			ContentMD: "Учимся держать два мяча в одной руке и продолжаем точно бросать – подготовка к третьему мячу. В конце – короткое интервью с вожатой Катей, которая училась между делом."},
		{Title: "Три броска", Slug: "tri-broska", SortOrder: 5,
			ContentMD: "Выпускаем третий мяч: раз, два, три – поймали. «Ваша вершина уже не за горами». Переходите дальше, когда третий бросок уверенно выходит."},
		{Title: "Жонглируем без остановки", Slug: "bez-ostanovki", SortOrder: 6,
			ContentMD: "Четвёртый, пятый бросок и дальше – запускаем фонтан. Теперь подход заканчивается упавшим мячом, а не пойманным. Переходите дальше, когда получается 7 бросков подряд."},
		{Title: "Цель – 30 секунд", Slug: "cel-30-sekund", SortOrder: 7,
			ContentMD: "Тренируемся подходами с отдыхом и выходим на 30 секунд жонглирования. Заранее придумываете себе подарок за этот результат."},
		{Title: "Что дальше", Slug: "chto-dalshe", SortOrder: 8,
			ContentMD: "Жонглирование как медитация на 3–5 минут, следующий трюк и как научить жонглировать близкого человека."},
	}
	for _, l := range lessons {
		l.CourseID = courseID
		if _, err := repo.CreateLesson(ctx, l); err != nil {
			return uuid.Nil, err
		}
	}
	return courseID, nil
}
