# Frontend (Astro)

`web/` — Astro 4 в режиме SSR (`output: 'server'`, Node adapter, standalone). Сборка в Docker multi-stage, прод запускает скомпилированный `dist/server/entry.mjs`. **Никакого dev-сервера в проде** — фронт поднимается из собранного билда сразу при `make up`.

## Структура

```
web/src/
  pages/
    index.astro                   — главная (новый дизайн: hero, услуги/курсы 2×2, обо мне, статьи)
    coaching.astro                — «Личное ведение» (legacy import index.html) + форма заявки (#lead-form → POST /api/leads, source=coaching)
    consultation.astro            — страница консультации (legacy import) + форма заявки в лист ожидания (#lead-form → POST /api/leads, source=consultation)
    course.astro                  — лендинг бесплатного курса (legacy import). Форма «Начните сегодня» (`#quick-signup-form`) шлёт `POST /api/auth/quick-signup` → если email уже есть, редирект на `/auth/login?email=...`; иначе создаём аккаунт, генерим пароль (на email), ставим cookies и ведём в `/cabinet`.
    results.astro                 — кейсы/результаты учеников (legacy import)
    blog/
      index.astro                 — список статей (fetch /api/articles, дизайн как в сайт/blog.html)
      [slug].astro                — статья (fetch /api/articles/{slug}, content_html через set:html)
    courses/
      index.astro                 — каталог (SiteLayout, маркетинговые карточки с фото/ценой/CTA)
      [slug].astro                — страница курса
      [slug]/lessons/[lesson].astro — урок (HLS-плеер, прогресс)
    cabinet/
      index.astro                 — обзор + мои курсы
      courses.astro               — мои курсы (с уведомлением после оплаты)
      settings.astro              — имя/пароль
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
    auth/
      login.astro
      register.astro
      verify.astro
      forgot.astro
  layouts/
    Base.astro                    — кабинет/админ-layout (palette navy/orange, .container/.btn/.card)
    SiteLayout.astro              — публичный layout сайта (Cormorant + Manrope, общая шапка + футер, поддержка pageStyles; проп `canonical` переопределяет canonical-URL — используется на /courses/myagkiy-start → /course)
  components/
    SiteHeader.astro              — фиксированная навигация для публичных страниц; на ≤1080px ссылки скрываются и включается бургер-меню (6 пунктов + логотип + кнопка Салюта не влезают на планшетах)
    SiteFooter.astro              — футер с социальными иконками
  legacy/                         — оригинальные HTML-исходники маркетинговых страниц, импортируются через ?raw
  lib/
    api.ts                        — server-side fetch к API (apiFetch / apiJson)
    legacy.ts                     — extractLegacy(raw) — достаёт styles+body из legacy HTML, чистит nav/footer
  middleware.ts                   — защита /cabinet/* и /admin/*
  astro.config.mjs                — output:'server', adapter:node
  Dockerfile                      — multi-stage build
```

## Дизайн-система

**Общий CSS:** дизайн-токены (`:root`), шапка `.site-nav`, футер `.site-footer`, `.container`, базовая типографика и универсальные responsive-правила вынесены в `web/src/styles/site.css` — его импортируют оба layout'а. В `SiteLayout.astro` и `Base.astro` остаётся только специфика (утилиты кабинета, `.btn-primary` публичного сайта и т.п.). Legacy-страницы несут свои копии `:root` — значения должны совпадать с site.css (сейчас `--text-light: #6e6e6e`).


Публичные страницы (главная, блог, consultation, course, results) используют **`SiteLayout.astro`** + общие `SiteHeader`/`SiteFooter`. Внутренний кабинет/админка — **`Base.astro`** (минималистичная палитра, таблицы, формы).

Адаптивность: помимо мобильного брейкпоинта 700px, у legacy-страниц (index/consultation/course/results) есть планшетные брейкпоинты 1000–1280px (промежуточные сетки 2–3 колонки, уменьшенные паддинги); у zdorovaya-spina исторически 960/1100. Hero-секции используют `min-height: 100svh` (с fallback `100vh`).

Служебная страница `/test-pay` (проверка интеграции с Продамусом, тариф `test10`) доступна только `role=admin`, остальным — 404.

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

Счётчик подключается в обоих layout'ах, если задан env `METRIKA_ID` (runtime SSR, прокидывается через docker-compose → web). Пусто — скрипт не грузится. Глобальный helper `window.reachGoal('имя_цели')` — no-op без счётчика. Цели: `lead_submit` (формы заявок coaching/consultation), `quick_signup` (форма /course), `checkout_start` (переход к оплате «Здоровой спины»).

## Блог: даты, похожие статьи, JSON-LD

- Карточки и шапка статьи показывают дату публикации (`published_at`).
- Фильтр тегов на /blog синхронизируется с `?tag=` (SSR рендерит уже отфильтрованный список — ссылку можно шарить).
- В статье блок «Похожие статьи» (по тегу, добор свежими) и `BlogPosting` JSON-LD.
- Позиции обложек — общий модуль `web/src/lib/covers.ts` (карточная и полноразмерная версии).

## Трекинг прогресса уроков (кабинет)

Кастомные страницы кабинета (`cabinet/myagkiy-start.astro`, `cabinet/zdorovaya-spina.astro`) отправляют прогресс просмотра в `POST /api/lessons/{id}/progress`: отметка при play, позиция каждые 30 секунд и на паузе, `completed=true` на 90% просмотра или по окончании видео. `<video>` несёт `data-lesson-id`. До 2026-07-13 трекинг в кабинете отсутствовал — lesson_progress был пуст, прогресс-бары всегда 0%.
