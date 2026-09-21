# База данных

PostgreSQL 16. UUID везде (`gen_random_uuid()` через `pgcrypto`), email — `CITEXT`, таймстемпы — `TIMESTAMPTZ`. Миграции — `golang-migrate`, файлы в `api/migrations/*.up.sql` / `*.down.sql`. Текущие миграции: `001_init`, `002_articles`, `003_users_phone`, `004_users_consent`, `005_user_logs_set_null`, `009_leads`, `010_lesson_activity`, `011_site_settings`, `006_zdorovaya_spina_v2` (чистит старую структуру `zdorovaya-spina` — 11 уроков / 4 модуля — чтобы сидер пересоздал 12 уроков в 5 модулях с новыми slug-ами).

## Таблицы

| Таблица                       | Назначение                                             |
|-------------------------------|--------------------------------------------------------|
| `users`                       | Юзеры. Поля: email, argon2id-хеш, role (`user`/`admin`), `email_verified_at`, `last_seen_at`, `phone` (миграция 003), `consent_pd_at` (152-ФЗ, обязательное, миграция 004), `consent_marketing_at` (отдельное согласие на рассылки). |
| `email_verification_tokens`   | Токены подтверждения email (hash + expires).           |
| `password_reset_tokens`       | Токены сброса пароля.                                  |
| `sessions`                    | Refresh-токены (хеш + UA + IP + expires + revoked).    |
| `courses`                     | Курсы. `kind in ('free','paid')`, `price_rub`, `is_published`. |
| `modules`                     | Модули внутри платных курсов.                          |
| `lessons`                     | Уроки. `is_preview` для пробного, `video_id` ссылка на `videos`. |
| `videos`                      | Видеофайлы. `status in ('uploading','processing','ready','failed')`. |
| `enrollments`                 | Доступ юзера к курсу. `granted_by in ('purchase','admin','free')`. |
| `lesson_progress`             | Прогресс по уроку (`completed_at`, `last_position_sec`). |
| `orders`                      | Заказы. `order_num BIGSERIAL` для человекочитаемого №. `status in ('pending','paid','failed','refunded')`. |
| `payment_webhooks`            | Сырой лог входящих вебхуков Продамуса (для аудита).    |
| `video_access_log`            | Лог доступа к видео (для аналитики).                   |
| `audit_log`                   | Действия в админке.                                    |
| `articles`                    | Статьи блога. Поля: `slug` (уникальный), `title`, `tag`, `excerpt`, `cover_image_url`, `content_html`, `reading_minutes`, `is_published`, `published_at`, `sort_order`, `author_id`. |
| `site_settings`               | Key-value флаги сайта, переключаемые из админки (миграция 011). |
| `email_opens`                 | Открытия писем рассылки по пикселю (миграция 012). |
| `campaigns`                   | Рассылки из админки: тема, текст, группа, темп, статус (миграция 013). |
| `campaign_recipients`         | Очередь отправки: кто получил письмо, когда, с какой ошибкой (миграция 013). |

## Ключевые индексы

- `users(last_seen_at)` — для «онлайн сейчас».
- `enrollments(user_id)`, `enrollments(course_id)` — лукапы доступа.
- `lessons(course_id, sort_order)` — отображение программы.
- `orders(status, created_at)` — фильтры в админке.
- Уникальные: `(user_id, course_id)` на enrollments, `(user_id, lesson_id)` на progress, `(course_id, slug)` на lessons, `slug` на articles.
- `articles(is_published, published_at DESC)` — сортировка ленты блога.

## Soft delete vs hard delete

- `users`, `orders`, `payment_webhooks` **не удалять** (для аудита). Если понадобится — добавь колонку `deleted_at`.
- `courses`, `lessons`, `modules`, `videos`, `articles` — можно `DELETE` (cascade удаляет дочерние записи).

## Как добавить миграцию

1. Создай два файла: `api/migrations/00X_<name>.up.sql` и `.down.sql`.
2. `make down && make up` (или `make migrate`) — миграции применяются автоматически из контейнера `migrate`.
3. **Тестовая БД**: тестовый стек применяет миграцию из `001_init.up.sql` через `docker-entrypoint-initdb.d`. Если добавляешь новую — обнови volume mapping в `docker-compose.test.yml` ИЛИ замени схему теста на прогон migrate-контейнера.

## Доступ к БД

- `make psql` — psql внутри контейнера.
- Из приложения: pgxpool, репозитории в `api/internal/db/queries.go`.
- **Все запросы — параметризованные.** Конкатенация строк в SQL запрещена (см. CLAUDE.md).

## leads (миграция 009)

Заявки с маркетинговых страниц (`/coaching`, `/start`).

| Колонка | Тип | Комментарий |
|---|---|---|
| id | UUID PK | |
| name | TEXT | имя |
| contact | TEXT | email / телефон / @telegram — одно поле, как удобно человеку |
| message | TEXT | необязательное сообщение |
| source | TEXT | `coaching` / `start` / `consultation` (архив) / `other` (неизвестные значения нормализуются) |
| status | TEXT | `new` / `in_progress` / `done` (CHECK) |
| consent_pd | BOOLEAN | факт согласия на обработку ПД (152-ФЗ) |
| created_at | TIMESTAMPTZ | индекс по убыванию |

Записи не удаляются (обращения с ПД), обработка — сменой `status`.

## lesson_activity (миграция 010)

Журнал просмотров уроков: одна строка = одна «сессия». Повторные просмотры — отдельные записи (перерыв > 30 минут = новая сессия, логика в `Repo.TouchLessonActivity`). Заполняется из `PostProgress` (best-effort). Миграция переносит существующее состояние из `lesson_progress` как стартовые сессии.

| Колонка | Тип | Комментарий |
|---|---|---|
| id | UUID PK | |
| user_id / lesson_id | UUID FK | каскадное удаление |
| started_at / updated_at | TIMESTAMPTZ | начало и последнее касание сессии |
| max_position_sec | INTEGER | максимум досмотра в сессии |
| completed | BOOLEAN | урок досмотрен в этой сессии |

## site_settings (миграция 011)

Настройки сайта, которые Алексей переключает из админки (`/admin/settings`) без деплоя.

| Колонка | Тип | Комментарий |
|---|---|---|
| key | TEXT PK | ключ настройки |
| value | TEXT | значение строкой (`'true'` / `'false'` для флагов) |
| updated_at | TIMESTAMPTZ | время последнего изменения |

**Белый список ключей и дефолты — в коде**, `db.SettingDefaults` (`api/internal/db/queries.go`). Строки БД накладываются поверх дефолтов, неизвестные ключи игнорируются на чтении и отклоняются на записи (400 `unknown_key`). Новый флаг достаточно добавить в `SettingDefaults` — миграция нужна только для сид-значения.

Текущие ключи:

| Ключ | Дефолт | Что делает |
|---|---|---|
| `salut_visible` | `false` | Показывать страницу «Здоровая спина в Салюте»: кнопка в шапке, строка в sitemap, индексация. Выключено — страница остаётся доступной по прямой ссылке `/salut-2026`, но с `noindex`. |

## email_opens (миграция 012)

Открытия писем рассылки: одна строка = одно срабатывание пикселя в письме. Повторное открытие пишется отдельной строкой.

| Колонка | Тип | Комментарий |
|---|---|---|
| id | UUID PK | |
| user_id | UUID FK | каскадное удаление вместе с пользователем |
| campaign | TEXT | имя рассылки (`return-sept-2026`), показывается на дашборде |
| opened_at | TIMESTAMPTZ | индекс `(campaign, opened_at DESC)` |
| user_agent | TEXT | чтобы отличать человека от прокси почтовика; обрезается до 300 символов |

IP намеренно не храним. Цифра открытий — **нижняя граница**: часть клиентов картинки блокирует, часть почтовиков подгружает их заранее.

`email_opens.campaign` — это `campaigns.id` в виде текста (для рассылок из админки). У сентябрьской рассылки, отправленной ещё скриптом, ключ был строковый и при переносе заменён на id кампании.

## campaigns / campaign_recipients (миграция 013)

Рассылки из админки (`/admin/mailing`).

`campaigns`: `name` (для себя), `subject`, `body` (простой текст, абзацы через пустую строку), `segment` (ключ группы из `db.Segments`), `daily_limit`, `pause_sec` (интервал между письмами, миграция 014, по умолчанию 120), `status` — `draft` → `sending` ⇄ `paused` → `done`.

`campaign_recipients`: снимок группы на момент создания рассылки — `user_id`, `email`, `name`, `status` (`pending`/`sent`/`failed`/`skipped`), `sent_at`, `error`. Уникальность по `(campaign_id, user_id)`, поэтому повторный импорт не плодит дублей.

**Группы (`db.Segments`)** — всегда пересечение с `consent_marketing_at IS NOT NULL`:

| Ключ | Кто |
|---|---|
| `all` | все с согласием на рассылку |
| `paid` | + есть доступ к платному курсу |
| `free` | + платных курсов нет |
| `sleeping` | + `last_seen_at` пустой или старше 30 дней |
| `service_paid` | **единственная группа БЕЗ согласия**: подтверждённая почта + платный курс + `consent_marketing_at IS NULL`. Только для сервисных писем про их курс. |

У `service_paid` в API стоит флаг `service_only: true` — админка показывает предупреждение, а `NextRecipient` не проверяет согласие. В письме таким людям вместо ссылки отписки подставляется кнопка «Хочу получать письма», заголовок `List-Unsubscribe` не ставится.

SQL каждой группы — отдельная константа, ввод пользователя в запрос не склеивается (см. CLAUDE.md).

## article_views

Счётчик чтения статей (миграция `015_article_views`).

| Поле | Тип | Описание |
|------|-----|----------|
| id | BIGSERIAL | |
| article_id | UUID | → `articles(id)`, `ON DELETE CASCADE` |
| event | TEXT | `open` / `read` / `cta`, ограничено CHECK |
| created_at | TIMESTAMPTZ | |

Индексы: `(article_id, event)` и `created_at DESC`.

**Персональных данных нет сознательно.** Не пишем ни IP, ни user-agent, ни идентификатора посетителя — только факт события и время. Поэтому счётчик не требует согласия по 152-ФЗ. User-agent используется в хендлере лишь для отсева роботов и никуда не сохраняется.
