# API контракт

Все эндпоинты под `/api`. JSON в обе стороны. Реализация — `chi` router в `api/cmd/server/main.go`, хендлеры — `api/internal/handlers/`.

## Аутентификация и сессии

- **Access token**: JWT HS256, TTL 15 минут. Кладётся в `access_token` httpOnly cookie на пути `/`.
- **Refresh token**: 32 случайных байта в hex, TTL 30 дней. Хранится в БД как SHA256-хеш в `sessions`. В `refresh_token` httpOnly cookie на `/api/auth`.
- **Auth UI hint**: ненадёжный нечувствительный cookie `auth=1` (не httpOnly) — фронт читает чтобы сразу показать «вошёл/нет» без сетевого запроса. Не используется как авторизация.
- Заголовок `Authorization: Bearer <token>` тоже работает (для curl/тестов).
- **Secure-флаг**: все cookies (`access_token`, `refresh_token`, `auth`, `csrf`) автоматически получают `Secure`, если запрос пришёл по HTTPS — напрямую (`r.TLS != nil`) или через reverse-proxy (заголовок `X-Forwarded-Proto: https`, который выставляет nginx). На HTTP — Secure не ставится, иначе браузер бы отбросил cookie. Хелпер: `handlers.IsHTTPSRequest(r)`.

## CSRF

Double-submit cookie. На любом GET к `/api/...` middleware ставит `csrf=<random>` cookie (НЕ httpOnly). Для всех не-GET (`POST/PATCH/PUT/DELETE`) клиент обязан слать заголовок `X-CSRF-Token` с тем же значением. Иначе — 403 `csrf_failed`.

Исключения: `/api/webhooks/*`, `/api/internal/*`. Фронт-обёртка `fetch` в `Base.astro` подкладывает токен автоматически.

## Rate limiting

In-memory token bucket (`api/internal/middleware/middleware.go`): 20 запросов / 15 мин на (IP, путь) для всех эндпоинтов под `/api/auth/*`. Превышение → 429.

## Public

| Метод/Путь                              | Описание                                       |
|----------------------------------------|------------------------------------------------|
| `GET  /api/health`                     | health-check.                                  |
| `POST /api/auth/register`              | `{email, password, name, consent_pd, consent_marketing?}` → юзер + email-токен. **Без `consent_pd=true` → 400 `consent_pd_required`** (152-ФЗ). Время согласий пишется в `users.consent_pd_at` и `consent_marketing_at`. В dev в ответе есть `verify_link_dev`. |
| `POST /api/auth/quick-signup`          | `{email, name?, phone?, consent_pd, consent_marketing?}` — одношаговая регистрация с лендинга. Без `consent_pd=true` → 400 `consent_pd_required`. Если email уже есть → `{exists:true}` (200). Если нет → создаёт юзера, генерит случайный пароль, шлёт его на почту, ставит `email_verified_at=now()`, фиксирует согласия, **автоматически записывает на ВСЕ опубликованные free-курсы** (`granted_by='free'`), выдаёт auth-cookies → `{created:true, id, email, enrolled_free}` (201). В dev добавляет `password_dev`. |
| `POST /api/auth/login`                 | `{email, password}` → ставит cookies.          |
| `POST /api/auth/logout`                | отзывает refresh, чистит cookies.              |
| `POST /api/auth/refresh`               | по refresh-cookie выдаёт новый access.         |
| `POST /api/auth/verify-email`          | `{token}` → подтверждает email.                |
| `POST /api/auth/forgot-password`       | `{email}` → создаёт reset-токен, шлёт письмо.  |
| `POST /api/auth/reset-password`        | `{token, password}`.                           |
| `GET  /api/courses`                    | Список **опубликованных** курсов. Если есть auth — добавляет `enrolled` для каждого. |
| `GET  /api/courses/{slug}`             | Курс + модули + уроки. У платного без enrollment контент уроков (`content_md`, `video_id`) вычищен (кроме `is_preview`). |
| `GET  /api/courses/{slug}/lessons/{lesson_slug}` | Урок. Для платного без enrollment и не-preview → 403. |

## Authenticated user

| Метод/Путь                                      | Описание |
|------------------------------------------------|----------|
| `GET  /api/me`                                 | Профиль. |
| `PATCH /api/me`                                | `{name?, old_password?, new_password?}`. |
| `GET  /api/me/courses`                         | Мои курсы с прогрессом (`progress_pct`, `lessons_done/total`). |
| `POST /api/courses/{slug}/enroll-free`         | Мгновенная запись на бесплатный курс. |
| `POST /api/courses/{slug}/checkout[?tariff=]`  | Создаёт `order` (status=`pending`). Возвращает `{payment_url, order_id}`. В test mode (`PRODAMUS_TEST_MODE=true`) возвращает локальный `/api/dev/fake-payment?order_id=...`. Требует `email_verified`. Для курсов с несколькими тарифами (сейчас только `zdorovaya-spina`: `tariff=self` 3990 ₽ / `tariff=support` 12990 ₽) параметр `tariff` обязателен; без него → 400 `tariff_required`, с неизвестным значением → 400 `bad_tariff`. Цена и имя товара в payform берутся из пресета (`tariffPresets` в `handlers/payments.go`), не из `courses.price_rub`. Служебные тарифы из `adminOnlyTariffs` (сейчас `test10`, 10 ₽) доступны только пользователям с ролью `admin` — остальным → 400 `bad_tariff`. |
| `GET  /api/courses/{slug}/files/{name}`        | Защищённая отдача PDF-материалов курса (`metodichka.pdf`, `kalendar.pdf`, `zamery.pdf`). Доступ только при наличии enrollment у текущего пользователя. Файлы embed-ятся в бинарь из `api/internal/assets/courses/{slug}/` (см. `handlers/course_files.go`). Белый список slug → файлов жёстко зашит в коде ради защиты от path traversal и случайной публикации лишнего. |
| `POST /api/lessons/{id}/progress`              | `{completed: bool, last_position_sec: int}` upsert. |
| `GET  /api/videos/{id}/playback`               | Возвращает `{playback_url}` со встроенным signed JWT (TTL `VIDEO_TOKEN_TTL`, по умолчанию 2 часа). |

## Webhooks / internal

| Метод/Путь                       | Описание |
|---------------------------------|----------|
| `POST /api/webhooks/prodamus`   | Без auth, без CSRF. Логирует raw body, проверяет HMAC через `subtle.ConstantTimeCompare`, идемпотентен по `order_id`. Подробнее — `docs/05-payments-video.md`. |
| `GET  /api/dev/fake-payment`    | Только при `APP_ENV != production`. Эмулирует успешный вебхук. |
| `GET  /api/internal/video-auth` | Вызывается nginx через `proxy_pass`. Валидирует video JWT, возвращает `X-Accel-Redirect`. |

## Admin (`role=admin`)

| Метод/Путь                                          | Описание |
|----------------------------------------------------|----------|
| `GET  /api/admin/stats`                            | users_total, online (5 мин), revenue_day/week/month (₽). |
| `GET  /api/admin/users?search=&page=`              | Поиск + пагинация по 50. |
| `GET  /api/admin/users/{id}`                       | Юзер + enrollments + orders. |
| `POST /api/admin/users/{id}/enrollments`           | `{course_id}` — выдать доступ (`granted_by='admin'`). |
| `DELETE /api/admin/users/{id}/enrollments/{course_id}` | Отозвать. |
| `GET  /api/admin/courses`                          | Все курсы (включая неопубликованные). |
| `POST /api/admin/courses`                          | Создать. Поля по `db.CourseInput`. |
| `GET  /api/admin/courses/{id}`                     | Полный курс + все модули + все уроки (без фильтрации по `is_published` и без вычистки контента). Используется редактором админки. |
| `PATCH /api/admin/courses/{id}`                    | Обновить. |
| `DELETE /api/admin/courses/{id}`                   | Удалить (cascade). |
| `POST /api/admin/modules`                          | `{course_id, title, sort_order}` — создать модуль. |
| `PATCH /api/admin/modules/{id}`                    | `{title, sort_order}` — переименовать/переставить модуль. Используется UI «вверх/вниз» в админке. |
| `DELETE /api/admin/modules/{id}`                   | Удалить модуль (FK у lessons.module_id `ON DELETE SET NULL` — уроки сохранятся). |
| `POST /api/admin/lessons`                          | Создать урок (см. `db.LessonInput`). |
| `GET  /api/admin/lessons/{id}`                     | Урок + связанное видео (если есть). |
| `PATCH /api/admin/lessons/{id}`                    | Обновить. |
| `DELETE /api/admin/lessons/{id}`                   | Удалить. |
| `POST /api/admin/videos/upload`                    | multipart `file`. Возвращает `{id, status:processing}`, ffmpeg в фоне нарезает HLS. |
| `GET  /api/admin/orders?status=&from=&to=`         | Список заказов. |
| `GET  /api/admin/online-users`                     | last_seen_at < 5 мин. |
| `GET  /api/admin/audit-log`                        | Последние 200 записей `audit_log`. |
| `GET  /api/admin/articles`                         | Все статьи (включая неопубликованные). |
| `POST /api/admin/articles`                         | Создать статью. Обязательно `slug`+`title`. Поля: `slug, title, tag, excerpt, cover_image_url, content_html, reading_minutes, is_published, published_at, sort_order`. Если `is_published=true` и `published_at` пуст — заполняется текущим временем. |
| `GET  /api/admin/articles/{id}`                    | Статья по id (полный объект). |
| `PATCH /api/admin/articles/{id}`                   | Обновить статью. |
| `DELETE /api/admin/articles/{id}`                  | Удалить статью. |

### Публичные эндпоинты блога

| Метод и путь                                       | Что делает |
|----------------------------------------------------|------------|
| `GET  /api/articles?limit=&offset=`                | Список **только опубликованных** статей. `content_html` не возвращается (для производительности списка). По умолчанию `limit=50`. |
| `GET  /api/articles/{slug}`                        | Опубликованная статья по slug. Если не опубликована — 404. |

Каждое write-действие админа пишется в `audit_log` через `repo.Audit(...)`.

## Коды ошибок

JSON `{"error": "<code>"}`:
- `unauthorized` — нет/просрочен access токен.
- `forbidden` — не админ или нет enrollment.
- `csrf_failed` — нет/не совпадает X-CSRF-Token.
- `rate_limited` — 429.
- `invalid_credentials`, `email_taken`, `email_not_verified`, `already_enrolled`, `weak_password`, `invalid_token`, `bad_json`, `bad_id`, `not_found`, `db`.

## Заявки (leads)

| Эндпоинт | Описание |
|---|---|
| `POST /api/leads` | Публичный. `{name, contact, message?, source, consent_pd}` → 201. Валидация: имя и контакт (≥5 симв.) обязательны, `consent_pd` обязателен (400 `consent_pd_required`), лимиты длины (400 `too_long`). Неизвестный `source` → `other`. Rate-limit 10/15мин на IP, CSRF обязателен. Шлёт письмо на `LEAD_NOTIFY_EMAIL`. |
| `GET /api/admin/leads` | Admin. Последние 500 заявок, новые сверху. |
| `PATCH /api/admin/leads/{id}` | Admin. `{status: new|in_progress|done}` → 200; иное → 400 `bad_status`. Пишет `audit_log` (`lead_status`). |

| `GET /api/admin/users/{id}` | Карточка пользователя: профиль+согласия, `courses[]` с прогрессом и поурочной детализацией, `orders[]`. |

| `GET /api/admin/users` | Ответ: `{users, total, page, per_page}` (точное количество + пагинация по 50). Фильтры: `?search=`, `?course=<slug>` (только с доступом к курсу), `?verified=1|0`, `?sort=last_seen`, `?page=`. |

| `GET /api/admin/users/export.csv` | CSV-выгрузка для сервиса рассылок: ТОЛЬКО пользователи с `consent_marketing_at NOT NULL`. Колонки: email, name, phone, registered_at, courses. Фильтры `?search/?course/?verified` как у списка. UTF-8 BOM. Факт выгрузки пишется в audit_log (`users_export_csv`). |

| `GET /api/admin/activity` | Журнал занятий: последние касания уроков (user+lesson, новые сверху). `?course=<slug>` — фильтр, `?limit=` (по умолчанию 200, максимум 500). Строка: время, пользователь, курс, урок, completed_at/позиция. |
