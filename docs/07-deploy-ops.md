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

### TLS (HTTPS) — пошагово

`nginx/nginx.conf` уже содержит готовый, но **закомментированный** TLS-блок и редирект 80→443. На сервере остаётся:

1. **Привязать домен.** A-запись `leshalarin.ru` (и `www`) → IP сервера. Подождать, пока DNS пропагнётся (`dig +short leshalarin.ru`).
2. **Установить certbot:** `apt install -y certbot`.
3. **Выпустить сертификат.** Один раз, при остановленном nginx-контейнере (Let's Encrypt должен сам слушать :80):
   ```bash
   docker compose stop nginx
   certbot certonly --standalone -d leshalarin.ru -d www.leshalarin.ru
   docker compose start nginx
   ```
   Серты появятся в `/etc/letsencrypt/live/leshalarin.ru/`.
4. **Прокинуть серты в контейнер.** В `docker-compose.yml` у сервиса `nginx` добавить в `volumes`:
   ```yaml
   - /etc/letsencrypt:/etc/letsencrypt:ro
   ```
5. **Включить HTTPS в `nginx/nginx.conf`:**
   - Раскомментировать весь `server { listen 443 ssl http2; ... }` блок.
   - Раскомментировать строку `return 301 https://$host$request_uri;` в `server { listen 80; }` и закомментировать всё, что ниже неё внутри того же блока (location-секции).
6. **Перезапустить nginx:**
   ```bash
   docker compose exec nginx nginx -t           # проверка конфига
   docker compose restart nginx
   ```
7. **Автообновление серта.** Cron на хосте:
   ```bash
   echo '0 3 * * * certbot renew --quiet --post-hook "docker compose -f /opt/larin/docker-compose.yml exec -T nginx nginx -s reload"' \
     | sudo crontab -
   ```

После этого сайт открывается по `https://leshalarin.ru`, cookies становятся `Secure` автоматически (Go-API смотрит на `X-Forwarded-Proto`, который nginx уже подставляет).

#### Что включено «из коробки» в этом конфиге

- `Content-Security-Policy` (через `map $csp_value`) — на 80 и 443.
- `Strict-Transport-Security` (HSTS) — только на 443.
- `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy` — на обоих.
- `ssl_protocols TLSv1.2 TLSv1.3` + OCSP stapling — стандартный современный набор.

Если что-то на сайте перестало работать после включения CSP — открой DevTools → Console, нарушения видно сразу. Исправляй в одном месте — в `map $csp_value`.

## Переменные окружения

| Переменная              | Назначение                                        | Дефолт          |
|-------------------------|---------------------------------------------------|-----------------|
| `APP_ENV`               | `development` / `production`. Влияет на показ dev-полей (`verify_link_dev`, `reset_link_dev`, `password_dev`) и enable `PRODAMUS_TEST_MODE`-флоу. **Secure-флаг cookies теперь зависит от реального протокола** (X-Forwarded-Proto), а не от APP_ENV. | `development` |
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
