SHELL := /bin/bash
COMPOSE ?= docker compose
ENVFILE ?= --env-file .env

.PHONY: help up down build rebuild logs ps psql migrate seed test test-clean clean fresh web-build api-build img

help:
	@echo "Targets:"
	@echo "  make up           — поднять весь стек (postgres + миграции + api + web + nginx)"
	@echo "  make down         — остановить стек (данные сохраняются в ./data)"
	@echo "  make build        — пересобрать образы api и web"
	@echo "  make web-build    — собрать только web"
	@echo "  make api-build    — собрать только api"
	@echo "  make rebuild      — down + build + up"
	@echo "  make logs         — хвост логов"
	@echo "  make ps           — статус контейнеров"
	@echo "  make psql         — psql внутри контейнера postgres"
	@echo "  make migrate      — прогнать миграции (выполняется при up автоматически)"
	@echo "  make seed         — пересоздать seed-данные (api сам сидит при старте)"
	@echo "  make test         — прогнать ВСЕ Go-тесты в контейнере (отдельная test-БД)"
	@echo "  make test-clean   — снести test-стек"
	@echo "  make fresh        — снести всё включая data/ (DESTRUCTIVE) и поднять заново"
	@echo "  make clean        — снести стек и тома, но НЕ data/"
	@echo "  make img FILE=foo.jpg DIR=hero [NAME=slug] — обработать картинку из корня репо в web/public/img/<DIR>/"

up:
	@if [ ! -f .env ]; then cp .env.example .env; echo ">> .env создан из .env.example"; fi
	$(COMPOSE) $(ENVFILE) up -d --build
	@echo ">> Готово. Открой http://localhost"

down:
	$(COMPOSE) $(ENVFILE) down

build:
	$(COMPOSE) $(ENVFILE) build api web

web-build:
	$(COMPOSE) $(ENVFILE) build web

api-build:
	$(COMPOSE) $(ENVFILE) build api

rebuild: down build up

logs:
	$(COMPOSE) $(ENVFILE) logs -f --tail=200

ps:
	$(COMPOSE) $(ENVFILE) ps

psql:
	$(COMPOSE) $(ENVFILE) exec postgres psql -U app -d app

migrate:
	$(COMPOSE) $(ENVFILE) run --rm migrate

seed:
	$(COMPOSE) $(ENVFILE) restart api
	@echo ">> API перезапущен — сидер запустится автоматически (идемпотентно)"

test:
	docker compose -f docker-compose.test.yml up --build --abort-on-container-exit --exit-code-from api-test
	docker compose -f docker-compose.test.yml down -v

test-clean:
	docker compose -f docker-compose.test.yml down -v

clean:
	$(COMPOSE) $(ENVFILE) down -v

fresh:
	$(COMPOSE) $(ENVFILE) down -v
	rm -rf data
	mkdir -p data/{pgdata,videos,uploads,backups}
	$(MAKE) up

# make img FILE=foo.jpg DIR=hero NAME=lazy-vs-coach
# Запускается в одноразовом контейнере node:20-alpine с примонтированным репо,
# чтобы не требовать локальной установки node/sharp.
img:
	@if [ -z "$(FILE)" ]; then echo "Usage: make img FILE=foo.jpg [DIR=subdir] [NAME=slug]"; exit 1; fi
	docker run --rm -v "$(CURDIR)":/repo -w /repo/web node:20-alpine sh -c "\
		(test -d node_modules/sharp || npm install --no-audit --no-fund --silent sharp) && \
		node scripts/process-image.mjs ../$(FILE) $(DIR) $(if $(NAME),--name=$(NAME))"
