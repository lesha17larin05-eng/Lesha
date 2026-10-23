-- Тексты курсов под новый сайт (23.10.2026): подписи в каталоге и описание
-- урока «Здоровье таза» без историй про диагнозы учениц.
UPDATE courses SET subtitle = '8 коротких практик: суставная гимнастика, шея, спина, медитация'
  WHERE slug = 'myagkiy-start' AND subtitle = 'Бесплатный вводный курс суставной гимнастики';
UPDATE courses SET subtitle = '3 недели тренировок для сильной и подвижной спины'
  WHERE slug = 'zdorovaya-spina' AND subtitle = 'Курс на 3 недели – от мягкого старта к привычке';
UPDATE lessons SET content_md = 'Силовые упражнения для области таза – приседания, вращения ногами на четвереньках, мягкое растяжение сидя. Они разгоняют кровь и питание там, где сидячая жизнь почти не пускает.'
  WHERE slug = 'tazobedrennyye-sustavy'
    AND content_md LIKE '%функциональное бесплодие%'
    AND course_id = (SELECT id FROM courses WHERE slug = 'myagkiy-start');
