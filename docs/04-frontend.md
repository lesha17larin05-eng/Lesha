# Frontend (Astro)

`web/` — Astro 4 в режиме SSR (`output: 'server'`, Node adapter, standalone). Сборка в Docker multi-stage, прод запускает скомпилированный `dist/server/entry.mjs`. **Никакого dev-сервера в проде** — фронт поднимается из собранного билда сразу при `make up`.

## Структура

```
web/src/
  pages/
    index.astro                   — главная (стили landing.css): позиционирование, «Тренировки со мной» (#so-mnoy: персональные, группа, ведение) + курсы, отзывы (ReviewsCarousel), обо мне, статьи, Telegram
    coaching.astro                — «Личное ведение» (не legacy, стили landing.css) + форма заявки (#lead-form → POST /api/leads, source=coaching); legacy/index.html больше не используется
    courses/index.astro           — витрина курсов (карточки из GET /api/courses). Все курсы, включая бесплатный, ведут на /courses/<slug>
    start.astro                   — «Точка перемен» закрыта (2026-10-23): 301 на /personal. legacy/start.html больше не используется; услуга `start` в API осталась для старых заказов
    kurs-oplachen.astro           — страница после оплаты курса в один шаг (urlSuccess quick-checkout, `?course=<slug>`; Продамус добавляет `_payform_order_id`). Пока заказ не `paid` (webhook ещё не пришёл) – «Оплата прошла, открываем доступ» и опрос `GET /api/orders/{id}/status` каждые 3 с; через 45 с – «Подтверждение задерживается» с кнопкой в Телеграм, опрос продолжается. Когда `paid` и заказ оформлен в этом браузере (`can_enter`) – «Сейчас откроем уроки» и через 2,5 с переход на `/api/orders/{id}/enter` (вход без почты). Иначе – на какую почту ушло письмо с кнопкой входа и паролем (маска), кнопки «Войти по коду из письма» и «Войти с паролем»; залогиненному – «Перейти к урокам». noindex
    consultation.astro            — только 301-редирект на /start: услуга «Консультация» закрыта, старые ссылки живут
    (components/SourceCapture.astro — в <head> обоих лейаутов: ?from= / ?utm_source= или referrer → кука `src` на 30 дней, см. docs/02-database.md «Источник трафика»)
    course.astro                  — 301-редирект на /courses/myagkiy-start (с 2026-09-30; старый адрес живёт в рекламе и письмах). Форма «Начните сегодня» (`components/QuickSignupForm.astro`, `#quick-signup-form`, на /courses/myagkiy-start для неавторизованных) подсказывает опечатки в домене почты («gmial.com → gmail.com?», один раз останавливает отправку) и шлёт `POST /api/auth/quick-signup` → если email уже есть, редирект на `/auth/login?email=...`; иначе аккаунт создан, cookies стоят – ведём сразу в `/cabinet/myagkiy-start`. Там для новичка (`can_fix_email`) плашка «Письмо ушло на … · Исправить» → `POST /api/auth/fix-email`.
    results.astro                 — кейсы/результаты учеников (legacy import)
    blog/
      index.astro                 — список статей (fetch /api/articles, дизайн как в сайт/blog.html)
      [slug].astro                — статья (fetch /api/articles/{slug}, content_html через set:html)
    courses/
      index.astro                 — каталог (landing.css, карточки из GET /api/courses: подпись – courses.subtitle, обложки – мапа covers)
      myagkiy-start.astro         — лендинг бесплатного курса (landing.css): уроки и описания из API, форма QuickSignupForm (незалогиненным) или кнопка enroll-free; записанным – ссылки на уроки в кабинете
      zdorovaya-spina.astro       — лендинг «Здоровой спины» (landing.css). Покупка в один шаг через компонент CourseBuy (quick-checkout, без пароля и подтверждения почты), цены и акция – из `tariffs` API. Отзывы – ReviewsCarousel, FAQ + JSON-LD из массива в странице
      zhonglirovanie.astro        — лендинг «Жонглирование: у вас получится» (landing.css). Покупка – CourseBuy; видео знакомства, рилсы из лагеря (миниатюры переключают ролик), FAQ + JSON-LD. Если курс снят с публикации (API 404) – 404 для всех, кроме админа (плашка «Черновик», noindex). legacy/zhonglirovanie.html и legacy/zdorovaya-spina.html больше не используются
      [slug].astro                — страница курса
      [slug]/lessons/[lesson].astro — урок (HLS-плеер, прогресс)
    cabinet/
      index.astro                 — обзор + мои курсы
      courses.astro               — мои курсы (с уведомлением после оплаты)
      settings.astro              — имя/пароль
      zhonglirovanie.astro        — уроки курса жонглирования по образцу кабинета «Здоровой спины»: карточки (номер, метка, название, описание из `lessons.content_md`) и mp4-плеер; пока видео нет на сервере – заглушка «Видео скоро появится». Прогресс просмотра – как у «Здоровой спины»
    admin/
      index.astro                 — дашборд (stats + последние оплаты)
      users.astro                 — список (email → ссылка на карточку)
      users/[id].astro            — карточка пользователя: профиль/согласия, курсы с прогрессом и уроками, оплаты; ссылки на «Выдать доступ» (grant предзаполняется через ?email=)
      courses.astro               — таблица курсов + кнопка «+ Создать курс» (открывает модалку); по клику «Открыть» — переход на страницу курса
      courses/
        [id].astro                — редактор курса: метаданные (collapsible), модули (создание/редактирование/удаление + reorder ↑/↓), уроки сгруппированы по модулям, edit/delete/reorder, модалка с HTML-редактором (toolbar + live preview) + загрузка видео. Использует web-компоненты `<lesson-editor>` и `<module-editor>` из `/admin-course-editor.js`. HTML сохраняется в `lessons.content_md` и рендерится через `marked.parse` (HTML проходит насквозь). |
      blog.astro                  — список статей блога с действиями
      blog/
        new.astro                 — создание статьи
        [id].astro                — редактирование статьи
      activity.astro              — журнал занятий: кто какой урок смотрел/прошёл, группировка по дням, фильтр по курсу
      leads.astro                 — «Люди»: задачи (просрочено / сегодня / до конца недели / дальше, кнопки «Сделано», «+3 дня», «+неделя»), добавление человека и списком, фильтры, правка в окне, выгрузка CSV. Список рисуется на клиенте из JSON (стили is:global, префикс ppl-). В меню админки – «Люди».
      orders.astro
      online.astro
      settings.astro              — тумблеры настроек сайта (сейчас: показывать страницу Салюта)
      mailing.astro               — рассылки: создание (группа, текст, темп) + список кампаний
      mailing/[id].astro          — карточка рассылки: прогресс, пробное письмо, старт/пауза, получатели
      emails.astro                — справочник: какие письма сайт шлёт, кому и когда
    sitemap.xml.ts                — SSR-карта сайта: страницы + все опубликованные статьи /blog/<slug> с lastmod (страница Салюта — только при salut_visible)
    anketa.astro                  — анкета перед занятием (noindex) по ссылке /anketa?u=&t= (письмо после оплаты «Точки перемен» или из админки); вопросы из GET /api/anketa, отправка POST; ответы – в карточке пользователя /admin/users/[id] (там же «скопировать ссылку» / «отправить на почту»); для нового клиента – блок «Анкета перед занятием для клиента» на /admin/users
    unsubscribe.astro             — отписка в два шага (noindex): ссылка «Отписаться» из письма → выбор причины + кнопка; после отписки (и если уже отписан) – «Подписаться снова». Битая подпись – «Ссылка не сработала»
    unsubscribed.astro            — старая страница «Вы отписаны» (новые письма сюда не ведут)
    subscribed.astro              — куда ведёт кнопка «Хочу получать письма» (noindex; ?error=1 — битая ссылка)
    auth/
      login.astro                 — вход: по паролю или «Войти по коду из письма» (`POST /api/auth/code` → поле кода → `/api/auth/code/verify`); `?email=` подставляет почту, `?mode=code` открывает сразу вход по коду, `?next=` – только относительный путь. При неверном пароле – подсказка «пароль есть в письме о доступе, или войдите по коду»
      link.astro                  — кнопка «Перейти к урокам» из письма о доступе: страница сама делает `POST /api/auth/link` (не GET – почтовые сканеры открывают ссылки) и ведёт к курсу; просрочена – «войти по коду» / «с паролем»
      register.astro
      verify.astro
      forgot.astro                — «Ссылка для входа»: письмо со ссылкой задать пароль и войти (для забывших пароль и купивших курс в один шаг); `?email=` подставляет почту
  layouts/
    Base.astro                    — кабинет/админ-layout (palette navy/orange, .container/.btn/.card)
    SiteLayout.astro              — публичный layout сайта (Cormorant + Manrope, общая шапка + футер, поддержка pageStyles; проп `canonical` переопределяет canonical-URL — сейчас не используется); в `<head>` – мета-теги подтверждения Яндекс Вебмастера и Google Search Console
  components/
    SiteHeader.astro              — фиксированная навигация для публичных страниц; на ≤1080px ссылки скрываются и включается бургер-меню (пункты + логотип + кнопка Салюта не влезают на планшетах). У пунктов «Тренировки со мной» и «Курсы» подменю со всеми опубликованными курсами (только названия): на десктопе выпадает при наведении/фокусе, в мобильном меню – отдельной плашкой под «Курсами». Список – `lib/nav-courses.ts` (GET /api/courses, кеш 60 с): новый курс появляется в меню сам после публикации; все курсы ведут на `/courses/<slug>`
    SiteFooter.astro              — футер с социальными иконками
  legacy/                         — оригинальные HTML-исходники маркетинговых страниц, импортируются через ?raw
  lib/
    api.ts                        — server-side fetch к API (apiFetch / apiJson)
    settings.ts                   — флаги сайта из /api/settings с кешем 10 с (getSiteSettings)
    legacy.ts                     — extractLegacy(raw) — достаёт styles+body из legacy HTML, чистит nav/footer
  middleware.ts                   — защита /cabinet/* и /admin/*
  astro.config.mjs                — output:'server', adapter:node
  Dockerfile                      — multi-stage build
```

## Дизайн-система

**Общий CSS:** дизайн-токены (`:root`), шапка `.site-nav`, футер `.site-footer`, `.container`, базовая типографика и универсальные responsive-правила вынесены в `web/src/styles/site.css` — его импортируют оба layout'а. В `SiteLayout.astro` и `Base.astro` остаётся только специфика (утилиты кабинета, `.btn-primary` публичного сайта и т.п.). Legacy-страницы несут свои копии `:root` — значения должны совпадать с site.css (сейчас `--text-light: #6e6e6e`).


Публичные страницы (главная, блог, start, course, results) используют **`SiteLayout.astro`** + общие `SiteHeader`/`SiteFooter`. Внутренний кабинет/админка — **`Base.astro`** (минималистичная палитра, таблицы, формы).

Адаптивность: помимо мобильного брейкпоинта 700px, у legacy-страниц (index/start/results) есть планшетные брейкпоинты 1000–1280px (промежуточные сетки 2–3 колонки, уменьшенные паддинги); у zdorovaya-spina исторически 960/1100. Hero-секции используют `min-height: 100svh` (с fallback `100vh`).

Служебная страница `/test-pay` (проверка интеграции с Продамусом, тариф `test10`) доступна только `role=admin`, остальным — 404.

### Сезонное скрытие страницы Салюта

Флаг `salut_visible` (`GET /api/settings` → `Astro.locals.settings`, тумблер в `/admin/settings`) управляет тремя местами:

- `SiteHeader.astro` — обе кнопки «Здоровая спина в Салюте» (десктопная `.site-nav-salyut` и мобильная `.smm-salyut`) рендерятся только при `salut_visible === true`.
- `salut-2026.astro` — при выключенном флаге передаёт в `Base.astro` проп `noindex` → `<meta name="robots" content="noindex, follow">`. Проп `noindex` есть и у `SiteLayout.astro` (им пользуется `/unsubscribed`).
- `sitemap.xml` — теперь **SSR-роут** `web/src/pages/sitemap.xml.ts` (раньше был статикой в `public/sitemap.xml`, файл удалён). `/salut-2026` попадает в карту только при включённом флаге.

Сама страница `/salut-2026` остаётся доступной по прямой ссылке в любом состоянии флага — чтобы не ломать ссылки, разосланные родителям. Дефолт — выключено.

Палитра публичного сайта (CSS-переменные в `SiteLayout.astro`):
- `--navy: #1a2744`, `--navy-mid: #243058`, `--navy-light: #2e3d6b`
- `--orange: #e8652a`, `--orange-warm: #f07530`
- `--cream: #f8f5ef`, `--warm-white: #fdfaf5`
- `--text: #1a1a1a`, `--text-mid: #444`, `--text-muted: #777`, `--text-light: #aaa`, `--border: #ece8e0`

Шрифты публичного сайта: **Cormorant Garamond** (заголовки, italic-акценты) + **Manrope** (sans, основной). Self-hosted: woff2-файлы в `web/public/fonts/` + `fonts.css` (subsets cyrillic/latin), Google CDN не используется.

В `Base.astro` (кабинет/админка):
- `--navy: #1a2744`, `--navy-light: #243058`
- `--orange: #e8652a`, `--orange-light: #f07840`
- `--blue: #2b5fa8`, `--cream: #f9f7f3`, `--warm: #fefcf8`

Шрифты: те же self-hosted Manrope + Cormorant Garamond из `web/public/fonts/`.

Базовые классы: `.container`, `.btn / .btn.secondary`, `.card`, `.grid / .grid-3`, `.badge.{free,paid,owned}`, `.input`, `.form`, `.alert / .alert.ok`, `.progress`, `.muted`, `.lock` (затенение заблокированных уроков).

## Middleware

`web/src/middleware.ts`:
- Каждый запрос → `apiJson('/api/me', { cookie })` → кладёт user в `Astro.locals.user`.
- Каждый запрос → `getSiteSettings()` (`web/src/lib/settings.ts`, кеш в памяти процесса на 10 с) → кладёт флаги в `Astro.locals.settings`.
- `/cabinet/*` без auth → редирект на `/auth/login?next=...`.
- `components/SubscribeInvite.astro` – плашка «Присылать вам полезные письма? Да, присылайте / Не сейчас» в кабинете и на страницах уроков (`/cabinet`, `/cabinet/myagkiy-start|zdorovaya-spina|zhonglirovanie`). Показывается по флагу `subscribe_offer` из `/api/me` (нет согласия и человек не отписывался); «Да» – `PATCH /api/me {consent_marketing:true}`, «Не сейчас» прячет в этом браузере (localStorage). В настройках галочка рассылки отправляется, только если её поменяли (иначе «Сохранить имя» отписывало бы).
- Форма покупки жонглирования: галочка «Присылать полезные письма» (по умолчанию отмечена, как в форме «Мягкого старта») → `consent_marketing` в quick-checkout.
- `/admin/*` без `role=admin` → 404 (намеренно, чтобы не палить наличие).

API-проверки на бэкенде дублируют — middleware фронта это только UX, не security.

## Auth state на клиенте

- Серверный рендер уже знает user через middleware.
- Клиентский JS читает cookie `auth=1` (нечувствительный) для мгновенного UI без ожидания сети.
- Все мутирующие fetch автоматически получают `X-CSRF-Token` через обёртку в `Base.astro`.

## Страница урока

- Sidebar со списком уроков курса, текущий выделен.
- Если у урока есть `video_id` — добавляется `<video>` + плеер. Скрипт делает `fetch('/api/videos/{id}/playback')`, получает signed URL, инициализирует hls.js. На Safari работает нативный HLS.
- Прогресс сохраняется в БД раз в 10 секунд через `POST /api/lessons/{id}/progress` (`completed: t/d > 0.9`).
- Кнопка «Отметить как пройденный» отдельно.

## Сборка

- `web/Dockerfile`: stage 1 — `npm install` + `npm run build` (Astro собирает SSR в `dist/`). Stage 2 — node:20-alpine + только `package.json` + `node_modules` + `dist/`.
- Команда: `make web-build` (только web) или `make build` (api + web).
- Запуск контейнера: `node ./dist/server/entry.mjs` слушает `0.0.0.0:4321`.

## Аналитика (Яндекс.Метрика)

Счётчик подключается в обоих layout'ах, если задан env `METRIKA_ID` (runtime SSR, прокидывается через docker-compose → web). Пусто — скрипт не грузится. Глобальный helper `window.reachGoal('имя_цели')` — no-op без счётчика. Цели: `lead_submit` (формы заявок coaching/start/gruppa/personal), `messenger_click` (кнопки Telegram/Max на лендингах), `quick_signup` (форма на /courses/myagkiy-start), `checkout_start` (переход к оплате «Здоровой спины»).

## Поле-ловушка для ботов

Во всех публичных формах (QuickSignupForm, регистрация, NewsletterForm, CourseBuy, заявки на /personal, /gruppa, /coaching, оплата группы) – скрытое поле `<input name="homepage" class="hp-field">`; значение уходит в JSON. Стиль `.hp-field` – в site.css (вне экрана). Сервер отклоняет формы с заполненной ловушкой (см. 03-api.md).

## Согласия в формах (27.10.2026)

Ни одна галочка не стоит заранее – человек ставит сам (152-ФЗ: согласие сознательное; с 01.09.2025 – отдельно от других документов).
- Согласие на обработку ПД – отдельная обязательная галочка во всех формах; без неё форма не отправляется.
- Оферта – отдельная обязательная галочка (`name="offer"`, проверяется на клиенте) там, где оплата: CourseBuy и форма оплаты группы (/gruppa#oplata).
- Письма (`consent_marketing`) – по желанию. «Мягкий старт»: «Получать письма-поддержку во время курса и первым узнавать про новые оздоровительные проекты, практики и тренировки»; регистрация и покупка: «Получать письма Алексея про новые оздоровительные проекты, практики и тренировки».

## Блог: даты, похожие статьи, JSON-LD

- Карточки и шапка статьи показывают дату публикации (`published_at`).
- Теги блога (октябрь 2026): «Спина и тело», «Начать и не бросить», «Сон и энергия», «Голова и стресс», «Похудение и питание», «Бег и выносливость» – цвета в `tagClasses` в `blog/index.astro`; тег задаётся в HTML статьи (`span.article-tag`).
- `blog/[slug].astro`: 301 для объединённых статей (princip-malenkih-shagov → kak-nachat-trenirovatsya, kak-sformirovat-privychku-trenirovatsya → pochemu-brosaju-trenirovki, trenirovki-i-mentalnoe-zdorove → kak-snizit-trevozhnost); скрытые статьи (миграция 029) при 404 – 302 на /blog.
- Фильтр тегов на /blog синхронизируется с `?tag=` (SSR рендерит уже отфильтрованный список — ссылку можно шарить).
- В статье блок «Похожие статьи» (по тегу, добор свежими) и `BlogPosting` JSON-LD.
- Позиции обложек — общий модуль `web/src/lib/covers.ts` (карточная и полноразмерная версии).

## Трекинг прогресса уроков (кабинет)

Кастомные страницы кабинета (`cabinet/myagkiy-start.astro`, `cabinet/zdorovaya-spina.astro`) отправляют прогресс просмотра в `POST /api/lessons/{id}/progress`: отметка при play, позиция каждые 30 секунд и на паузе, `completed=true` на 90% просмотра или по окончании видео. `<video>` несёт `data-lesson-id`. До 2026-07-13 трекинг в кабинете отсутствовал — lesson_progress был пуст, прогресс-бары всегда 0%.

## Лендинг «Мягкого старта»

Основной адрес – `/courses/myagkiy-start` (`/course` отдаёт 301 сюда). Незалогиненным в блоке `#get-access` – форма быстрой записи `QuickSignupForm`, залогиненным без записи – кнопка «Записаться бесплатно» (`POST /api/courses/myagkiy-start/enroll-free`), записанным блок не показывается. Кнопка в hero для незалогиненных ведёт на `#get-access`.

Сетка карточек «Уроки курса» на `/courses/myagkiy-start` показывается только тем, кто ещё не записан (записанным хватает кнопки «Перейти к урокам» и списка в hero). Обложки – кадры из видео в `web/public/img/myagkiy-start/<slug>.{jpg,webp}` (+@2x), клик ведёт к блоку регистрации `#get-access`. Страницы `/courses/myagkiy-start/lessons/<slug>` встраивают YouTube, а CSP (`frame-src` в `nginx/nginx.conf`) его не пропускает – поэтому с лендинга на них больше не ссылаемся.

## Дожим и удержание

- `/auth/check-email`: кнопка «Письмо не пришло — отправить ещё раз» → POST /api/auth/resend-verification. Такая же кнопка в карточке пользователя админки (для неподтверждённых).
- `/cabinet`: блок «Продолжить с того же места» (GET /api/me/continue) — последний урок, позиция, ссылка с якорем в кабинет курса.
- `/cabinet/myagkiy-start`: при 100% прогресса и отсутствии «Здоровой спины» — cross-sell баннер (цель Метрики `crosssell_click`).
- Письмо после оплаты: ссылка на кабинет курса, контакты для вопросов (webhook Продамуса).
- CSP (nginx): разрешены mc.yandex.ru / mc.webvisor.org для Метрики; Google Fonts удалены из CSP (self-hosted).

## Шапка и типографика

Пункты меню (`components/SiteHeader.astro`): «Тренировки со мной» (подменю: /personal, /gruppa, /coaching; сама ссылка – /#so-mnoy), «Курсы» (подменю из API), Статьи, Отзывы. Подсветка «Тренировки со мной» – на страницах с `active` home/personal/gruppa. `/consultation` и `/start` – 301 на /personal.

Базовый кегль — 18px. Весь мелкий текст поднят на ступень относительно исходного макета (11→12, 13→14, 15→16, 17→18, 20→21, 22→23); заголовки заданы через `clamp()` и масштабируются от ширины экрана. На телефоне у `/start` кегль заголовка и цены переопределён в медиазапросе 700px.

## Иконки соцсетей на /results

Кнопки «ВКонтакте» и «YouTube» повторяются у каждого из 65 отзывов. Раньше SVG был вписан в разметку 132 раза и раздувал страницу до 186 КБ. Теперь иконки объявлены один раз спрайтом сразу после `<body>` в `legacy/results.html` (`<symbol id="i-vk">`, `<symbol id="i-yt">`), а кнопки ссылаются на них через `<svg class="rv-ico"><use href="#i-vk">`. Размер — через класс `.rv-ico`, цвет наследуется от кнопки (`fill: currentColor`).

## Счётчик чтения статей

`pages/blog/[slug].astro` шлёт три события на `POST /api/articles/{slug}/view`:

- `open` — при открытии страницы;
- `read` — когда конец `.article-body` попал в экран (IntersectionObserver, каждое событие шлётся один раз);
- `cta` — клик по ссылке на любой продукт: `/start`, `/coaching`, `/course` и `/courses/*`.

Отправка через `navigator.sendBeacon`, с откатом на `fetch(..., {keepalive:true})`. Ничего о посетителе не передаётся.

Цифры видны в `/admin/blog` — колонки «Открыли» (в скобках за 30 дней), «Дочитали» и «Перешли дальше» с долей от открытий.

### Призыв в конце статьи

Блок `.article-cta-final` в `pages/blog/[slug].astro` ведёт на «Точку перемен» (2 990 ₽) — это основное действие. Бесплатный курс остался запасным вариантом тихой строкой ниже. Оба клика попадают в счётчик как событие `cta`.

## Форма подписки на письма

Компонент `components/NewsletterForm.astro` (props: `title`, `text`, `tone`). Стоит в конце статьи блога и под отзывами на `/results` — там, где человек уже прогрет, но платить не готов.

Подписка в два шага: форма шлёт `POST /api/newsletter`, после чего показывает «проверьте почту». Согласие на рассылку появляется только после нажатия ссылки из письма. Цель Метрики — `newsletter_signup`.

## Главная

Первый экран ведёт на «Точку перемен» одной кнопкой, под ней строка с составом и ценой; бесплатный курс — вторым, тихим действием. Витрина форматов называется «С чего можно начать» и прямо рекомендует «Точку перемен» тем, кто не знает, с чего начать. Ниже — четыре видеоотзыва с `/results` (те же ролики, `preload="none"`).

## Автозапуск чекаута после входа (фикс гонки CSRF)

На лендингах `zdorovaya-spina` и `zhonglirovanie` после входа через модалку страница перезагружается с `?tariff=…` и сама запускает чекаут. Раньше это делал inline-скрипт сразу при загрузке – до модульного скрипта SiteLayout, который оборачивает `fetch` и ставит `X-CSRF-Token`. Итог: `POST /checkout` → 403 и alert «Не удалось создать платёж». Теперь автозапуск ждёт `DOMContentLoaded` (модульные скрипты к нему уже выполнены).

## Курсы-черновики

Курс с `is_published=false` не виден в витрине и sitemap, его лендинг отдаёт 404 всем, кроме админа. Публикация – галочка «Опубликован» в `/admin/courses/[id]`; опубликованный курс сам появляется в витрине (`GET /api/courses`) и в `sitemap.xml` (список `DRAFTABLE` в `sitemap.xml.ts`).

### /gruppa – групповое ведение

Страница `web/src/pages/gruppa.astro` (не legacy). Запись – через заявку (`source=gruppa`); блок оплаты скрыт и открывается по ссылке `/gruppa#oplata`, которую Алексей присылает после переписки. `/anketa` берёт тексты из `form` ответа API и передаёт `k` дальше.

### /personal – персональные тренировки

Страница `web/src/pages/personal.astro` (не legacy, стили – как у `/gruppa`). Онлайн и выезд на дом, цены и условия пакетов – в разметке. Запись – через Telegram/Max или заявку (`source=personal`). Блока оплаты нет: первое занятие и пакеты Алексей оплачивает ссылкой сам. До 7 ноября (пока в группе есть места) под ценами – ссылка на `/gruppa`.

Кнопки «Написать в Telegram / Max» на `/gruppa` и `/personal` отправляют цель Метрики `messenger_click` (атрибут `data-goal`).

Первый экран у лендингов (`/gruppa`, `/personal`, `/coaching`, `/start`): текст прижат к верху, первая строка – в 80px под меню на десктопе.

Общие стили лендингов – `web/src/styles/landing.css` (классы `gp-*`, подключается импортом в странице). Минимальный размер текста – 15px, основной – 17px. `/coaching-preview` (черновик новой страницы) – 301 на `/coaching`.

Покупка курса в один шаг – `web/src/components/CourseBuy.astro` (кнопки `[data-tariff][data-label][data-price]`, окно «имя + почта» → `POST /api/courses/{slug}/quick-checkout`, отсчёт акции `[data-countdown]`).

Карусель отзывов – `web/src/components/ReviewsCarousel.astro`: бесконечная лента (у края крайняя карточка переносится на другую сторону), видео грузится по клику, обложки – когда лента рядом с экраном.
