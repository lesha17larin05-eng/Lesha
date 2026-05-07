# Архитектура

## Обзор

Один VPS, всё в Docker Compose. Наружу торчит только nginx (порт 80, в проде +443). API и фронт — во внутренней сети.

```
                      ┌─────────────────────────┐
   client ──443/80──▶ │        nginx            │
                      │  / → web                │
                      │  /api/ → api            │
                      │  /video-stream/ → api → │
                      │     X-Accel → /protected-videos/ (alias data/videos/)
                      └────────┬────────────────┘
                               │ internal docker network
              ┌────────────────┼────────────────┐
              ▼                ▼                ▼
         ┌─────────┐      ┌─────────┐      ┌──────────┐
         │  web    │      │  api    │      │ postgres │
         │ Astro   │      │ Go 1.22 │◄─────┤    16    │
         │ SSR :4321│      │ :8080   │      │  :5432   │
         └─────────┘      └─────────┘      └──────────┘
                                  │
                          ┌───────┴────────┐
                          │  data/ (bind)  │
                          │  pgdata/       │
                          │  videos/       │
                          │  uploads/      │
                          │  backups/      │
                          └────────────────┘
```

## Сервисы (docker-compose.yml)

| Сервис    | Образ                | Назначение                                                |
|-----------|----------------------|-----------------------------------------------------------|
| postgres  | postgres:16-alpine   | БД. Bind mount → `data/pgdata`. Healthcheck `pg_isready`. |
| migrate   | migrate/migrate:v4   | One-shot. Запускается до api, прогоняет `api/migrations/`.|
| api       | local build          | Go-бэкенд. Внутри: ffmpeg для HLS-нарезки.                |
| web       | local build          | Astro SSR. Multi-stage: build → run.                      |
| nginx     | nginx:1.27-alpine    | Реверс-прокси, единственный публичный сервис.             |

Тестовый стек (`docker-compose.test.yml`): `test-db` + `api-test`. Миграции применяются через `/docker-entrypoint-initdb.d/` (один файл `001_init.up.sql`, запускается postgres-образом при первом старте).

## Хранилище

Всё через **bind mounts** в `./data/` (локально) или `/srv/leshalarin/` (прод). Это даёт:
- Файлы видны через `ls`, бэкапятся `rsync`/`tar` без знания Docker.
- `docker compose down -v` не сносит данные (они лежат вне томов).
- Перенос на другой VPS — обычным `rsync data/`.

| Каталог            | Mount в контейнере      | Кто пишет |
|--------------------|-------------------------|-----------|
| `data/pgdata`      | postgres:/var/lib/postgresql/data | postgres |
| `data/videos`      | api:/var/videos (rw), nginx:/var/videos (ro) | api → ffmpeg |
| `data/uploads`     | api:/var/uploads        | api       |
| `data/backups`     | postgres:/backups       | pg_dump (вручную/по cron) |

## Сети

- `internal` (bridge) — все сервисы. nginx — единственный с `ports: 80:80`.
- В проде в nginx добавляется `:443` + сертификаты Let's Encrypt.

## Поток данных типового запроса

1. **Public страница** (`/`, `/courses`): браузер → nginx → web (Astro SSR). Фронт делает SSR-fetch напрямую в `api:8080` через docker-сеть для рендера.
2. **API из браузера** (`/api/...`): браузер → nginx → api. JS-обёртка в `Base.astro` подкладывает `X-CSRF-Token` для не-GET запросов.
3. **Видео**: браузер ← `playback_url` (signed JWT) → nginx (`/video-stream/`) → api (`/api/internal/video-auth`) проверяет JWT → возвращает заголовок `X-Accel-Redirect: /protected-videos/{id}/...` → nginx отдаёт файл с диска. Go не качает байты.
4. **Webhook Продамуса**: внешний → nginx (`/api/webhooks/prodamus`) → api. CSRF и rate limit на этот путь не применяются.

## Конфигурация

- `.env` (в `.gitignore`) — секреты, креды, флаги. Шаблон — `.env.example`.
- `DATA_DIR` определяет корень bind mount (по умолчанию `./data`).
- API читает env через простой `os.Getenv` (`api/internal/config/config.go`).
