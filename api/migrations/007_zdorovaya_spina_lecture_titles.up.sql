-- Обновляем названия и описания трёх лекций + финального урока в курсе
-- «Здоровая спина». Сидер пропускает существующие записи (idempotent),
-- поэтому применяем правки данных миграцией.
UPDATE lessons
   SET title = 'Грыжи и протрузии',
       content_md = 'Разбираю, как живёт спина с протрузией и грыжей, чего стоит избегать, а что наоборот помогает. На примерах учеников и моём собственном.'
 WHERE slug = 'gryzhi-protruzii'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET title = 'Осанка',
       content_md = 'Что в осанке держит её действительно прямой и почему «выпрямись» от мамы не работает. Простые ориентиры, которые встраиваются в обычный день — за столом, в машине, в очереди.'
 WHERE slug = 'osanka'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET title = 'Расслабление и стресс',
       content_md = 'Откуда в спине берётся напряжение, даже когда никаких тренировок не было. Дыхание и мягкие движения, после которых отпускает и тело, и голова.'
 WHERE slug = 'rasslablenie'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET title = 'Что дальше',
       content_md = 'Поздравление, неделя отдыха, дальше — циклы тренировок и периоды паузы. Простая схема, по которой я тренирую себя и клиентов уже годами.'
 WHERE slug = 'itog'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');
