-- Откат: возвращаем прежние, более длинные ИИ-формулировки.
UPDATE lessons
   SET title = 'Грыжи и протрузии: чего бояться, а чего нет',
       content_md = 'Лекция о том, что большая часть болей в спине лечится движением, а не покоем. Как ориентироваться на ощущения и где проходит граница «полезного дискомфорта».'
 WHERE slug = 'gryzhi-protruzii'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET title = 'Осанка: про что она на самом деле',
       content_md = 'Осанка – не про положение лопаток, а про раскрытую грудную клетку и состояние, с которым вы заходите в день. Простые принципы, которые встраиваются в обычную жизнь.'
 WHERE slug = 'osanka'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET title = 'Расслабление: как выходить из стресса через тело',
       content_md = 'Почему стресс делает нас сутулыми и как это разворачивать. Дыхание и мягкие движения, после которых отпускает не только тело, но и голова.'
 WHERE slug = 'rasslablenie'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET title = 'Что дальше – путь после курса',
       content_md = 'Поздравление, неделя отдыха, как продолжать заниматься. Циклы тренировок и периоды паузы – простой принцип, по которому ритм держится без надрыва.'
 WHERE slug = 'itog'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');
