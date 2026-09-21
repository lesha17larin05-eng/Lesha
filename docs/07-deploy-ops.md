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
| `METRIKA_ID` | Номер счётчика Яндекс.Метрики (пусто — аналитика выключена) |
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

## Offsite-бэкап базы (почта)

Помимо локального `pg-backup.sh` (04:00, ротация 7 дней), крон `04:15 /root/bin/pg-backup-mail.sh` шифрует свежий дамп (AES-256-CBC, pbkdf2, пароль в `/root/.backup-pass`, копия пароля у владельца) и отправляет вложением на почту владельца через smtp.yandex.ru (креды из `.env`). Лог: `/var/log/pg-backup.log`. Восстановление: `openssl enc -d -aes-256-cbc -pbkdf2 -in <file>.enc -out dump.sql.gz && gunzip … && psql`.

## Рассылка по базе (ручная, с сервера)

Живёт на сервере в `/root/mailing/` (в репозиторий не коммитится — внутри тексты писем и журнал адресов):

| Файл | Назначение |
|---|---|
| `letter.html` | Шаблон письма. Плейсхолдеры `{{NAME}}` (имя; если пусто — приветствие без имени) и `{{UNSUB}}` (персональная ссылка отписки). |
| `send.py` | Отправка. Берёт из базы только `consent_marketing_at IS NOT NULL`, шлёт каждому отдельное письмо, пауза ~55–75 с, лимит на запуск. |
| `sent.log` | Журнал `время \| email \| OK/FAIL \| причина`. Повторный запуск пропускает тех, у кого `OK`. |

В шаблоне есть ещё `{{PIXEL}}` — подписанная ссылка на `/api/pixel.gif`. Имя рассылки задаётся константой `CAMPAIGN` в `send.py`; под ним открытия видны на дашборде админки.

**Важно:** на каждое письмо скрипт открывает SMTP-соединение заново. Одно соединение на всю партию не живёт — при паузах в минуту Яндекс закрывает его примерно через 25 минут (так были потеряны два письма в первой партии 15.09.2026).

```bash
python3 /root/mailing/send.py --dry-run --limit 25   # посмотреть, кому уйдёт
python3 /root/mailing/send.py --limit 25             # отправить партию
python3 /root/mailing/send.py --only a@b.ru --force  # тестовое письмо себе
```

Письма уходят через тот же SMTP Яндекса, что и транзакционные (`SMTP_USER` из `.env`). **Лимит Яндекса — 300 писем в сутки через SMTP**, поэтому партиями по 25 и с паузами. При ответе сервера с `550`/`blocked`/`limit` скрипт останавливается сам, чтобы не спалить ящик, которым выдаются доступы к курсам.

Каждое письмо несёт заголовки `List-Unsubscribe` и `List-Unsubscribe-Post: List-Unsubscribe=One-Click` — отписка обрабатывается эндпоинтом `/api/unsubscribe` (см. `docs/03-api.md`). Ссылка подписана HMAC на `JWT_SECRET`, скрипт считает её той же формулой, что и Go.

## Сжатие и кэш статики (nginx)

`gzip on` в блоке `http` — уровень 6, от 1 КБ, для text/css/js/json/xml/svg. Видео, растровые картинки и woff2 уже сжаты, их не трогаем. До включения страницы отдавались сырыми: `/results` — 186 КБ вместо 16.

Кэш задаётся в nginx, а не Astro (тот отдаёт `max-age=0`), поэтому в каждом location стоит `proxy_hide_header Cache-Control` перед своим `add_header`:

| Путь | Cache-Control |
|------|---------------|
| `/_astro/` | `public, max-age=31536000, immutable` (имена с хешем) |
| `/fonts/` | `public, max-age=31536000, immutable` |
| `/img/`, `/videos/` | `public, max-age=2592000` |
| favicon, apple-touch-icon, robots.txt | `public, max-age=604800` |
| остальное (HTML) | без кэша, как отдаёт Astro |

`favicon.ico` пересобран из `fav-256.png` в три размера (16/32/48): 5 КБ вместо 117.

### Осторожно: `docker compose` на сервере — только с `-f docker-compose.yml`

В корне лежит `docker-compose.override.yml` — он для локальной разработки и
подменяет `api`/`web` на `golang:1.22-alpine` и `node:20-alpine` с dev-сервером.
Docker Compose подхватывает override **автоматически**, поэтому голая команда
`docker compose up -d` на проде поднимет Vite вместо сборки, и сайт начнёт
отвечать `403 Blocked request. This host is not allowed`.

Правильно — как в `scripts/deploy.sh`:

```bash
docker compose -f docker-compose.yml --env-file .env up -d
```

Если нужно перечитать конфиг nginx после `git pull`, недостаточно `nginx -s reload`:
`git reset --hard` заменяет файл целиком (новый inode), а bind-mount продолжает
показывать старый. Контейнер надо пересоздать:

```bash
docker compose -f docker-compose.yml --env-file .env up -d --force-recreate nginx
```
