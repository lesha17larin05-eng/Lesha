-- Перестройка курса «Здоровая спина» под финальную программу:
-- 12 уроков в 5 модулях (Старт / Неделя 1 / Неделя 2 / Неделя 3 / Бонус и итог).
-- Сидер при следующем старте пересоздаст уроки и модули с новыми slug-ами.
-- lesson_progress снимется каскадно через lessons.

DELETE FROM lessons
 WHERE course_id IN (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

DELETE FROM modules
 WHERE course_id IN (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE courses
   SET subtitle    = 'Курс на 3 недели — от мягкого старта к привычке',
       description = '12 уроков в 5 модулях, аудио-практика расслабления в подарок и год доступа в кабинете. Начинаете в любой день, идёте в своём темпе.',
       updated_at  = now()
 WHERE slug = 'zdorovaya-spina';
