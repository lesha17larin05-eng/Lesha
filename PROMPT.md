# ТЗ для Claude Code: платформа онлайн-курсов «Алексей Ларин»

## 0. Контекст и цель

Нужно построить production-ready платформу для тренера по здоровью. Существующий лендинг (HTML/CSS, Manrope + Playfair Display, палитра navy `#1a2744` / orange `#e8652a` / cream `#f9f7f3`) переносим как главную страницу. Поверх него — система курсов (бесплатных и платных), личный кабинет, админка, приём оплат через Продамус, защищённая отдача видео.

**Стек:** Astro (frontend, SSR-режим), Go 1.22+ (backend API), PostgreSQL 16, nginx, всё в Docker Compose на одном VPS. Никаких внешних SaaS кроме Продамуса и SMTP.

**Принцип работы:** действуй итеративно. Сначала каркас и инфраструктура, потом домен за доменом (auth → courses → payments → admin → video). После каждого крупного блока — сборка, прогон тестов, ручная проверка по чек-листу. Не пиши весь код сразу — сначала спроси себя «можно ли это запустить и проверить прямо сейчас», и если нет — разбей на меньшие шаги.

---

## 1. Архитектура

```
┌─────────────────────────────────────────────────────────────┐
│                      nginx (443/80)                          │
│  ┌──────────────┬──────────────┬───────────────────────────┐│
│  │   /          │   /api/*     │   /video/* (internal)    ││
│  │   → astro    │   → go-api   │   X-Accel-Redirect       ││
│  └──────────────┴──────────────┴───────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
         │                │                    │
    ┌────▼────┐      ┌────▼────┐         ┌────▼────────┐
    │  astro  │      │  go-api │◄────────┤ postgres 16 │
    │  :4321  │      │  :8080  │         │   :5432     │
    └─────────┘      └────┬────┘         └─────────────┘
                          │
                  ┌─────────────────────┐
                  │ /srv/leshalarin/    │  (bind mount на хосте,
                  │   videos/  ← rw api │   видна напрямую через ls,
                  │            ← ro nginx│   переживает любой docker rm)
                  │   pgdata/  ← postgres│
                  │   uploads/ ← rw api │
                  │   backups/          │
                  └─────────────────────┘
```

### Сервисы в `docker-compose.yml`
- `postgres` — Postgres 16, healthcheck. Данные — в bind mount.
- `api` — Go-бинарник, порт 8080 только во внутренней сети.
- `web` — Astro в SSR-режиме (Node adapter), порт 4321 только во внутренней сети.
- `nginx` — единственный наружу. Слушает 80 (редирект на 443) и 443 (TLS через Let's Encrypt + certbot, либо примонтированные сертификаты — сделай настройку через переменную окружения).
- `migrate` — отдельный one-shot контейнер для применения миграций (`golang-migrate/migrate`), запускается перед `api`.

### Сети и хранилище
- Сеть `internal` — все сервисы. Наружу торчит только nginx.
- **Никаких named volumes — всё через bind mounts** в директорию хоста `/srv/leshalarin/` (для локальной разработки — `./data/` рядом с проектом). Это позволяет:
  - Видеть файлы в обычном `ls`, бэкапить через `rsync`/`tar` без знания Docker.
  - Полностью пересоздавать контейнеры (`docker compose down`, `docker compose down -v`, `docker system prune`) без риска потерять данные.
  - Переносить проект на другой VPS простым `rsync /srv/leshalarin/`.
- Структура на хосте:
  ```
  /srv/leshalarin/
    pgdata/      — данные Postgres (mount в postgres:/var/lib/postgresql/data)
    videos/      — исходники + HLS (mount в api:/var/videos rw, в nginx:/var/videos ro)
    uploads/     — обложки курсов и прочая статика (mount в api:/var/uploads rw)
    backups/     — pg_dump артефакты (mount в postgres:/backups rw)
  ```
- **Права на хосте:** создай пользователя `app` (UID 1000) на VPS, назначь владельцем `/srv/leshalarin/`, в Dockerfile-ах api/postgres укажи тот же UID (`USER 1000`). Иначе контейнер не сможет писать в bind mount, либо запишет от root и потом ты не удалишь файлы без sudo. **Это самая частая боль bind mount — реши её сразу.** Postgres-образ чуть капризнее (там свой `postgres` user UID 999) — для него либо отдельный bind mount с правильным владельцем, либо через `user: "999:999"` в compose.
- Путь к корню задаётся переменной `DATA_DIR` в `.env` (по умолчанию `./data` для dev, `/srv/leshalarin` для прод).
- `.env` файл — все секреты, в репозиторий не коммитим. В репозиторий кладём `.env.example`.

---

## 2. База данных (PostgreSQL)

Используй `golang-migrate`, миграции в `api/migrations/*.up.sql` / `*.down.sql`. UUID для всех ID (`gen_random_uuid()`, расширение `pgcrypto`). Все таймстемпы — `TIMESTAMPTZ`, по умолчанию `now()`.

### Таблицы

**users**
- `id` UUID PK
- `email` CITEXT UNIQUE NOT NULL
- `password_hash` TEXT NOT NULL  *(argon2id)*
- `name` TEXT
- `role` TEXT NOT NULL DEFAULT `'user'` CHECK in (`'user'`, `'admin'`)
- `email_verified_at` TIMESTAMPTZ
- `last_seen_at` TIMESTAMPTZ  *(используется для «кто онлайн»)*
- `created_at`, `updated_at`

**email_verification_tokens**, **password_reset_tokens**
- `id`, `user_id` FK, `token_hash` TEXT, `expires_at`, `used_at`

**sessions** *(server-side для refresh)*
- `id` UUID, `user_id` FK, `refresh_token_hash` TEXT UNIQUE, `user_agent`, `ip`, `expires_at`, `revoked_at`

**courses**
- `id`, `slug` TEXT UNIQUE, `title`, `subtitle`, `description` TEXT, `cover_image_url`
- `kind` TEXT CHECK in (`'free'`, `'paid'`)
- `price_rub` INTEGER NULL  *(NULL для free, копейки или рубли — выбери одно и зафиксируй; рекомендую целые рубли, в Продамус так удобнее)*
- `prodamus_product_id` TEXT NULL  *(идентификатор товара в кабинете Продамус)*
- `is_published` BOOLEAN DEFAULT FALSE
- `sort_order` INTEGER
- `kind = 'free'` → структура простая (см. lessons с module_id NULL допустим). `kind = 'paid'` → обязательно через модули.
- `created_at`, `updated_at`

**modules**
- `id`, `course_id` FK CASCADE, `title`, `sort_order`

**lessons**
- `id`, `course_id` FK CASCADE, `module_id` FK NULL (для free курса можно без модулей), `title`, `slug`
- `content_md` TEXT  *(markdown тела урока)*
- `video_id` FK NULL → `videos.id`
- `duration_sec` INTEGER
- `sort_order` INTEGER
- `is_preview` BOOLEAN DEFAULT FALSE  *(для платного курса можно показать «бесплатный пробный урок»)*

**videos**
- `id`, `original_filename`, `storage_path` TEXT  *(путь внутри контейнера, например `/var/videos/{uuid}/index.m3u8`; на хосте это `/srv/leshalarin/videos/{uuid}/index.m3u8`)*
- `hls_master_playlist` TEXT  *(относительный путь к мастер-плейлисту от корня videos/)*
- `duration_sec`, `size_bytes`
- `status` TEXT CHECK in (`'uploading'`, `'processing'`, `'ready'`, `'failed'`)
- `created_at`

**enrollments** *(у кого к чему есть доступ)*
- `id`, `user_id` FK, `course_id` FK, `granted_at`, `granted_by` TEXT CHECK in (`'purchase'`, `'admin'`, `'free'`), `granted_by_admin_id` UUID NULL
- UNIQUE (`user_id`, `course_id`)

**lesson_progress**
- `id`, `user_id` FK, `lesson_id` FK
- `completed_at` TIMESTAMPTZ NULL
- `last_position_sec` INTEGER DEFAULT 0  *(где остановился в видео)*
- UNIQUE (`user_id`, `lesson_id`)

**orders** *(заказы / попытки оплаты)*
- `id` UUID, `order_num` BIGSERIAL UNIQUE  *(человекочитаемый номер; передаётся в Продамус как `order_num`)*
- `user_id` FK, `course_id` FK
- `amount_rub` INTEGER
- `status` TEXT CHECK in (`'pending'`, `'paid'`, `'failed'`, `'refunded'`)
- `prodamus_order_id` TEXT NULL  *(их `order_id` если приходит)*
- `paid_at`, `created_at`

**payment_webhooks** *(сырой лог входящих вебхуков от Продамуса для аудита)*
- `id`, `received_at`, `headers` JSONB, `body` JSONB, `signature_valid` BOOLEAN, `processed` BOOLEAN, `processing_error` TEXT NULL, `order_id` UUID NULL FK

**video_access_log** *(для будущей аналитики и детекта расшаривания)*
- `id`, `user_id` FK, `video_id` FK, `ip` INET, `user_agent` TEXT, `accessed_at`

**audit_log** *(действия в админке)*
- `id`, `admin_id` FK, `action` TEXT, `target_type` TEXT, `target_id` UUID, `meta` JSONB, `created_at`

### Индексы
Минимум: `enrollments(user_id)`, `enrollments(course_id)`, `lessons(course_id, sort_order)`, `lesson_progress(user_id, lesson_id)`, `orders(user_id)`, `orders(status, created_at)`, `users(last_seen_at)`, `payment_webhooks(received_at)`.

---

## 3. Backend (Go)

### Структура проекта
```
api/
  cmd/server/main.go
  internal/
    config/      — загрузка env через caarlos0/env или viper
    db/          — pgxpool, репозитории
    auth/        — argon2id, JWT, middleware
    handlers/    — HTTP handlers по доменам
    services/    — бизнес-логика (CourseService, EnrollmentService, PaymentService, VideoService)
    prodamus/    — клиент + проверка HMAC
    video/       — выдача signed URL, HLS
    email/       — SMTP отправка (verification, reset, payment receipt)
    middleware/  — auth, admin-only, ratelimit, request-id, recover, csrf
  migrations/
  Dockerfile
  go.mod
```

### HTTP-фреймворк и зависимости
- `chi` (router) — простой, идиоматичный.
- `pgx/v5` + `pgxpool` (Postgres). **Не используй database/sql + lib/pq** — pgx эффективнее и нативно поддерживает JSONB.
- `golang-jwt/jwt/v5` — JWT.
- `golang.org/x/crypto/argon2` — хеширование паролей.
- `golang-migrate/migrate/v4`.
- Логирование: `log/slog` (stdlib).
- Валидация: `go-playground/validator/v10`.

### Аутентификация
- **Пароли:** argon2id, параметры `time=2, memory=64*1024, threads=4, keyLen=32, saltLen=16`. Хранить как `argon2id$v=19$m=65536,t=2,p=4$<saltb64>$<hashb64>`.
- **Токены:** короткоживущий **access JWT** (15 минут, HS256, секрет в env) + длинноживущий **refresh token** (random 32 байта → SHA256 → хранится в БД, expires 30 дней). Refresh — в **httpOnly Secure SameSite=Lax cookie** на пути `/api/auth/refresh`. Access — тоже httpOnly cookie на `/api/`. **Не клади токены в localStorage.**
- **CSRF:** double-submit token. На первом запросе ставим cookie `csrf=<random>` (не httpOnly), фронт читает и шлёт в заголовке `X-CSRF-Token` для всех мутирующих методов. Middleware проверяет совпадение.
- **Email verification:** при регистрации создаём запись в `email_verification_tokens` (token хранится как hash), шлём ссылку с raw token, по клику — `POST /api/auth/verify-email`. До верификации пускаем читать бесплатные курсы, **но не даём записаться на платный и не даём пройти оплату**.
- **Rate limit на /auth/\*:** 5 попыток / 15 минут на IP+email (in-memory token bucket — для одного VPS достаточно).
- **`last_seen_at`:** обновлять не на каждый запрос, а раз в 60 секунд (через middleware с проверкой «прошло ли 60с с последнего апдейта в этой сессии»).

### Эндпоинты (все под `/api`)

**Public**
- `POST /auth/register` — `{email, password, name}` → создаёт user, шлёт письмо.
- `POST /auth/verify-email` — `{token}`.
- `POST /auth/login` — `{email, password}` → ставит cookies.
- `POST /auth/logout` — отзывает refresh.
- `POST /auth/refresh` — обновляет access.
- `POST /auth/forgot-password`, `POST /auth/reset-password`.
- `GET /courses` — список **опубликованных** курсов (без приватных полей).
- `GET /courses/:slug` — курс + структура модулей/уроков (для бесплатных — с контентом, для платных — только meta + первый `is_preview` урок).

**Authenticated user**
- `GET /me` — профиль.
- `PATCH /me` — имя, смена пароля.
- `GET /me/courses` — мои курсы (через enrollments) с прогрессом (% завершённых уроков).
- `GET /courses/:slug/lessons/:lesson_slug` — отдаёт контент урока **только если есть enrollment** (либо урок `is_preview`, либо курс `kind='free'`).
- `POST /lessons/:id/progress` — `{completed: bool, last_position_sec: int}` (upsert в `lesson_progress`).
- `GET /videos/:id/playback` — возвращает `{playback_url: "/video/<signed-token>"}`. Сервер выдаёт **signed URL** (см. раздел 5). **Только если у юзера есть enrollment на курс, в котором этот video используется.**
- `POST /courses/:slug/checkout` — создаёт `order` со статусом `pending`, возвращает URL платёжной страницы Продамуса с подставленными параметрами (см. раздел 4).

**Webhook (без auth)**
- `POST /webhooks/prodamus` — см. раздел 4.

**Admin (`role = 'admin'`)**
- `GET /admin/stats` — счётчики: всего юзеров, онлайн (last_seen_at < 5 мин), оплат за сегодня/неделю/месяц, выручка.
- `GET /admin/users?search=&page=` — список с пагинацией.
- `GET /admin/users/:id` — детали + список enrollments + список заказов.
- `POST /admin/users/:id/enrollments` — `{course_id}` — выдать доступ вручную (granted_by='admin').
- `DELETE /admin/users/:id/enrollments/:course_id` — отозвать.
- `POST /admin/courses` / `PATCH /admin/courses/:id` / `DELETE /admin/courses/:id`.
- `POST /admin/courses/:id/modules`, `POST /admin/modules/:id/lessons` и т.д. (CRUD).
- `POST /admin/videos/upload` — multipart upload, см. раздел 5.
- `GET /admin/orders?status=&from=&to=` — список оплат.
- `GET /admin/online-users` — `last_seen_at` за последние 5 минут.
- `GET /admin/audit-log`.

Каждое write-действие админа пишется в `audit_log`.

### Middleware
- `RequestID` — uuid в каждом запросе, в логах.
- `Logger` — slog в JSON формате.
- `Recover` — паника → 500.
- `CORS` — только нужный origin.
- `Auth` — парсит JWT из cookie, кладёт user в context. Не валидный → 401.
- `RequireAuth` / `RequireAdmin`.
- `CSRF` — на все non-GET.
- `RateLimit` — на /auth.

### Безопасность HTTP
- `helmet`-style заголовки выставляет nginx (см. раздел 6), но Go тоже шлёт `X-Content-Type-Options: nosniff`.
- Все JSON-ответы. Никаких HTML-инжекций.
- Все SQL — через параметризованные запросы pgx. Никакой конкатенации строк.

---

## 4. Интеграция с Продамусом

**Документация:** https://help.prodamus.ru/payform/integracii/rest-api

### Создание ссылки на оплату
Когда юзер жмёт «Купить курс» → `POST /api/courses/:slug/checkout`:
1. Проверяем что юзер залогинен и email верифицирован.
2. Если уже есть enrollment — 400 «Уже куплено».
3. Создаём `order` со статусом `pending`, генерим `order_num`.
4. Формируем ссылку на платёжную страницу Продамуса (домен из env `PRODAMUS_PAYFORM_URL`, например `https://leshalarin.payform.ru`):

   Параметры в query string (URL-encoded):
   - `do=pay`
   - `order_id={order.id}` *(наш UUID)*
   - `order_num={order.order_num}`
   - `customer_email={user.email}`
   - `customer_phone=` *(если знаем)*
   - `products[0][name]={course.title}`
   - `products[0][price]={course.price_rub}`
   - `products[0][quantity]=1`
   - `urlReturn=https://{HOST}/cabinet/courses` *(куда вернуть после успешной)*
   - `urlSuccess=https://{HOST}/cabinet/courses?paid={course.slug}`
   - `urlNotification=https://{HOST}/api/webhooks/prodamus`
   - `npd_income_type=` если самозанятый, см. документацию

5. Все параметры (кроме `signature`) подписываем HMAC-SHA256 c `PRODAMUS_SECRET_KEY` по их алгоритму:
   - Привести значения к строкам.
   - Рекурсивно отсортировать ключи (`ksort` рекурсивно).
   - Закодировать в JSON с флагами `JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES` (в Go: `json.Marshal` с явной обработкой — Go не эскейпит unicode по умолчанию, но эскейпит `<>&` — отключи через `Encoder.SetEscapeHTML(false)`).
   - HMAC-SHA256 → hex.
   - Добавить как `signature=...`.
6. Возвращаем фронту `{ payment_url: "..." }`.

### Webhook `/api/webhooks/prodamus`
Прилетает `application/x-www-form-urlencoded` (или JSON — поддержи оба). Заголовок `Sign` содержит подпись.

**Шаги обработки:**
1. Прочитать сырое тело **до** парсинга. Сохранить запись в `payment_webhooks` (raw).
2. Достать заголовок `Sign`. Если нет — 400.
3. Распарсить тело. Удалить из map ключ `signature`/`sign` если он там есть.
4. Воспроизвести алгоритм подписи (тот же что в п.5 выше) и сравнить (constant-time `subtle.ConstantTimeCompare`).
5. Если подпись неверна → пометить webhook `signature_valid=false`, вернуть 200 с пустым телом (чтобы Продамус не ретраил), но **не обрабатывать**. Залогировать как security event.
6. Если подпись верна:
   - Найти `order` по `order_id` (наш UUID, мы его передавали) ИЛИ по `order_num`.
   - Если `payment_status == 'success'` и order еще `pending`:
     - Транзакция: `order.status = 'paid'`, `order.paid_at = now()`, создать `enrollment(user_id=order.user_id, course_id=order.course_id, granted_by='purchase')`.
     - Послать письмо «Доступ к курсу открыт».
   - Если `payment_status == 'success'` и order уже `paid` → идемпотентно, ничего не делать (Продамус ретраит).
   - Любые другие статусы → залогировать, не падать.
7. Вернуть 200 OK всегда после успешной валидации подписи (даже если бизнес-логика отказала — иначе будут бесконечные ретраи).

### Тестовый режим
Добавь env-флаг `PRODAMUS_TEST_MODE=true`. В этом режиме `/api/courses/:slug/checkout` возвращает специальный URL `/api/dev/fake-payment?order_id=...` который сразу триггерит вебхук с правильной подписью (только когда `APP_ENV != 'production'`). Это для локальной разработки.

---

## 5. Хранение и отдача видео (HLS + nginx X-Accel-Redirect)

### Загрузка
1. `POST /api/admin/videos/upload` — multipart, лимит 5 ГБ. Стримим в файл `/var/videos/{video_id}/source.mp4` (внутри контейнера это `/var/videos`, на хосте — `/srv/leshalarin/videos`, через bind mount).
2. После загрузки — статус `processing`. Запускаем горутину (или отдельный воркер, если масштабируется) которая через `ffmpeg` нарезает на HLS:
   ```
   ffmpeg -i source.mp4 \
     -codec:v libx264 -codec:a aac \
     -hls_time 10 -hls_playlist_type vod \
     -hls_segment_filename "/var/videos/{id}/seg_%03d.ts" \
     -f hls /var/videos/{id}/index.m3u8
   ```
   Опционально — несколько качеств (480p/720p/1080p) и master playlist.
3. По окончании — статус `ready`, `duration_sec`, `size_bytes`, `hls_master_playlist`.
4. ffmpeg должен быть установлен в Docker-образ `api`.

### Защита и отдача
**Не отдавай видео напрямую.** Используй паттерн «signed URL → X-Accel-Redirect»:

1. Когда юзер открывает урок, фронт запрашивает `GET /api/videos/:id/playback`.
2. Backend проверяет enrollment, генерирует **временный токен** (JWT, 2 часа, claim `video_id`, `user_id`, `ip` для биндинга по желанию). Возвращает `{ playback_url: "/video-stream/{video_id}/index.m3u8?token=..." }`.
3. nginx ловит `/video-stream/`, проксирует на go-api эндпоинт `/api/internal/video-auth?path=...&token=...` через `auth_request`. Go валидирует токен → возвращает 200 с заголовком `X-Accel-Redirect: /protected-videos/{video_id}/index.m3u8` и `Content-Type`.
4. nginx ловит `/protected-videos/` (location `internal`), отдаёт файл напрямую с диска.

```nginx
location /video-stream/ {
    auth_request /_video_auth;
    auth_request_set $video_path $upstream_http_x_video_path;
    proxy_pass http://localhost; # never reached, X-Accel-Redirect перехватит
}

location = /_video_auth {
    internal;
    proxy_pass http://api:8080/api/internal/video-auth;
    proxy_pass_request_body off;
    proxy_set_header Content-Length "";
    proxy_set_header X-Original-URI $request_uri;
}

location /protected-videos/ {
    internal;
    alias /var/videos/;
    add_header Cache-Control "no-store";
}
```

Так Go не качает байты видео — только проверяет права. nginx тянет с диска быстро.

### Плеер на фронте
- `hls.js` для всех браузеров кроме Safari (там нативный HLS).
- Записываем `last_position_sec` раз в 10 секунд через `POST /api/lessons/:id/progress`.
- При >90% просмотра — отмечаем `completed_at`.
- Базовая защита от копирования — не нужно сходить с ума: HLS-токен с TTL и привязкой к user_id уже отсекает 95% случаев. Если хочешь strict — добавь дополнительно ватермарку с email пользователя поверх плеера (CSS overlay, можно убрать через DevTools, но это уже усложняет).

---

## 6. Frontend (Astro)

### Структура
```
web/
  src/
    pages/
      index.astro                 — лендинг (перенос текущего HTML)
      courses/
        index.astro                — каталог
        [slug].astro               — страница курса
        [slug]/lessons/[lesson].astro
      cabinet/
        index.astro                — обзор + мои курсы
        courses.astro
        settings.astro
      admin/
        index.astro
        users/...
        courses/...
        orders.astro
      auth/
        login.astro
        register.astro
        verify.astro
        forgot.astro
    components/
      Nav.astro, Footer.astro, CourseCard.astro, VideoPlayer.astro (island, React/Solid),
      LessonSidebar.astro, AdminTable.astro
    layouts/
      Public.astro, Cabinet.astro, Admin.astro
    lib/
      api.ts          — fetch wrapper, добавляет CSRF, обрабатывает 401 → refresh → retry
      auth.ts         — server-side проверка cookie в Astro middleware
    middleware.ts     — глобальный middleware: проверка auth для /cabinet/* и /admin/*
  astro.config.mjs    — output: 'server', adapter: node
  package.json
```

### Дизайн-система
Бери из текущего HTML:
- Шрифты: Manrope (sans), Playfair Display (serif для заголовков).
- Палитра: navy `#1a2744`, navy-light `#243058`, orange `#e8652a`, orange-light `#f07840`, blue `#2b5fa8`, cream `#f9f7f3`, warm-white `#fefcf8`.
- Стиль кнопок, карточек, секций — как в лендинге.
- Tailwind с кастомным config, либо CSS variables как сейчас. Выбери одно.

### Главная
Перенеси существующий HTML 1:1 как `index.astro`. Удали edit-mode скрипт (`#edit-bar`). Линки «Личный кабинет» / «Войти» в навигации должны менять текст в зависимости от состояния auth (через middleware подкладываем `Astro.locals.user`).

### Каталог курсов `/courses`
- Сетка карточек. Карточка показывает: обложку, название, описание, тип (бесплатный/платный + цена). Если у юзера уже есть enrollment — бейдж «Куплено» / «Открыт».
- Клик → `/courses/:slug`.

### Страница курса
- Hero с обложкой, описанием, кнопкой:
  - не залогинен → «Войти и купить» / «Войти и начать» (для бесплатных).
  - залогинен, нет enrollment, free → «Начать бесплатно» (мгновенно создаёт enrollment).
  - залогинен, нет enrollment, paid → «Купить за {price} ₽» → `POST /checkout` → редирект на Продамус.
  - залогинен, есть enrollment → «Продолжить обучение» (на последний незавершённый урок).
- Структура программы: модули → уроки. Закрытые уроки показаны заблокированными (иконка замка) если нет enrollment.

### Урок `/courses/:slug/lessons/:lesson_slug`
- Layout: слева sidebar со списком всех уроков курса (текущий выделен), справа контент.
- Сверху видео-плеер (если есть `video_id`), ниже markdown-контент урока.
- Внизу кнопка «Отметить как пройденный» + «Следующий урок».

### Личный кабинет `/cabinet`
- `/cabinet` — приветствие + блок «Продолжить обучение» (последний урок, на котором остановился) + список моих курсов с прогресс-барами.
- `/cabinet/courses` — то же что выше, но полнее.
- `/cabinet/settings` — имя, смена пароля, email (только просмотр).

### Админка `/admin`
- `/admin` — дашборд: total users, online now (live, обновляется раз в 30 сек), revenue today/week/month, последние оплаты.
- `/admin/users` — таблица с поиском, пагинацией. Клик по юзеру → детали + кнопки «Выдать доступ к курсу» (модалка с селектом курсов), «Отозвать», список оплат.
- `/admin/courses` — список + кнопка «Создать». Редактор курса: title, slug, kind, price, обложка, флаг publish.
- `/admin/courses/:id/edit` — управление модулями и уроками, drag-n-drop сортировка (sortablejs или dnd-kit). Кнопка «Загрузить видео» в каждом уроке → multipart upload с прогресс-баром, после загрузки показываем статус processing → ready.
- `/admin/orders` — таблица всех заказов с фильтрами по статусу и датам.
- `/admin/online` — список юзеров с `last_seen_at < 5 мин`.

### Auth state
- В Astro middleware читаем `access_token` cookie, валидируем через `/api/me`. Кладём user в `Astro.locals`.
- На клиенте (для интерактивных кусков) держим минимальное состояние (id, role) в обычной cookie (не httpOnly, без секретов) — чтобы UI мог сразу показать «вошёл/нет» без ожидания сети.

### Защита роутов
- Astro middleware: `/cabinet/*` → если нет user → redirect на `/auth/login?next=...`.
- `/admin/*` → если `user.role !== 'admin'` → 404.
- API сам тоже проверяет — middleware Astro это только UX, не security.

---

## 7. nginx

`nginx/nginx.conf`:

```nginx
server {
    listen 443 ssl http2;
    server_name leshalarin.ru;

    ssl_certificate     /etc/letsencrypt/live/leshalarin.ru/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/leshalarin.ru/privkey.pem;

    # security headers
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "DENY" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header Content-Security-Policy "default-src 'self'; img-src 'self' data: https:; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; script-src 'self'; media-src 'self' blob:; connect-src 'self'" always;
    add_header Permissions-Policy "geolocation=(), microphone=(), camera=()" always;

    client_max_body_size 5G;  # для загрузки видео

    location /api/ {
        proxy_pass http://api:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_request_buffering off;  # для streaming uploads
    }

    location /video-stream/ { ... см. раздел 5 ... }
    location /protected-videos/ { internal; alias /var/videos/; }

    location / {
        proxy_pass http://web:4321;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}

server {
    listen 80;
    server_name leshalarin.ru;
    return 301 https://$host$request_uri;
}
```

---

## 8. Email

SMTP (Yandex 360 / Mail.ru / любой). Шаблоны (HTML + plain text):
- Подтверждение email.
- Сброс пароля.
- Доступ к курсу открыт после оплаты.
- (опц) Welcome после регистрации.

Использовать `gomail` или stdlib `net/smtp`. Все письма — асинхронно через очередь в памяти (канал + воркер), чтобы webhook не висел.

---

## 9. Безопасность — чек-лист

- [ ] HTTPS-only, HSTS.
- [ ] Cookies: `Secure`, `HttpOnly`, `SameSite=Lax` для refresh; CSRF-cookie без HttpOnly.
- [ ] CSP без `unsafe-inline` для `script-src` (для стилей пока разрешим).
- [ ] CSRF double-submit на все мутации.
- [ ] Argon2id для паролей.
- [ ] Rate limit на /auth/*.
- [ ] Email verification обязателен для покупки.
- [ ] Все SQL — параметризованные.
- [ ] Webhook Продамуса — обязательная HMAC-проверка через `subtle.ConstantTimeCompare`, идемпотентность по `order_id`.
- [ ] Видео — только через signed URL + nginx internal location.
- [ ] Логи без секретов (никогда не логируй headers целиком, password, token).
- [ ] Бэкапы:
    - **Postgres:** `pg_dump` ежедневно по cron внутри контейнера postgres → `/backups/dump-YYYY-MM-DD.sql.gz` (это `/srv/leshalarin/backups/` на хосте).
    - **Видео и uploads:** `rsync /srv/leshalarin/videos/ /srv/leshalarin/uploads/` на внешний VPS / S3 раз в сутки. Поскольку это bind mount, бэкап не требует знания Docker — обычный `rsync` от имени root.
    - **Bind mount = можно бэкапить хоть холодно (`tar` всю `/srv/leshalarin/` после `docker compose down`), хоть горячо (rsync + pg_dump).**
    - **Бэкап ДО запуска в проде.** Проверь восстановление на тестовой машине ДО того как пустишь живых юзеров.
- [ ] Контейнеры под non-root user.
- [ ] `fail2ban` на хосте для ssh.
- [ ] `ufw`: открыты только 22 (с whitelist IP если возможно), 80, 443.
- [ ] Регулярный `apt upgrade` или unattended-upgrades.
- [ ] Логи ротейтятся (`logrotate` или Docker log driver).
- [ ] `.env` — `chmod 600`, не в git.

---

## 10. Тесты

- Go: `go test ./...`. Минимум — unit на `prodamus.VerifySignature` (с реальным фикстурным телом + подписью), на `auth.HashPassword`/`VerifyPassword`, на `services.EnrollmentService.GrantAccess` (идемпотентность).
- Integration: testcontainers-go запускает Postgres, прогоняем миграции, тестируем хэндлеры через `httptest`.
- E2E (опционально): Playwright на критические пути — регистрация → логин → покупка курса (mock webhook) → доступ к видео.

---

## 11. Деплой и эксплуатация

### Локальная разработка
- `make up` → `docker compose up -d`.
- `make migrate` → применить миграции.
- `make seed` → создать админа из env (`ADMIN_EMAIL`, `ADMIN_PASSWORD`) и пару тестовых курсов.
- `make logs`, `make down`, `make psql`.

### Прод
- VPS с Ubuntu 22.04+, минимум 4 ГБ RAM (ffmpeg прожорлив), 80 ГБ диск (видео).
- `useradd -u 1000 -m app` + `mkdir -p /srv/leshalarin/{pgdata,videos,uploads,backups}` + `chown -R 1000:1000 /srv/leshalarin/` (для pgdata — отдельно, под UID postgres-образа).
- `git pull && docker compose build && docker compose up -d`.
- `certbot` для TLS, обновление по cron.
- `systemd`-таймер для `pg_dump` ежедневно + `rsync` `/srv/leshalarin/videos` на внешний бэкап раз в сутки.
- Мониторинг: для старта хватит `docker stats` и uptime-проверки через cron+curl. Можно добавить `uptime-kuma` отдельным контейнером.
- **Перенос на другой VPS:** `rsync -a /srv/leshalarin/ user@new-host:/srv/leshalarin/` + `git clone` + `docker compose up -d`. Всё.

### .env.example
```
APP_ENV=development
APP_HOST=https://leshalarin.local
SESSION_SECRET=change-me-32-bytes
JWT_SECRET=change-me-32-bytes

# Корень для всех bind mount. dev: ./data, prod: /srv/leshalarin
DATA_DIR=./data

DATABASE_URL=postgres://app:app@postgres:5432/app?sslmode=disable

SMTP_HOST=
SMTP_PORT=465
SMTP_USER=
SMTP_PASSWORD=
SMTP_FROM="Алексей Ларин <noreply@leshalarin.ru>"

PRODAMUS_PAYFORM_URL=https://leshalarin.payform.ru
PRODAMUS_SECRET_KEY=
PRODAMUS_TEST_MODE=true

VIDEO_TOKEN_SECRET=change-me-32-bytes
VIDEO_TOKEN_TTL=2h
VIDEO_STORAGE_PATH=/var/videos

ADMIN_EMAIL=
ADMIN_PASSWORD=
```

---

## 12. Порядок работы для Claude Code

Делай по этапам, после каждого — коммить и проверяй что собирается и работает. **Не лети вперёд, не запустив предыдущее.**

1. **Каркас:** монорепо, `docker-compose.yml`, пустой Astro, пустой Go с `/api/health`, postgres, nginx. Поднимается одной командой.
2. **Миграции и схема БД** (раздел 2 целиком).
3. **Auth:** регистрация, верификация, логин, refresh, logout. CSRF. Rate limit. Astro-страницы login/register. Middleware. Тесты.
4. **Profile + admin seed:** `/me`, страница `/cabinet`, скрипт создания админа из env.
5. **Курсы (CRUD в админке + публичная выдача):** модели, эндпоинты, страницы каталога и курса. Без видео и оплат.
6. **Бесплатные курсы end-to-end:** запись на free-курс одним кликом, прогресс уроков, страница урока с markdown-контентом.
7. **Видео:** загрузка в админке, ffmpeg-конвертация, signed URL, nginx X-Accel-Redirect, плеер на hls.js. Сохранение прогресса.
8. **Платные курсы и Продамус:** генерация ссылки оплаты, обработка webhook, выдача enrollment. Тестируй через `PRODAMUS_TEST_MODE=true`.
9. **Админка остальное:** users, выдача/отзыв доступа, orders, online, audit log, dashboard.
10. **Шлифовка:** письма, security headers, бэкап-скрипт, README с инструкцией деплоя.

---

## 13. Что НЕ делать

- Не использовать ORM (`gorm`/`ent`) — в этом проекте оверкилл, делай через pgx + sqlc, либо просто pgx с ручными запросами.
- Не хранить токены в `localStorage`.
- Не делать собственную крипту. argon2id, JWT-библиотеки, HMAC из stdlib.
- Не отдавать видео напрямую через `proxy_pass` или `sendfile` без auth_request.
- Не удалять записи `users`, `orders`, `payment_webhooks` — только soft-delete (отдельная колонка `deleted_at` на users если понадобится).
- Не доверять `customer_email` из вебхука для матчинга — сматчивай по `order_id`/`order_num` который мы сами создавали.
- Не хранить `prodamus_secret_key` в коде / в БД — только env.

---

## 14. Что отдать в README.md

- Краткое описание проекта.
- Quickstart: clone → `cp .env.example .env` → `make up` → `make migrate` → `make seed` → открыть http://localhost.
- Список всех env-переменных с пояснениями.
- Как настроить Продамус (где взять секретный ключ, какой URL уведомлений указать).
- Как добавить SSL в проде.
- Как создать бэкап и как восстановиться.
- Структура каталогов.

---

**Критерий готовности проекта:** новый юзер может зарегистрироваться → подтвердить email → купить курс через Продамус → получить доступ → смотреть видео с защитой → отмечать прогресс. Админ может создать курс → загрузить видео → опубликовать → видеть оплаты и юзеров онлайн в дашборде. Всё это — через `docker compose up` на чистом VPS.
