-- Откат: возвращаем тексты из миграции 007.
UPDATE lessons SET content_md = 'Как пройти курс с пользой, не сорваться и не переусердствовать. Разбираем устройство недели, чем отличается основная тренировка от короткой и как выбрать интенсивность под свою ситуацию.'
 WHERE slug = 'vvodnoe' AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');
UPDATE lessons SET content_md = 'Разбираю, как живёт спина с протрузией и грыжей, чего стоит избегать, а что наоборот помогает. На примерах учеников и моём собственном.'
 WHERE slug = 'gryzhi-protruzii' AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');
UPDATE lessons SET content_md = 'Что в осанке держит её действительно прямой и почему «выпрямись» от мамы не работает. Простые ориентиры, которые встраиваются в обычный день — за столом, в машине, в очереди.'
 WHERE slug = 'osanka' AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');
UPDATE lessons SET content_md = 'Откуда в спине берётся напряжение, даже когда никаких тренировок не было. Дыхание и мягкие движения, после которых отпускает и тело, и голова.'
 WHERE slug = 'rasslablenie' AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');
UPDATE lessons SET content_md = 'Поздравление, неделя отдыха, дальше — циклы тренировок и периоды паузы. Простая схема, по которой я тренирую себя и клиентов уже годами.'
 WHERE slug = 'itog' AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');
