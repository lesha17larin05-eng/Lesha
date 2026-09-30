# Frontend (Astro)

`web/` — Astro 4 в режиме SSR (`output: 'server'`, Node adapter, standalone). Сборка в Docker multi-stage, прод запускает скомпилированный `dist/server/entry.mjs`. **Никакого dev-сервера в проде** — фронт поднимается из собранного билда сразу при `make up`.

## Структура

```
web/src/
  pages/
    index.astro                   — главная (новый дизайн: hero, услуги/курсы 2×2, обо мне, статьи)
    coaching.astro                — «Личное ведение» (legacy import index.html) + форма заявки (#lead-form → POST /api/leads, source=coaching)
    courses/index.astro           — витрина курсов (карточки из GET /api/courses). Все курсы, включая бесплатный, ведут на /courses/<slug>
    start.astro                   — «Точка перемен» (legacy import): занятие 55 мин + неделя сопровождения, 2 990 ₽. Форма заявки (#lead-form → POST /api/leads, source=start)
    consultation.astro            — только 301-редирект на /start: услуга «Консультация» закрыта, старые ссылки живут
    course.astro                  — 301-редирект на /courses/myagkiy-start (с 2026-09-30; старый адрес живёт в рекламе и письмах). Форма «Начните сегодня» (`components/QuickSignupForm.astro`, `#quick-signup-form`, на /courses/myagkiy-start для неавторизованных) подсказывает опечатки в домене почты («gmial.com → gmail.com?», один раз останавливает отправку) и шлёт `POST /api/auth/quick-signup` → если email уже есть, редирект на `/auth/login?email=...`; иначе аккаунт создан, cookies стоят – ведём сразу в `/cabinet/myagkiy-start`. Там для новичка (`can_fix_email`) плашка «Письмо ушло на … · Исправить» → `POST /api/auth/fix-email`.
    results.astro                 — кейсы/результаты учеников (legacy import)
    blog/
      index.astro                 — список статей (fetch /api/articles, дизайн как в сайт/blog.html)
      [slug].astro                — статья (fetch /api/articles/{slug}, content_html через set:html)
    courses/
      index.astro                 — каталог (SiteLayout, маркетинговые карточки с фото/ценой/CTA; обложки – мапа covers)
      zhonglirovanie.astro        — лендинг «Жонглирование: у вас получится» (legacy/zhonglirovanie.html через extractLegacy; фото IMG_4035, схема фонтана /img/zhonglirovanie/fontan.svg). Тарифы self/support, модалка регистрации/входа и автозапуск чекаута – как у «Здоровой спины». Если курс снять с публикации (API 404) – страница 404 для всех, кроме админа (у админа жёлтая плашка «Черновик» и noindex)
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
      leads.astro                 — заявки с сайта: таблица + смена статуса (PATCH /api/admin/leads/{id})
      orders.astro
      online.astro
      settings.astro              — тумблеры настроек сайта (сейчас: показывать страницу Салюта)
      mailing.astro               — рассылки: создание (группа, текст, темп) + список кампаний
      mailing/[id].astro          — карточка рассылки: прогресс, пробное письмо, старт/пауза, получатели
      emails.astro                — справочник: какие письма сайт шлёт, кому и когда
    sitemap.xml.ts                — SSR-карта сайта (страница Салюта — только при salut_visible)
    unsubscribed.astro            — куда ведёт ссылка «отписаться» из письма (noindex; ?error=1 — битая ссылка)
    subscribed.astro              — куда ведёт кнопка «Хочу получать письма» (noindex; ?error=1 — битая ссылка)
    auth/
      login.astro
      register.astro
      verify.astro
      forgot.astro
  layouts/
    Base.astro                    — кабинет/админ-layout (palette navy/orange, .container/.btn/.card)
    SiteLayout.astro              — публичный layout сайта (Cormorant + Manrope, общая шапка + футер, поддержка pageStyles; проп `canonical` переопределяет canonical-URL — сейчас не используется)
  components/
    SiteHeader.astro              — фиксированная навигация для публичных страниц; на ≤1080px ссылки скрываются и включается бургер-меню (6 пунктов + логотип + кнопка Салюта не влезают на планшетах). У пункта «Курсы» подменю со всеми опубликованными курсами (только названия): на десктопе выпадает при наведении/фокусе, в мобильном меню – отдельной плашкой под «Курсами». Список – `lib/nav-courses.ts` (GET /api/courses, кеш 60 с): новый курс появляется в меню сам после публикации; все курсы ведут на `/courses/<slug>`
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

Счётчик подключается в обоих layout'ах, если задан env `METRIKA_ID` (runtime SSR, прокидывается через docker-compose → web). Пусто — скрипт не грузится. Глобальный helper `window.reachGoal('имя_цели')` — no-op без счётчика. Цели: `lead_submit` (формы заявок coaching/start), `quick_signup` (форма на /courses/myagkiy-start), `checkout_start` (переход к оплате «Здоровой спины»).

## Блог: даты, похожие статьи, JSON-LD

- Карточки и шапка статьи показывают дату публикации (`published_at`).
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

Пункты меню (`components/SiteHeader.astro`): Точка перемен, Личное ведение, Курсы, Отзывы, Статьи. Отдельных пунктов «Бесплатный курс» и «Здоровая спина» больше нет — оба курса живут на витрине `/courses`, страницы курсов подсвечивают пункт через `active="courses"`.

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
