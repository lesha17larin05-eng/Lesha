# Платформа онлайн-курсов «Алексей Ларин»

Production-ready монорепо: лендинг + платформа курсов (бесплатные/платные), личный кабинет, админка, оплата через Продамус, защищённая отдача видео через nginx X-Accel-Redirect.

**Стек:** Astro (SSR, Node adapter) · Go 1.22 (chi + pgx) · PostgreSQL 16 · nginx · Docker Compose · Make.

> 📚 **Документация:** [`docs/`](docs/README.md) — полное описание архитектуры, API, БД, тестов, деплоя.
> 🤖 **Правила работы для агентов:** [`CLAUDE.md`](CLAUDE.md) — обязательно к прочтению перед изменениями.

---

## Quickstart (локально)

```bash
git clone <repo> larin && cd larin
cp .env.example .env       # секреты — в .env (он в .gitignore)
make up                     # поднимает весь стек: postgres → migrate → api → web → nginx
```

Открыть [http://localhost](http://localhost).

Стек поднимается одной командой. Миграции применяются автоматически (контейнер `migrate`). Сидер запускается из API на старте — идемпотентно.

### Дефолтные креды (после `make up`)

| Роль   | Email                  | Пароль       |
|--------|------------------------|--------------|
| Админ  | `admin@leshalarin.ru`  | `admin12345` |
| Юзер   | `user@leshalarin.ru`   | `user12345`  |

> Креды задаются в `.env` (`ADMIN_EMAIL`/`ADMIN_PASSWORD`/`USER_EMAIL`/`USER_PASSWORD`). После старта сидер создаёт их с `email_verified_at`. Меняйте перед продом.

### Что засеяно

- **Бесплатный курс** `/courses/intro-zdorovye` — 5 уроков, без модулей. Юзер автоматически записан.
- **Платный курс** `/courses/transformaciya-90` — 6 уроков в 2 модулях, цена 9 900 ₽. Один из уроков — `is_preview=true` (бесплатный пробный).

---

## Команды Makefile

| Команда           | Что делает |
|-------------------|------------|
| `make up`         | Поднять весь стек (postgres + миграции + api + web + nginx). Фронт пересобирается автоматически — без `dev`-сервера. |
| `make down`       | Остановить (данные в `./data` сохранены) |
| `make build`      | Пересобрать api и web |
| `make web-build`  | Только web |
| `make api-build`  | Только api |
| `make rebuild`    | down + build + up |
| `make logs`       | Хвост логов |
| `make ps`         | Статус контейнеров |
| `make psql`       | psql внутри postgres |
| `make migrate`    | Прогнать миграции вручную (обычно не нужно — выполняется при up) |
| `make seed`       | Перезапустить api → пересидит идемпотентно |
| `make test`       | Прогнать **все Go-тесты в отдельном контейнере** с тестовой БД |
| `make test-clean` | Удалить тестовый стек |
| `make clean`      | Снести стек и тома (но не `./data`) |
| `make fresh`      | **DESTRUCTIVE** — снести всё, включая `./data`, и поднять заново |

---

## Прогон тестов

```bash
make test
```

Поднимает отдельный compose (`docker-compose.test.yml`): чистая Postgres-БД, миграции, контейнер с `go test ./...`. Покрытие:

- `internal/auth` — argon2id хеширование/верификация, JWT issue/parse.
- `internal/prodamus` — HMAC-подпись (детерминированность, исключение `signature` поля, валидация).
- `internal/video` — JWT-токен видео (roundtrip, чужой секрет, expired).
- `internal/handlers` — интеграционные тесты через `httptest` с реальной БД: health, register/login/me, неавторизованный `/me`, бесплатная запись + список «Мои курсы», checkout требует verified email, тестовый режим Продамуса возвращает fake-payment URL, admin-эндпоинты требуют `role=admin`, апсерт прогресса, валидный/невалидный webhook Продамуса, скрытие платного контента анонимам.

---

## Структура

```
.
├── api/                          Go-бэкенд
│   ├── cmd/server/main.go        точка входа + роутинг
│   ├── internal/
│   │   ├── auth/                 argon2id, JWT, рандом-токены
│   │   ├── config/               загрузка env
│   │   ├── db/                   pgxpool + репозитории + модели
│   │   ├── email/                SMTP (async)
│   │   ├── handlers/             HTTP-хендлеры (auth, courses, payments, video, admin)
│   │   ├── middleware/           request-id, logger, recover, CORS, auth, CSRF, rate limit
│   │   ├── prodamus/             клиент + HMAC sign/verify
│   │   ├── seed/                 idempotent seeding (admin/user/free+paid)
│   │   └── video/                signed JWT для X-Accel-Redirect
│   ├── migrations/               *.up.sql / *.down.sql (golang-migrate)
│   ├── Dockerfile                runtime образ (alpine + ffmpeg)
│   └── Dockerfile.test           для прогона go test в контейнере
├── web/                          Astro SSR
│   ├── src/pages/                index, consultation, course, results, blog/, courses, cabinet, admin, auth
│   ├── src/components/           SiteHeader, SiteFooter (общая шапка/футер для публичных страниц)
│   ├── src/layouts/              Base.astro (кабинет/админ), SiteLayout.astro (публичный сайт)
│   ├── src/legacy/               оригинальные HTML маркетинговых страниц, импортируются ?raw
│   ├── src/middleware.ts         защита /cabinet и /admin
│   ├── src/lib/api.ts            SSR-клиент к API
│   ├── public/admin-blog-editor.js  кастом-элемент HTML-редактора статей
│   ├── astro.config.mjs          output: server + Node adapter
│   └── Dockerfile                multi-stage build
├── nginx/nginx.conf              443/80, /api/, /video-stream/, X-Accel-Redirect
├── docker-compose.yml            постgres + migrate + api + web + nginx
├── docker-compose.test.yml       test-db + test-migrate + api-test
├── Makefile                      все команды
├── .env / .env.example
└── data/                         bind mount: pgdata/, videos/, uploads/, backups/
```

---

## Продамус

1. В `.env` укажи `PRODAMUS_PAYFORM_URL` (например `https://leshalarin.payform.ru`) и `PRODAMUS_SECRET_KEY`.
2. В кабинете Продамуса в настройках уведомлений укажи URL: `https://<твой-домен>/api/webhooks/prodamus`.
3. Для разработки — оставь `PRODAMUS_TEST_MODE=true`. Тогда `POST /api/courses/:slug/checkout` возвращает локальную ссылку `/api/dev/fake-payment?order_id=...`, переход по которой эмулирует успешную оплату и сразу выдаёт `enrollment`.
4. Подпись webhook сравнивается через `subtle.ConstantTimeCompare`. Идемпотентность по `order_id` (повторный вебхук на уже-оплаченный заказ — no-op).

---

## Видео

1. Админ: `POST /api/admin/videos/upload` (multipart, поле `file`). Лимит 5 ГБ.
2. API кладёт файл в `data/videos/{video_id}/source.mp4` (bind mount), запускает `ffmpeg` → HLS (`index.m3u8` + сегменты).
3. Юзер запрашивает `GET /api/videos/:id/playback` — получает `playback_url` с короткоживущим JWT (по умолчанию 2 часа, `VIDEO_TOKEN_TTL`).
4. Плеер на странице урока запрашивает `/video-stream/{vid}/index.m3u8?token=...` → nginx проксирует в Go (`/api/internal/video-auth`) → Go валидирует JWT и возвращает `X-Accel-Redirect: /protected-videos/{vid}/index.m3u8` → nginx отдаёт файл напрямую с диска (`location /protected-videos/` помечен `internal`).
5. Прогрес сохраняется раз в 10 секунд через `POST /api/lessons/:id/progress`.

---

## SSL / прод

В nginx-конфиге сейчас только `:80`. Для прода:

1. Подцепи certbot (отдельным compose-сервисом или на хосте) и положи серты в `/etc/letsencrypt/live/<domain>/`.
2. Раскомментируй/добавь блок `server { listen 443 ssl http2; ... }` и редирект `:80 → :443`.
3. CSP / HSTS уже подготовлены в шаблоне (раздел 6 ТЗ).

---

## Бэкапы

Всё в `./data` (или `/srv/leshalarin/` в проде) — bind mount. Можно бэкапить обычным `rsync` без знания Docker:

```bash
# базы данных
docker compose exec postgres pg_dump -U app app | gzip > data/backups/dump-$(date +%F).sql.gz
# видео и uploads
rsync -a data/videos data/uploads user@backup-host:/backup/leshalarin/
```

---

## Полезные эндпоинты для ручной проверки

```bash
# health
curl http://localhost/api/health

# логин админом (получит cookies)
curl -c cookies.txt -X POST http://localhost/api/auth/login \
  -H 'content-type: application/json' \
  -d '{"email":"admin@leshalarin.ru","password":"admin12345"}'

# статистика админа
curl -b cookies.txt -H "X-CSRF-Token: $(grep csrf cookies.txt | awk '{print $7}')" \
  http://localhost/api/admin/stats

# список статей блога
curl http://localhost/api/articles | jq '.[0:3]'

# одна статья
curl http://localhost/api/articles/akrojoga-chto-eto | jq '{slug,title,reading_minutes}'
```

## Куда заходить

- Публичный сайт: <http://localhost/>
- Бесплатный курс: <http://localhost/course>
- Консультация: <http://localhost/consultation>
- Результаты: <http://localhost/results>
- Блог: <http://localhost/blog>
- Кабинет: <http://localhost/cabinet>
- Админка: <http://localhost/admin> → раздел **Блог** (<http://localhost/admin/blog>) для управления статьями.

При первом старте сидер автоматически загружает 32 статьи из `сайт/blog/*.html` (через `go:embed` в `api/internal/seed/articles_html/`). Идемпотентно: при повторном запуске пропустит уже существующие slug'и.

---

## Что НЕ сделано / known limitations

- TLS-блок в nginx закомментирован — добавь сертификаты для прода.
- Email рассылка работает только при заполненных `SMTP_*`. Без них письма логируются и пропускаются — для разработки в JSON-ответе на `/auth/register` и `/auth/forgot-password` возвращаются `verify_link_dev` / `reset_link_dev`.
- Драг-н-дроп сортировка уроков в админке не реализована — есть базовая форма CRUD, перепорядок через `sort_order` в API.
