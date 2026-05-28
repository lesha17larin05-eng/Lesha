#!/usr/bin/env bash
# Одноразовая инициализация TLS на проде:
#   1) Генерим self-signed placeholder в /etc/letsencrypt/live/leshalarin.ru/,
#      чтобы nginx-контейнер мог стартовать с 443-блоком.
#   2) Поднимаем стек (если ещё не поднят).
#   3) Запускаем certbot через webroot — он заменяет placeholder настоящим сертом.
#   4) Релоадим nginx.
#   5) Печатаем готовую cron-строку для авто-renew.
#
# Все шаги идемпотентны: повторный запуск ничего не сломает.
set -euo pipefail

REPO_DIR="${REPO_DIR:-/root/Lesha}"
DOMAIN="${DOMAIN:-leshalarin.ru}"
ALT_DOMAIN="${ALT_DOMAIN:-www.leshalarin.ru}"
EMAIL="${LETSENCRYPT_EMAIL:-admin@leshalarin.ru}"
DATA_DIR_DEFAULT="/root/data"
COMPOSE="${COMPOSE:-docker compose -f docker-compose.yml}"

cd "$REPO_DIR"

if [ ! -f .env ]; then
  echo "!! $REPO_DIR/.env отсутствует — заполни секреты сначала."; exit 1
fi

# DATA_DIR из .env
DATA_DIR="$(grep -E '^DATA_DIR=' .env | tail -1 | cut -d= -f2- || true)"
DATA_DIR="${DATA_DIR:-$DATA_DIR_DEFAULT}"
WEBROOT="$DATA_DIR/certbot/www"

echo ">> [1/5] подготавливаю каталоги"
mkdir -p "$WEBROOT"
mkdir -p /etc/letsencrypt/live/"$DOMAIN"

# Self-signed placeholder — только если нет настоящего серта.
# Признак "настоящий" — это symlink в live/, который ставит certbot.
if [ ! -L /etc/letsencrypt/live/"$DOMAIN"/fullchain.pem ]; then
  if [ ! -f /etc/letsencrypt/live/"$DOMAIN"/fullchain.pem ]; then
    echo ">> placeholder cert не найден — генерирую self-signed на 1 день"
    docker run --rm -v /etc/letsencrypt:/etc/letsencrypt alpine:3.20 sh -c "
      apk add --no-cache openssl >/dev/null &&
      openssl req -x509 -nodes -newkey rsa:2048 -days 1 \
        -keyout /etc/letsencrypt/live/$DOMAIN/privkey.pem \
        -out  /etc/letsencrypt/live/$DOMAIN/fullchain.pem \
        -subj '/CN=$DOMAIN' 2>/dev/null
    "
  fi
fi

echo ">> [2/5] docker compose up -d (с новым nginx.conf)"
$COMPOSE --env-file .env up -d --remove-orphans

# Дать nginx 2-3 секунды чтобы поднялся
sleep 3

echo ">> [3/5] pre-check: nginx отдаёт /.well-known/acme-challenge?"
mkdir -p "$WEBROOT/.well-known/acme-challenge"
TEST_FILE=".acme-precheck-$$"
echo "ok" > "$WEBROOT/.well-known/acme-challenge/$TEST_FILE"
trap "rm -f $WEBROOT/.well-known/acme-challenge/$TEST_FILE" EXIT

# Проба сначала по IP+Host (DNS мог ещё не пропагнуться, но nginx обязан отвечать)
if ! curl -fsS -H "Host: $DOMAIN" "http://127.0.0.1/.well-known/acme-challenge/$TEST_FILE" >/dev/null; then
  echo "!! nginx не отдаёт acme-challenge с локалхоста. Проверь docker compose ps + logs nginx"
  $COMPOSE --env-file .env ps
  $COMPOSE --env-file .env logs --tail=30 nginx
  exit 2
fi

# А теперь проба через публичный DNS — это то, что увидит Let's Encrypt.
if ! curl -fsS "http://$DOMAIN/.well-known/acme-challenge/$TEST_FILE" >/dev/null; then
  echo "!! Через публичный $DOMAIN не доходит — проверь DNS (A-запись на этот IP) и фаервол на :80."
  echo "   Текущий публичный IP сервера:"
  curl -fsS https://ifconfig.me || true; echo
  echo "   dig +short $DOMAIN:"
  getent hosts "$DOMAIN" || true
  exit 3
fi

echo ">> [4/5] запускаю certbot (webroot)"
STAGING_ARGS=()
if [ "${STAGING:-0}" = "1" ]; then
  STAGING_ARGS=( --staging )
  echo "   STAGING — серт будет невалидный, только для отладки"
fi

docker run --rm \
  -v /etc/letsencrypt:/etc/letsencrypt \
  -v /var/lib/letsencrypt:/var/lib/letsencrypt \
  -v "$WEBROOT":/var/www/certbot \
  certbot/certbot:latest \
  certonly --webroot -w /var/www/certbot \
  -d "$DOMAIN" -d "$ALT_DOMAIN" \
  --email "$EMAIL" --agree-tos --no-eff-email --non-interactive \
  "${STAGING_ARGS[@]}"

echo ">> [5/5] reload nginx"
$COMPOSE --env-file .env exec -T nginx nginx -t
$COMPOSE --env-file .env exec -T nginx nginx -s reload

echo ""
echo ">> Готово. Проверь https://$DOMAIN"
echo ""
echo ">> Поставь cron на автообновление (один раз, копипастом):"
echo ""
cat <<EOF
( crontab -l 2>/dev/null | grep -v 'leshalarin renew' ; \\
  echo '0 3 * * * docker run --rm -v /etc/letsencrypt:/etc/letsencrypt -v /var/lib/letsencrypt:/var/lib/letsencrypt -v $WEBROOT:/var/www/certbot certbot/certbot:latest renew --quiet --webroot -w /var/www/certbot && docker compose -f $REPO_DIR/docker-compose.yml --env-file $REPO_DIR/.env exec -T nginx nginx -s reload  # leshalarin renew' \\
) | crontab -
EOF
