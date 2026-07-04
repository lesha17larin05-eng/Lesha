# Тестирование

## Как запускать

```bash
make test         # поднимает test-стек и прогоняет все Go-тесты в контейнере
make test-clean   # сносит test-стек, тома и сети
```

`make test` использует `docker-compose.test.yml`:
- `test-db` — отдельная Postgres, миграция `001_init.up.sql` применяется через `/docker-entrypoint-initdb.d/`.
- `api-test` — образ из `api/Dockerfile.test`, запускает `go test -count=1 -v ./...`.

`-count=1` отключает кеш тестов; `--exit-code-from api-test` — Makefile считает успехом 0 от api-test.

Локально, без Docker (если есть Go 1.22+):
```bash
cd api
TEST_DATABASE_URL="postgres://app:app@localhost:5432/test?sslmode=disable" go test ./...
```
Без `TEST_DATABASE_URL` интеграционные тесты `t.Skip(...)`.

## Что покрыто

### Unit-тесты

| Пакет                          | Файл                          | Покрывает |
|--------------------------------|-------------------------------|-----------|
| `internal/auth/password.go`    | `password_test.go`            | `HashPassword/VerifyPassword` (рандомизация соли, валидный/невалидный пароль). |
| `internal/auth/jwt.go`         | `jwt_test.go`                 | Issue/Parse access JWT, неправильный секрет, `RandomToken`+`HashToken`. |
| `internal/prodamus/prodamus.go`| `prodamus_test.go`            | `Sign/Verify` (детерминизм; `signature` ключ зачищается; невалидируется при изменении полей; `PaymentURL` содержит подпись). |
| `internal/video/token.go`      | `token_test.go`               | Roundtrip JWT видео, неправильный секрет, expired токен. |

### Integration-тесты (`internal/handlers/integration_test.go`)

Поднимают полное HTTP-приложение через `httptest.NewServer` поверх реальной Postgres. Перед каждым `setup()` — `TRUNCATE ... RESTART IDENTITY CASCADE`.

| Тест                                    | Сценарий |
|----------------------------------------|----------|
| `TestHealth`                           | `/api/health`. |
| `TestRegisterLoginMe`                  | Регистрация → логин → `/api/me` отдаёт email. |
| `TestLoginWrongPassword`               | Неверный пароль → 401. |
| `TestUnauthenticatedMe`                | `/api/me` без auth → 401. |
| `TestEnrollFreeFlow`                   | Бесплатная запись → курс появляется в `/api/me/courses`. |
| `TestPaidCheckoutRequiresVerification` | Checkout без `email_verified` → 400 `email_not_verified`. |
| `TestPaidCheckoutTestModeReturnsFakeURL` | После verify checkout возвращает URL `fake-payment`. |
| `TestCheckoutTariffPresets`            | Курс `zdorovaya-spina`: `?tariff=self` → order.AmountRub=3990, `?tariff=support` → 12990, без `?tariff` → 400 `tariff_required`, неизвестный → 400 `bad_tariff`, `?tariff=test10` обычному юзеру → 400 `bad_tariff`, админу → 200 и AmountRub=10. |
| `TestAdminEndpointsRequireAdminRole`   | Юзер без `role=admin` → 403 на `/api/admin/stats`. |
| `TestQuickSignupCreatesAndAuthenticates` | Quick-signup: 201 + `verify_required`, ДО verify — email не подтверждён, enrollment не выдан, `/api/me` 401; ПОСЛЕ `verify-email` — подтверждён, enrollment в published free, залогинен, в `/api/me/courses` только published free. |
| `TestQuickSignupExistingEmailReturnsExists` | Тот же endpoint для уже существующего email → 200 `{exists:true}` без создания сессии. |
| `TestQuickSignupRejectsBadEmail`       | Невалидный email → 400. |
| `TestQuickSignupRequiresCSRF`          | Без `X-CSRF-Token` → 403 (защита write-эндпоинта). |
| `TestAdminCanCreateCourse`             | Админ может создать курс. |
| `TestProgressEndpoint`                 | `POST /api/lessons/{id}/progress` обновляет `lesson_progress`, `CourseProgress` возвращает 1/1. |
| `TestProdamusWebhookValidSignature`    | Валидный HMAC → order переходит в `paid`, создаётся `enrollment`. |
| `TestProdamusWebhookInvalidSignature`  | Невалидный HMAC → order остаётся `pending`. |
| `TestPublicCoursesHidesPaidContent`    | Анонимный `GET /api/courses/{slug}` для платного курса не отдаёт `content_md`. |
| `TestUUIDsFormat`                      | Sanity на uuid.New(). |
| `TestAdminCRUDLessonsFlow`             | Полный цикл админ-редактора: GET admin-курса → PATCH полей курса → создание модуля → создание/чтение/обновление/удаление урока → удаление модуля. Все 200/201, БД консистентна. |
| `TestAdminLessonEndpointsForbiddenForUser` | Юзер без `role=admin` → 403 на `GET /api/admin/courses/{id}` и `POST /api/admin/lessons`. |
| `TestParseArticleHTML` (unit, `internal/seed`) | Парсер HTML-статей: достаёт `title`, `tag`, `excerpt`, `reading_minutes`, очищает back-link и cta. |
| `TestListArticlesEmpty`                | `GET /api/articles` без статей → пустой массив 200. |
| `TestArticleCRUDFlow`                  | Полный цикл: админ создаёт → публичный список и `/api/articles/{slug}` отдают → unpublish → публичный 404 → admin list виден → delete. |
| `TestArticleAdminRequiresAdmin`        | Юзер без admin → 403 на `POST /api/admin/articles`. |
| `TestArticleCreateValidatesRequiredFields` | Без `slug` → 400. |
| `TestArticleCreateRequiresCSRF`        | Без X-CSRF-Token → 403. |
| `TestParseArticleHTMLViaRepo`          | `is_published=true` без `published_at` → сервер сам ставит `now()`. |
| `TestCourseFileRequiresEnrollment`     | `GET /api/courses/zdorovaya-spina/files/metodichka.pdf`: 401 без auth, 403 без enrollment, 200 с `Content-Type: application/pdf` и валидным `%PDF-` после `repo.Grant(...)`, 404 для неизвестного файла/слага. |
| `TestRegisterRequiresConsentPD`        | `POST /auth/register` без `consent_pd=true` → 400 `consent_pd_required`, в users такой email не появился. |
| `TestQuickSignupRequiresConsentPD`     | `POST /auth/quick-signup` без `consent_pd=true` → 400 `consent_pd_required`, в users такой email не появился. |
| `TestRegisterSavesConsentTimestamps`   | При `consent_pd=true, consent_marketing=true` — `users.consent_pd_at` и `users.consent_marketing_at` не NULL. При `consent_marketing=false` — `consent_marketing_at = NULL`, `consent_pd_at` всё равно проставлен. |

## Как добавить тест

1. Unit-тест — в том же пакете, файл `*_test.go`. Не требует БД.
2. Integration-тест — в `api/internal/handlers/integration_test.go` (или соседний файл `*_test.go` в этом же пакете). Используй helpers `setup(t)` и `newClient(srv)` из текущего файла. После любого write-теста полагайся на `TRUNCATE` в начале следующего `setup()` — изоляции между тестами достаточно.
3. Покрытие нового хендлера → обнови этот файл.

## Что НЕ покрыто (известно)

- ffmpeg-конвертация видео (требует длинного фикстурного видео; smoke вручную).
- `/api/internal/video-auth` через nginx (живой smoke в e2e).
- Email-рассылка (отправка скипается без `SMTP_*`, в тестах никогда не идёт реально).
- E2E через Playwright не настроен — добавить при необходимости.
