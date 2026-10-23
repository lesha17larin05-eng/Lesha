UPDATE courses SET subtitle = 'Бесплатный вводный курс суставной гимнастики'
  WHERE slug = 'myagkiy-start' AND subtitle = '8 коротких практик: суставная гимнастика, шея, спина, медитация';
UPDATE courses SET subtitle = 'Курс на 3 недели – от мягкого старта к привычке'
  WHERE slug = 'zdorovaya-spina' AND subtitle = '3 недели тренировок для сильной и подвижной спины';
UPDATE lessons SET content_md = 'Силовые упражнения для области таза – приседания, вращения ногами на четвереньках, мягкое растяжение сидя. Они разгоняют кровь и питание там, где сидячая жизнь почти не пускает. В уроке Алексей рассказывает истории своих учениц: одна забеременела после диагноза «функциональное бесплодие», другая восстановилась после четвёртых родов.'
  WHERE slug = 'tazobedrennyye-sustavy'
    AND course_id = (SELECT id FROM courses WHERE slug = 'myagkiy-start');
