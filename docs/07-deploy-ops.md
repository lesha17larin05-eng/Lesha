# Деплой и эксплуатация

## Локально

```bash
cp .env.example .env   # один раз
make up                # postgres → migrate → api (со встроенным сидером) → web → nginx
```

Открыть http://localhost. Креды seed-аккаунтов — в корневом README.

## Production VPS (Ubuntu 22.04+)

Требования: 4+ ГБ RAM (ffmpeg прожорлив), 80+ ГБ диск, открыты порты 22 (whitelist), 80, 443.

```bash
# 1. Подготовить хост
useradd -u 1000 -m app
mkdir -p /srv/leshalarin/{pgdata,videos,uploads,backups}
chown -R 1000:1000 /srv/leshalarin/

# 2. Раскатать
cd /opt && git clone <repo> larin && cd larin
cp .env.example .env
$EDITOR .env   # секреты, DATA_DIR=/srv/leshalarin, APP_HOST=https://...
make build && make up
```

### TLS

Базовый nginx-конфиг слушает только `:80`. Для прода:
1. На хосте: `apt install certbot && certbot certonly --standalone -d leshalarin.ru`.
2. Расширить `nginx/nginx.conf` блоком `server { listen 443 ssl http2; ... }` и сертификатами из `/etc/letsencrypt/live/...`.
3. Добавить volume mount сертификатов в `nginx` сервис в `docker-compose.yml`.
4. Cron для обновления: `0 3 * * * certbot renew --post-hook "docker compose -f /opt/larin/docker-compose.yml restart nginx"`.

## Переменные окружения

| Переменная              | Назначение                                        | Дефолт          |
|-------------------------|---------------------------------------------------|-----------------|
| `APP_ENV`               | `development` / `production`. Режим cookies (Secure). | `development` |
| `APP_HOST`              | Базовый URL, используется в письмах и redirect Продамуса. | `http://localhost` |
| `JWT_SECRET`            | Подпись access JWT (≥32 байта).                   | dev            |
| `SESSION_SECRET`        | Резерв для будущей серверной сессии.              | dev            |
| `DATABASE_URL`          | Строка подключения к Postgres.                    | `postgres://app:app@postgres:5432/app` |
| `DATA_DIR`              | Корень bind mount.                                | `./data`       |
| `POSTGRES_USER/PASSWORD/DB` | Используются и postgres-контейнером и `DATABASE_URL`. | `app/app/app` |
| `SMTP_HOST/PORT/USER/PASSWORD/FROM` | SMTP. Если HOST пуст — письма пропускаются (логируются). | пусто |
| `PRODAMUS_PAYFORM_URL`  | URL платёжной страницы (например `https://leshalarin.payform.ru`). | dev placeholder |
| `PRODAMUS_SECRET_KEY`   | Секрет для HMAC-подписи (берётся в кабинете Продамуса). | dev placeholder |
| `PRODAMUS_TEST_MODE`    | `true` → checkout возвращает локальный fake-payment. | `true` |
| `VIDEO_TOKEN_SECRET`    | Подпись JWT signed URL для HLS.                   | dev            |
| `VIDEO_TOKEN_TTL`       | Время жизни видео-токена.                          | `2h`           |
| `VIDEO_STORAGE_PATH`    | Путь внутри контейнера api.                        | `/var/videos`  |
| `ADMIN_EMAIL/PASSWORD`  | Сидер создаст этого юзера с `role=admin`.          | `admin@leshalarin.ru / admin12345` |
| `USER_EMAIL/PASSWORD`   | Сидер создаст обычного юзера с записью на free курс. | `user@leshalarin.ru / user12345` |
| `CORS_ORIGIN`           | Origin для CORS. В проде — `https://leshalarin.ru`. | `http://localhost` |

**В проде обязательно перегенерируй**: `JWT_SECRET`, `SESSION_SECRET`, `VIDEO_TOKEN_SECRET`, `ADMIN_PASSWORD`, `USER_PASSWORD`, `PRODAMUS_SECRET_KEY`, `PRODAMUS_TEST_MODE=false`.

## Бэкапы

Bind mount → можно бэкапить обычными утилитами. Команды для cron:

```bash
# 1. БД
docker compose exec -T postgres pg_dump -U app app | gzip > /srv/leshalarin/backups/db-$(date +%F).sql.gz
# Хранить 30 дней:
find /srv/leshalarin/backups -name "db-*.sql.gz" -mtime +30 -delete

# 2. Видео и uploads
rsync -a --delete /srv/leshalarin/videos /srv/leshalarin/uploads backup@remote:/backup/leshalarin/
```

Хранение не путать: pgdata бэкапить через `pg_dump`, не сырыми файлами (Postgres ругается на холодный backup живой БД). Видео — через rsync.

### Восстановление

```bash
gunzip -c db-2026-05-06.sql.gz | docker compose exec -T postgres psql -U app -d app
rsync -a backup@remote:/backup/leshalarin/videos /srv/leshalarin/
```

## Логи и мониторинг

- Логи api/web/nginx — через `docker compose logs` (Docker JSON driver).
- Ротация — Docker сам режет (`max-size: 10m, max-file: 3` рекомендуется добавить в `docker-compose.yml` если будет много трафика).
- Простой uptime-мониторинг: `uptime-kuma` отдельным контейнером, либо cron + `curl /api/health`.

## Перенос на другой VPS

```bash
# на старом
docker compose down
tar -czf /tmp/larin.tgz /srv/leshalarin /opt/larin

# на новом
mkdir -p /srv/leshalarin /opt/larin
tar -xzf larin.tgz -C /
chown -R 1000:1000 /srv/leshalarin
cd /opt/larin && make up
```
