# Деплой и эксплуатация

## Локально

```bash
cp .env.example .env   # один раз
make up                # postgres → migrate → api (со встроенным сидером) → web → nginx
```

Открыть http://localhost. Креды seed-аккаунтов — в корневом README.

## Production VPS (Ubuntu 22.04+)

Требования: 4+ ГБ RAM (ffmpeg прожорлив), 80+ ГБ диск, Docker + docker compose v2, открыты порты 22 (whitelist), 80, 443.

### Первая раскатка

```bash
# 1. Подготовить хост (один раз)
mkdir -p /root/data/{pgdata,videos,uploads,backups,certbot/www}

# 2. Клонировать и сконфигурировать
cd /root && git clone https://github.com/lesha17larin05-eng/Lesha.git Lesha && cd Lesha
cp .env.example .env
$EDITOR .env   # секреты, DATA_DIR=/root/data, APP_HOST=https://leshalarin.ru, CORS_ORIGIN=https://leshalarin.ru, APP_ENV=production

# 3. Выпустить TLS-сертификат и поднять стек
bash scripts/init-tls.sh
# (см. ниже про DNS — перед запуском A-запись leshalarin.ru должна указывать на IP сервера)
```

### Обновления

Дальнейшие апдейты — одной командой с локальной машины:

```bash
cp .deploy.env.example .deploy.env   # один раз, заполнить DEPLOY_PASS
make deploy
```

Под капотом — `git pull && docker compose build && up -d` + smoke-проверка `/api/health`. Миграции и сидер запускаются автоматически (compose-сервис `migrate` + сам api при старте — идемпотентно).

Соседние таргеты: `make prod-logs`, `make prod-ps`, `make deploy-shell`, `make tls-init` (повторный запуск тоже безопасен — certbot ничего не делает, если серт ещё валиден).

### TLS (HTTPS)

`scripts/init-tls.sh` делает всё за раз:

1. Генерит self-signed placeholder в `/etc/letsencrypt/live/leshalarin.ru/` — чтобы nginx-контейнер с 443-блоком мог стартовать.
2. Поднимает стек (`docker compose up -d`). Nginx слушает 80 (отдаёт `/.well-known/acme-challenge`) и 443 (на placeholder-серте).
3. Делает pre-check, что `http://leshalarin.ru/.well-known/acme-challenge/...` реально доходит снаружи (то, что увидит Let's Encrypt).
4. Запускает `certbot/certbot:latest certonly --webroot -w /var/www/certbot -d leshalarin.ru -d www.leshalarin.ru` — реальный серт заменяет placeholder.
5. Делает `nginx -t && nginx -s reload`.

Требования:
- A-записи `leshalarin.ru` и `www.leshalarin.ru` указывают на IP сервера.
- Порт 80 открыт извне (без него Let's Encrypt не сможет валидировать).

Опции скрипта:
- `STAGING=1 bash scripts/init-tls.sh` — выпустить тестовый серт через Let's Encrypt staging (для отладки, не расходует лимит реальных серов).
- `DOMAIN=`, `ALT_DOMAIN=`, `LETSENCRYPT_EMAIL=` — переопределить дефолты.

**Автообновление серта.** Скрипт в конце печатает готовую cron-строку. Скопируй и выполни — будет каждый день в 03:00 проверять renew и релоадить nginx, если выпустится новый серт. Let's Encrypt сам обновит за 30 дней до истечения; чаще — no-op.

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
| `LEAD_NOTIFY_EMAIL` | Куда слать письма о новых заявках с форм сайта (default `les-larin@yandex.ru`) |
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
