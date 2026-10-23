#!/usr/bin/env python3
"""Сборка PDF-материалов курса «Здоровая спина»: методичка, календарь, замеры.

Исходник текста и оформления – этот файл. Шрифты – из web/public/fonts.
Запуск (нужен WeasyPrint: pip install weasyprint brotli):

    python3 api/internal/assets/courses/zdorovaya-spina/src/build.py

Готовые PDF ложатся рядом, в api/internal/assets/courses/zdorovaya-spina/,
и попадают в бинарник API через go:embed (internal/assets/files.go).
"""
import pathlib
import re

from weasyprint import HTML

HERE = pathlib.Path(__file__).resolve().parent
OUT = HERE.parent
REPO = HERE.parents[5]
FONTS = REPO / "web" / "public" / "fonts"

# Шрифты сайта разбиты на поднаборы (latin, cyrillic, …) с unicode-range,
# который WeasyPrint не учитывает. Поэтому каждый поднабор – отдельное
# семейство, а нужный глиф подбирается по цепочке font-family.
SUBSETS = ["latin", "cyrillic", "latin-ext", "cyrillic-ext"]
faces = []
for f in sorted(FONTS.glob("*.woff2")):
    m = re.match(r"(Manrope|CormorantGaramond)-(\d+)(i?)-(.+)\.woff2", f.name)
    if not m:
        continue
    fam, weight, ital, sub = m.groups()
    faces.append(f"@font-face {{ font-family: '{fam}-{sub}'; font-weight: {weight}; "
                 f"font-style: {'italic' if ital else 'normal'}; src: url(file://{f}); }}")
fonts_css = "\n".join(faces)
SANS = ", ".join(f"'Manrope-{s}'" for s in SUBSETS) + ", sans-serif"
SERIF = ", ".join(f"'CormorantGaramond-{s}'" for s in SUBSETS) + ", serif"

CSS = fonts_css + """
@page { size: A4; margin: 0; }
* { box-sizing: border-box; }
html { font-family: SANS_STACK; color: #1a2744; font-size: 9.6pt; line-height: 1.6; }
body { margin: 0; }
.page { width: 210mm; height: 297mm; position: relative; page-break-after: always; background: #fdfaf5; overflow: hidden; }
.page:last-child { page-break-after: auto; }
.inner { position: absolute; left: 18mm; right: 18mm; top: 22mm; bottom: 22mm; }
.top { position: absolute; left: 18mm; right: 18mm; top: 9mm; display: flex; justify-content: space-between; font-size: 7pt; letter-spacing: .04em; text-transform: uppercase; color: #1a2744; }
.top .who::before { content: ''; display: inline-block; width: 9mm; height: 0; border-top: .6pt solid #1a2744; vertical-align: middle; margin-right: 3mm; }
.foot { position: absolute; left: 18mm; right: 18mm; bottom: 9mm; display: flex; justify-content: space-between; font-size: 7.5pt; color: #444; }
.label { font-size: 7pt; font-weight: 700; letter-spacing: .14em; text-transform: uppercase; color: #e8652a; margin: 0 0 3mm; }
h1 { font-family: SERIF_STACK; font-weight: 500; font-size: 27pt; line-height: 1.15; margin: 0 0 7mm; color: #1a2744; }
h1 em { font-style: italic; }
h3 { font-size: 9.6pt; font-weight: 700; margin: 6mm 0 2.5mm; }
p { margin: 0 0 3.2mm; color: #2c2c2c; }
.muted { color: #666; }
ul.dots { margin: 0 0 4mm; padding: 0; list-style: none; }
ul.dots li { position: relative; padding-left: 5mm; margin-bottom: 1.6mm; color: #2c2c2c; }
ul.dots li::before { content: ''; position: absolute; left: 0; top: 2.1mm; width: 1.4mm; height: 1.4mm; border-radius: 50%; background: #e8652a; }
ol.num { margin: 0; padding-left: 5mm; color: #2c2c2c; }
ol.num li { margin-bottom: 1.4mm; }

/* обложка */
.cover { display: flex; flex-direction: column; }
.cover-top { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; text-align: center; padding: 0 20mm; }
.dot { width: 16mm; height: 16mm; border-radius: 50%; background: #e8652a; margin-bottom: 9mm; }
.kicker { font-size: 7.5pt; font-weight: 700; letter-spacing: .3em; color: #e8652a; margin-bottom: 8mm; }
.cover h1 { font-size: 50pt; line-height: 1.02; margin: 0 0 9mm; }
.cover .sub { color: #999; font-size: 9pt; }
.band { height: 40mm; background: #1a2744; color: white; display: flex; flex-direction: column; align-items: center; justify-content: center; }
.band b { font-size: 8pt; letter-spacing: .2em; }
.band span { font-size: 7.5pt; color: #f4b48f; margin-top: 2.5mm; }

/* последняя страница */
.back { background: #1a2744; color: white; }
.back .dot { position: absolute; right: 22mm; top: 22mm; width: 9mm; height: 9mm; }
.back .bx { position: absolute; left: 18mm; right: 18mm; bottom: 46mm; }
.back h1 { color: white; font-size: 26pt; }
.back p { color: rgba(255,255,255,.85); }
.back .contacts { position: absolute; left: 18mm; bottom: 16mm; font-size: 7.5pt; color: #f4b48f; line-height: 1.7; }

/* таблицы-строки */
table.rows { width: 100%; border-collapse: collapse; margin: 0 0 6mm; }
table.rows td { padding: 2.4mm 0; border-bottom: .5pt solid #e6e0d6; vertical-align: top; }
table.rows td.k { width: 32mm; font-size: 6.8pt; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; color: #e8652a; padding-top: 3mm; }
table.rows tr.pair-end td { border-bottom: .5pt solid #e6e0d6; }
table.rows tr.pair-start td { border-bottom: none; }

/* модули */
.mod { border-left: 1.4mm solid #1a2744; padding: .6mm 0 .6mm 4mm; margin-bottom: 4mm; }
.mod.bonus { border-left-color: #2e9e63; }
.mod small { display: block; font-size: 6.5pt; letter-spacing: .14em; text-transform: uppercase; color: #888; }
.mod b { display: block; font-family: SERIF_STACK; font-weight: 500; font-size: 13.5pt; margin: .6mm 0 .4mm; }
.mod span { font-size: 7.6pt; color: #666; }

.note { background: #efe9dd; border-radius: 3mm; padding: 4.5mm 5mm; margin-top: 6mm; }
.note p { margin: 0; }
.note b { color: #1a2744; }

/* полезно / тревожно */
.cols { display: flex; gap: 4mm; margin: 2mm 0 6mm; }
.box { flex: 1; border-radius: 3mm; padding: 4mm 4.5mm; }
.box.ok { background: #eaf5ee; border: .6pt solid #9fd3b2; }
.box.bad { background: #fbeaea; border: .6pt solid #efb1b1; }
.box small { display: block; font-size: 6.5pt; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; }
.box.ok small { color: #2e9e63; } .box.bad small { color: #c9573a; }
.box b { display: block; margin: 1mm 0 2mm; }
.box ul { margin: 0; padding-left: 4mm; font-size: 8.4pt; color: #2c2c2c; }

/* советы */
.tip { margin-bottom: 4.2mm; padding-left: 5mm; position: relative; }
.tip::before { content: ''; position: absolute; left: 0; top: 1.8mm; width: 1.4mm; height: 1.4mm; border-radius: 50%; background: #e8652a; }
.tip b { display: block; margin-bottom: .8mm; }
.tip p { margin: 0; }

/* календарь */
table.cal { width: 100%; border-collapse: collapse; margin: 6mm 0 6mm; }
table.cal th { font-size: 6.5pt; letter-spacing: .14em; color: #888; font-weight: 600; padding-bottom: 2mm; }
table.cal td { text-align: center; padding: 3mm 1mm; background: #f3eee4; }
table.cal td.wk { background: #1a2744; color: white; text-align: left; padding-left: 4mm; width: 30mm; }
table.cal td.wk b { display: block; font-size: 7pt; letter-spacing: .12em; }
table.cal td.wk span { font-size: 7pt; color: #f4b48f; }
table.cal tr + tr td { border-top: 1.2mm solid #fdfaf5; }
.cell small { display: block; font-size: 6.2pt; color: #999; margin-bottom: 1.6mm; }
.cell i { display: inline-block; width: 7mm; height: 7mm; border: .7pt solid #c9c2b5; border-radius: 1.6mm; background: white; }
.cell.main small { color: #e8652a; font-weight: 700; }
.cell.main i { border: 1pt solid #e8652a; background: #fdf0e8; }
.cell.rest small { color: #bbb; }
.legend { font-size: 8pt; }
.legend div { margin-bottom: 1.6mm; }
.legend b { display: inline-block; width: 24mm; font-size: 6.8pt; letter-spacing: .12em; }
.legend .o { color: #e8652a; } .legend .n { color: #1a2744; } .legend .g { color: #999; }

/* замеры */
table.tests { width: 100%; border-collapse: collapse; margin: 4mm 0 4mm; border: .8pt solid #1a2744; }
table.tests th { background: #1a2744; color: white; font-size: 7pt; letter-spacing: .1em; text-align: left; padding: 2.4mm 3mm; }
table.tests td { padding: 3mm; border-top: .5pt solid #e6e0d6; vertical-align: top; }
table.tests td:nth-child(2), table.tests td:nth-child(3) { width: 30mm; border-left: .5pt solid #e6e0d6; }
table.tests td b { display: block; font-size: 8.6pt; }
table.tests td span { font-size: 7.4pt; color: #777; }
.diary { border: .6pt solid #e6e0d6; background: white; border-radius: 3mm; padding: 4mm 5mm 3mm; margin-bottom: 5mm; height: 52mm; }
.diary small { font-size: 6.8pt; font-weight: 700; letter-spacing: .12em; color: #e8652a; }
.diary .ln { border-bottom: .5pt solid #e6e0d6; height: 8.5mm; }
blockquote { margin: 7mm 0 0; padding: 5mm 6mm; background: #efe9dd; border-left: 1mm solid #e8652a; border-radius: 0 3mm 3mm 0; font-family: SERIF_STACK; font-style: italic; font-size: 13pt; line-height: 1.4; color: #1a2744; }
.sig { font-family: SERIF_STACK; font-style: italic; font-size: 12pt; margin-top: 6mm; }
blockquote cite { display: block; margin-top: 2.5mm; font-family: SANS_STACK; font-style: normal; font-size: 7.5pt; color: #777; }
"""

CSS = CSS.replace("SANS_STACK", SANS).replace("SERIF_STACK", SERIF)

BRAND = "leshalarin.ru · @lesha_larin"


def cover(title_html, sub):
    return f"""<section class="page cover">
  <div class="cover-top">
    <div class="dot"></div>
    <div class="kicker">КУРС</div>
    <h1>{title_html}</h1>
    <div class="sub">{sub}</div>
  </div>
  <div class="band"><b>АЛЕКСЕЙ ЛАРИН</b><span>{BRAND}</span></div>
</section>"""


def page(section, num, body):
    return f"""<section class="page">
  <div class="top"><span class="who">Алексей Ларин</span><span>Здоровая спина · курс</span></div>
  <div class="inner">{body}</div>
  <div class="foot"><span>{section}</span><span>стр. {num}</span></div>
</section>"""


BACK = """<section class="page back">
  <div class="dot"></div>
  <div class="bx">
    <h1>Спасибо, что выбрали<br><em>этот курс</em></h1>
    <p>Если возникнут вопросы – пишите в Telegram.<br>Я отвечаю всем выпускникам, в любом тарифе.</p>
    <p>Желаю вам лёгкости, регулярности и хороших отношений со своей спиной.</p>
    <p class="sig">– Алексей</p>
  </div>
  <div class="contacts">leshalarin.ru<br>Telegram: @larin_lesha · @lesha_larin</div>
</section>"""


def build(name, pages):
    html = f"<!doctype html><html lang='ru'><head><meta charset='utf-8'><style>{CSS}</style></head><body>{''.join(pages)}</body></html>"
    HTML(string=html, base_url=str(HERE)).write_pdf(OUT / f"{name}.pdf")
    print("ok", OUT / f"{name}.pdf")


# ─── Методичка ───────────────────────────────────────────────────────────
metodichka = [
    cover("Методичка<br><em>курса</em>", "Здоровая спина · 3 недели"),
    page("Вступление", 1, """
<p class="label">Вступление</p>
<h1>Если вы это читаете –<br><em>значит уже начали</em></h1>
<p>Меня зовут Алексей. Этот курс я собрал из своего опыта восстановления после перелома трёх позвонков в 9 лет – и из больше чем 10 лет работы тренером с людьми, которые хотели нормальные отношения со своей спиной.</p>
<p>Эта методичка – ваш спутник на 3 недели. В ней то, что я повторяю всем своим клиентам и что хочу, чтобы у вас было под рукой: как выбрать свою интенсивность, когда стоит отступить, как не сорваться через 10 дней и что делать дальше.</p>
<p>Прочитайте её перед стартом. Возвращайтесь по ходу – особенно если будут сомнения. И помните: главное – продолжать двигаться после курса, даже если пройдёте его неидеально.</p>
<p class="muted">– Алексей</p>
"""),
    page("Структура курса", 2, """
<p class="label">Структура</p>
<h1>Как устроен курс</h1>
<p>Курс – это 3 недели тренировок плюс вводный, бонус-аудио и финальный уроки. Всего 12 уроков в 5 модулях.</p>
<div class="mod"><small>Модуль 1 · Старт</small><b>С чего начнём</b><span>Как пройти курс с пользой и не сорваться</span></div>
<div class="mod"><small>Модуль 2 · Неделя 1</small><b>Знакомимся с базой</b><span>Основная · Короткая · Лекция: грыжи и протрузии</span></div>
<div class="mod"><small>Модуль 3 · Неделя 2</small><b>Углубление</b><span>Основная · Короткая · Лекция: осанка</span></div>
<div class="mod"><small>Модуль 4 · Неделя 3</small><b>Закрепление</b><span>Основная · Короткая · Лекция: расслабление и стресс</span></div>
<div class="mod bonus"><small>Модуль 5 · Бонус и итог</small><b>Дальше</b><span>Бонус-аудио: практика расслабления (30 мин) · Что дальше</span></div>
<h3>Ритм одной недели</h3>
<ul class="dots">
<li>Основная тренировка 45–48 минут – рекомендую субботу с утра</li>
<li>Короткая тренировка 28–34 минуты – от 2 до 5 раз в неделю</li>
<li>Воскресенье – день отдыха, никаких тренировок</li>
</ul>
"""),
    page("Интенсивность", 3, """
<p class="label">Ваш темп</p>
<h1>Как выбрать интенсивность</h1>
<p>Курс работает и для тех, кто давно не двигался, и для тех, кто хочет интенсивнее. Главное – выберите свой темп сразу, чтобы потом не сорваться.</p>
<table class="rows">
<tr class="pair-start"><td class="k">Если</td><td>Давно не занимались – год и больше, тело отвыкло</td></tr>
<tr class="pair-end"><td class="k">Ваш план</td><td>1 основная + 2 короткие в неделю</td></tr>
<tr class="pair-start"><td class="k">Если</td><td>Сидячая работа, средний уровень активности</td></tr>
<tr class="pair-end"><td class="k">Ваш план</td><td>1 основная + 3–4 короткие в неделю</td></tr>
<tr class="pair-start"><td class="k">Если</td><td>Высокая мотивация, готовы вкладываться</td></tr>
<tr class="pair-end"><td class="k">Ваш план</td><td>1 основная + 5 коротких в неделю</td></tr>
</table>
<h3>Главное правило</h3>
<p>Слушайте тело. Если после короткой практики ощущения «как раз» – это ваш уровень. Если осталась усталость на следующий день – снизьте частоту. Если хочется ещё – добавьте короткую.</p>
<p>Не пытайтесь сразу делать максимум. Лучше начать с меньшего и нарастить, чем начать с большего и сломаться через неделю.</p>
"""),
    page("Безопасность", 4, """
<p class="label">Безопасность</p>
<h1>Когда сначала к врачу</h1>
<p>Я не врач, и курс не заменяет медицинской помощи. Прежде чем начать – проверьте этот список. Если хотя бы один пункт про вас, сначала покажитесь врачу и спросите его про лёгкую регулярную нагрузку.</p>
<ul class="dots">
<li>Острая стреляющая боль прямо сейчас</li>
<li>Онемение в ногах, руках или ягодицах</li>
<li>Недавняя операция или свежая травма позвоночника или таза</li>
<li>Температура или общее воспаление</li>
<li>Острая фаза любого заболевания</li>
<li>Крупная грыжа диска – по заключению врача</li>
</ul>
<div class="note"><p><b>Если в этом списке вас нет – курс ваш.</b> При разумной нагрузке тело становится крепче. Регулярное движение – основа здоровой спины.</p></div>
"""),
    page("Подготовка", 5, """
<p class="label">Подготовка</p>
<h1>Что нужно для занятий</h1>
<table class="rows">
<tr><td class="k">Минимум</td><td>Коврик. Всё.</td></tr>
<tr><td class="k">По желанию</td><td>Подушка или валик под спину, плотный плед, носки</td></tr>
<tr><td class="k">Не нужно</td><td>Гантели, тренажёры, фитбол, спортивная одежда</td></tr>
<tr><td class="k">Место</td><td>Любая комната, где помещается коврик</td></tr>
<tr><td class="k">Одежда</td><td>Удобная, не сковывающая движений</td></tr>
<tr><td class="k">Время</td><td>Когда удобно – но субботу с утра я особенно рекомендую</td></tr>
</table>
<h3>Когда заниматься</h3>
<p>Основная тренировка – суббота, первая половина дня. По моему опыту, так большинству удобнее: впереди выходной, тело может отозваться без спешки.</p>
<p>Короткие практики – в любое удобное время: утром перед работой, вечером перед сном, в обеденный перерыв. Главное – поставить их в расписание и сделать, не дожидаясь, когда будете «готовы».</p>
"""),
    page("Если что-то болит", 6, """
<p class="label">Ощущения</p>
<h1>Если что-то болит</h1>
<p>Лёгкая мышечная усталость или приятное тепло – это нормально, тело работает. Резкая боль, прострелы или ухудшение – сигнал остановиться. Вот как отличить.</p>
<div class="cols">
<div class="box ok"><small>Полезный дискомфорт</small><b>Продолжайте</b><ul><li>Лёгкая усталость мышц</li><li>Тепло в работающих зонах</li><li>Чувство «потянулось»</li><li>Дрожь при удержании позы</li><li>Лёгкая болезненность на следующий день (1–2 дня)</li></ul></div>
<div class="box bad"><small>Тревожный сигнал</small><b>Остановитесь</b><ul><li>Резкая стреляющая боль</li><li>Прострел в ногу или руку</li><li>Онемение конечностей</li><li>Головокружение</li><li>Боль усиливается при движении</li></ul></div>
</div>
<h3>Что делать при тревожном сигнале</h3>
<ol class="num">
<li>Остановите занятие</li>
<li>Полежите на спине, дайте телу 10–15 минут</li>
<li>Не возвращайтесь к курсу 1–2 дня</li>
<li>Если симптомы не уходят за сутки – к врачу</li>
</ol>
"""),
    page("Регулярность", 7, """
<p class="label">Дистанция</p>
<h1>Как не сорваться</h1>
<p>Главный враг курса – перфекционизм. Через 5–7 дней энтузиазм спадёт, и появится мысль «сегодня не буду». Вот что помогает.</p>
<div class="tip"><b>Лучше короче и регулярно</b><p>Три коротких практики в неделю дают больше, чем одна идеальная длинная раз в две недели. Ритм важнее объёма.</p></div>
<div class="tip"><b>Если выпали – продолжайте</b><p>Продолжайте оттуда, где остановились, – начинать заново не нужно. Чувство вины обнуляет результат, простое возвращение к ритму – нет.</p></div>
<div class="tip"><b>Воскресенье – отдых</b><p>Отдых – часть программы: тело восстанавливается именно в эти дни. Не пытайтесь догнать пропущенное в воскресенье.</p></div>
<div class="tip"><b>Распечатайте календарь</b><p>Календарь занятий из комплекта повесьте на видное место. Отмечайте каждое занятие. Видеть три выполненных недели подряд – это удовольствие.</p></div>
<div class="tip"><b>Бонус-аудио – на случай «нет сил»</b><p>30-минутная практика расслабления из 11-го урока – лёгкий вход, когда нет ресурса на полноценную тренировку. Послушать в кровати, в дороге, в обед – и не выпасть из ритма.</p></div>
<div class="tip"><b>После курса – неделя отдыха</b><p>Не нужно сразу запускать новый цикл. Дайте телу неделю свободного режима, а потом решите, что дальше: продолжить самостоятельно, перейти в личное ведение или на тренировки со мной.</p></div>
"""),
    BACK,
]

# ─── Календарь ───────────────────────────────────────────────────────────
def cal_row(n, name):
    cells = "".join('<td><div class="cell"><small>короткая</small><i></i></div></td>' for _ in range(5))
    return (f'<tr><td class="wk"><b>НЕДЕЛЯ {n}</b><span>{name}</span></td>{cells}'
            '<td><div class="cell main"><small>ОСНОВНАЯ</small><i></i></div></td>'
            '<td><div class="cell rest"><small>отдых</small></div></td></tr>')

kalendar = [
    cover("Календарь<br><em>занятий</em>", "3 недели · ваш ритм"),
    page("Календарь", 1, f"""
<p class="label">Ваш путь</p>
<h1>3 недели –<br><em>на одном листе</em></h1>
<p>Распечатайте этот лист, повесьте на холодильник или над рабочим столом. Отмечайте каждое занятие. Видеть три выполненных недели подряд – это удовольствие.</p>
<table class="cal">
<tr><th></th><th>ПН</th><th>ВТ</th><th>СР</th><th>ЧТ</th><th>ПТ</th><th>СБ</th><th>ВС</th></tr>
{cal_row(1, 'Запуск')}{cal_row(2, 'Углубление')}{cal_row(3, 'Закрепление')}
</table>
<div class="legend">
<div><b class="o">● ОСНОВНАЯ</b>Раз в неделю, 45–48 мин · рекомендую субботу с утра</div>
<div><b class="n">● КОРОТКАЯ</b>От 2 до 5 раз в неделю · 28–34 мин · в любой день</div>
<div><b class="g">● ОТДЫХ</b>Воскресенье – день восстановления, без тренировок</div>
</div>
<p class="muted" style="margin-top:4mm">Коротких клеток в неделе пять – отмечайте только те дни, когда занимались. Две-три отметки в неделю – это нормальный темп.</p>
"""),
    page("Как пользоваться", 2, """
<p class="label">Практика</p>
<h1>Как пользоваться календарём</h1>
<div class="tip"><b>Распечатайте</b><p>Повесьте на видное место – холодильник, стена над рабочим столом. Календарь работает, только когда вы его видите.</p></div>
<div class="tip"><b>Отмечайте после</b><p>Отметку ставьте, когда тренировка уже сделана: галочку или закрашенный квадрат, как удобно. Это маленький ритуал, который держит ритм.</p></div>
<div class="tip"><b>Выбирайте интенсивность</b><p>Сколько коротких в неделю – решайте сами: от 2 до 5. Если давно не двигались – начните с двух и слушайте тело.</p></div>
<div class="tip"><b>Если выпали</b><p>Ничего не перерисовывайте и не начинайте заново – просто продолжайте с того дня, где вы сейчас. Курс не школа, чувство вины не двигатель.</p></div>
<div class="tip"><b>Воскресенье – отдых</b><p>Это часть программы: тело восстанавливается именно в этот день. Никаких упражнений, можно просто погулять.</p></div>
<div class="tip"><b>Старт – суббота</b><p>Если есть возможность, начните в субботу с утра. Тогда первая основная тренировка сразу попадёт на нужный день недели – и календарь встанет ровно.</p></div>
"""),
    BACK,
]

# ─── Замеры ──────────────────────────────────────────────────────────────
def diary(title):
    return f'<div class="diary"><small>{title}</small>' + '<div class="ln"></div>' * 5 + '</div>'

zamery = [
    cover("Замеры<br><em>до и после</em>", "Ваш собственный отзыв самому себе"),
    page("Зачем замеры", 1, """
<p class="label">Идея</p>
<h1>Зачем замерять</h1>
<p>Самое незаметное в работе со своим телом – изменения. Они происходят постепенно, понемногу каждый день. И легко пропустить момент, когда вы впервые коснулись пола, хотя три недели назад не могли.</p>
<p>Замеры до и после – ваш собственный отзыв самому себе. Сделайте их сегодня и в день финала курса. Сравните цифры – увидите, что произошло.</p>
<blockquote>«Боль в шее беспокоила два года. Занялась с Алексеем – и в какой-то момент просто поняла, что боль ушла. Не заметила когда.»<cite>Марина, ученица</cite></blockquote>
"""),
    page("Тесты", 2, """
<p class="label">Инструкция</p>
<h1>6 простых тестов</h1>
<p>Сделайте все 6 тестов сегодня, в день старта, и повторите в день финала курса. Запишите оба значения в таблицу.</p>
<table class="tests">
<tr><th>Тест</th><th>До курса</th><th>После курса</th></tr>
<tr><td><b>1. Наклон вперёд</b><span>Сколько сантиметров от кончиков пальцев до пола (стоя, ноги прямые, без рывков)</span></td><td></td><td></td></tr>
<tr><td><b>2. Поворот головы влево</b><span>Комфортно / лёгкое напряжение / трудно</span></td><td></td><td></td></tr>
<tr><td><b>3. Поворот головы вправо</b><span>Комфортно / лёгкое напряжение / трудно</span></td><td></td><td></td></tr>
<tr><td><b>4. Утренняя скованность</b><span>По шкале от 0 до 10 (0 – нет, 10 – очень сильная)</span></td><td></td><td></td></tr>
<tr><td><b>5. Боль в спине за день</b><span>В среднем, по шкале от 0 до 10</span></td><td></td><td></td></tr>
<tr><td><b>6. Общее самочувствие</b><span>По шкале от 0 до 10 (0 – плохо, 10 – отлично)</span></td><td></td><td></td></tr>
</table>
<p class="muted">Распечатайте этот лист и заполните от руки. Или сохраните файл и впишите в редакторе.</p>
"""),
    page("Дневник практики", 3, f"""
<p class="label">Дневник</p>
<h1>Что заметили по ходу</h1>
<p>Необязательно, но полезно. После каждой недели запишите 2–3 наблюдения – что изменилось в теле, в ощущениях, в настроении. К финалу курса перечитаете.</p>
{diary('После недели 1')}{diary('После недели 2')}{diary('После недели 3')}
"""),
    BACK,
]

if __name__ == "__main__":
    build("metodichka", metodichka)
    build("kalendar", kalendar)
    build("zamery", zamery)
