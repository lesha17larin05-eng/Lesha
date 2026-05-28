#!/usr/bin/env bash
# Идемпотентный апдейт прода. Запускается:
#   - вручную на сервере: `/root/Lesha/scripts/deploy.sh`
#   - или с локальной машины: `make deploy` (sshpass под капотом).
#
# Делает: git pull → docker compose build → docker compose up -d → проверка хелсчеков.
# Никаких миграций руками: сервис `migrate` в compose отрабатывает при `up` сам.
# Сидер тоже сам запускается api при старте (идемпотентно).
set -euo pipefail

REPO_DIR="${REPO_DIR:-/root/Lesha}"
BRANCH="${BRANCH:-main}"
COMPOSE="${COMPOSE:-docker compose -f docker-compose.yml}"

cd "$REPO_DIR"

echo ">> [1/5] git fetch + reset --hard origin/$BRANCH"
git fetch --prune origin
git reset --hard "origin/$BRANCH"

if [ ! -f .env ]; then
  echo "!! .env отсутствует в $REPO_DIR. Скопируй .env.example в .env и пропиши секреты."
  exit 1
fi

echo ">> [2/5] docker compose build (api + web)"
$COMPOSE --env-file .env build api web

echo ">> [3/5] docker compose up -d"
$COMPOSE --env-file .env up -d --remove-orphans

echo ">> [4/5] подождать healthcheck postgres (до 30с)"
for i in $(seq 1 30); do
  if $COMPOSE --env-file .env ps postgres | grep -q "(healthy)"; then
    break
  fi
  sleep 1
done

echo ">> [5/5] статус контейнеров и smoke-test"
$COMPOSE --env-file .env ps

# Локальный smoke: api сам себя проверяет через /api/health
if curl -fsS -o /dev/null http://127.0.0.1/api/health; then
  echo ">> OK: /api/health отвечает 200"
else
  echo "!! /api/health не отвечает 200 — посмотри логи: $COMPOSE logs api"
  exit 2
fi

echo ">> Deploy готов."
