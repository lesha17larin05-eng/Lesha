-- Описания уроков переписаны из транскрипций Алексея (прямые цитаты
-- или близкие к оригинальной речи фразы), чтобы избежать сочинённого
-- «маркетингового» языка.

UPDATE lessons
   SET content_md = 'Как пройти курс с пользой и удовольствием, чтобы решить свой запрос, с которым вы пришли. Алексей разбирает, что важно сделать параллельно тренировкам и как выбрать свою интенсивность.'
 WHERE slug = 'vvodnoe'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET content_md = 'Знакомимся с базой: мостики, кошка-корова, дыхание. Алексей подробно разбирает каждое упражнение — чтобы вы прочувствовали технику и не торопились.'
 WHERE slug = 'nedelya-1-osnovnaya'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET content_md = 'Та же база, но без подробных объяснений — делаете за Алексеем. Для будних дней. Минимум 2 раза в неделю, можно больше.'
 WHERE slug = 'nedelya-1-korotkaya'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET content_md = '«По научным исследованиям, боль в спине, которая вызвана грыжами, это около 5%. Чаще всего спина болит из-за мышечных спазмов, из-за малоподвижного образа жизни». Алексей разбирает, что такое грыжи и протрузии — и чего реально стоит бояться.'
 WHERE slug = 'gryzhi-protruzii'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET content_md = 'Усложняем базу: боковые мостики, длиннее удержания. Тело уже привыкло к ритму — идём чуть глубже.'
 WHERE slug = 'nedelya-2-osnovnaya'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET content_md = 'Динамичная версия второй недели. Подключайте утром или в перерыв рабочего дня.'
 WHERE slug = 'nedelya-2-korotkaya'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET content_md = '«Болит спина или не болит спина — это только показатель: есть у человека физическая культура в жизни или нет». Алексей говорит, что осанка — это не про лопатки назад, а про лёгкое усилие на раскрытие грудной клетки и про то, как мы чувствуем себя в моменте.'
 WHERE slug = 'osanka'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET content_md = 'Финальная основная тренировка — закрепляем технику. Проверяем диапазон, разбираем точки роста.'
 WHERE slug = 'nedelya-3-osnovnaya'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET content_md = 'Финальная короткая. Этот набор движений остаётся с вами и после курса.'
 WHERE slug = 'nedelya-3-korotkaya'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET content_md = '«Реакция стресса развивалась эволюционно. Когда у наших предков возникала опасность, плечи уходят, закрывают шею, и люди вот так собираются». Алексей разбирает, как тело защищается стрессом и как из этого состояния выходить.'
 WHERE slug = 'rasslablenie'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET content_md = '30-минутная йога-нидра. Простая практика на внимание к разным частям тела. Алексей: «Возможно, вы уснёте, такое частенько случается — нормально». Слушайте в кровати, в дороге, в обед.'
 WHERE slug = 'praktika-rasslableniya'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');

UPDATE lessons
   SET content_md = '«Три недели человек занимается, четвёртую неделю отдых. И так три цикла, потом месяц отдых. Это используют и профессиональные спортсмены, и обычные люди». Поздравление и схема, как продолжать после курса.'
 WHERE slug = 'itog'
   AND course_id = (SELECT id FROM courses WHERE slug = 'zdorovaya-spina');
