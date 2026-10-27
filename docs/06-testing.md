# Тестирование

## Как запускать

```bash
make test         # поднимает test-стек и прогоняет все Go-тесты в контейнере
make test-clean   # сносит test-стек, тома и сети
```

`make test` использует `docker-compose.test.yml`:
- `test-db` — отдельная Postgres, схемные миграции (`*.up.sql`, кроме контентных 006–008) применяются через `/docker-entrypoint-initdb.d/`. **Новую миграцию добавляй и в `docker-compose.test.yml`** – иначе тесты падают с `relation ... does not exist` (так было до 2026-10-23: 017–025 не подключили, падали почти все тесты).
- Прогон на сервере, не трогая прод: скопировать `api/` и `docker-compose.test.yml` в `/root/lesha-test` и запустить `docker compose -p lesha-test -f docker-compose.test.yml up --build --abort-on-container-exit --exit-code-from api-test` (отдельное имя проекта `-p lesha-test` обязательно).
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
| `TestResendVerification`               | Resend: 200 + новый токен для неподтверждённого; 200 и для неизвестного email. |
| `TestAdminActivity`                    | Журнал занятий: admin-only, содержит email/урок/completed_at, фильтр по курсу отсекает чужое. |
| `TestAdminUsersExportCSV`              | Выгрузка admin-only; в CSV только согласившиеся на рассылку; Content-Type text/csv. |
| `TestAdminUsersFilters`                | /api/admin/users: `?course=` отбирает только с доступом, `?verified=0/1` — по подтверждению email, комбинация работает. |
| `TestAdminUserCard`                    | Карточка пользователя: закрыта для не-админа; админ видит профиль (согласие ПД не null), доступ (`granted_by=free`), прогресс 1/2 (50%), список уроков, заказ 990 ₽. |
| `TestHoneypotForms`                   | Заполненное поле-ловушка `homepage`: /api/leads и quick-signup отвечают 200, но ничего не создают; пустая ловушка – обычный 201. Плюс unit `TestHoneypot` в middleware (тело запроса доходит до хендлера нетронутым). |
| `TestPeople`                           | «Люди»: ручное добавление (400 без имени/с плохой датой, 409 на дубль контакта без учёта регистра), PATCH статуса/даты/заметки и очистка шага, 400 на неизвестный интерес, массовое добавление (разделители, маркеры, телефон без имени, пропуск дублей), CSV с BOM, всё закрыто для не-админов. `TestServiceCheckout` дополнительно проверяет: оплата услуги → запись в «Людях» со статусом «купил». |
| `TestLeadsFlow`                        | POST /api/leads: 400 без согласия/имени, 201 при успехе (неизвестный source → `other`, status=new), заявка со страницы «Точка перемен» сохраняет `source=start`; /api/admin/leads закрыт для юзера, доступен админу; PATCH статуса работает, невалидный статус → 400. |
| `TestCheckoutTariffPresets`            | Курс `zdorovaya-spina`: `?tariff=self` → order.AmountRub=3990, `?tariff=support` → 12990, без `?tariff` → 400 `tariff_required`, неизвестный → 400 `bad_tariff`, `?tariff=test10` обычному юзеру → 400 `bad_tariff`, админу → 200 и AmountRub=10. |
| `TestZhonglirovanieTariffsAndDraft`    | Курс `zhonglirovanie`: черновик – checkout 404 и `GET /api/courses/zhonglirovanie` 404; после публикации `self` → 1490, `support` → 3990, без `?tariff` → 400 `tariff_required` (время зафиксировано после акции). |
| `TestQuickCheckoutAndPromo`            | Покупка в один шаг: во время акции `self` → 990, покупатель создаётся с почтой в нижнем регистре, `tariffs` отдаёт price/base_price/promo_until; после дедлайна → 1490; 400 на кривую почту, пустое имя, без согласия, неизвестный тариф; 403 без CSRF; 409 при уже выданном доступе; 404 для черновика. |
| `TestOrderStatus`                      | Статус заказа для страницы после оплаты: `pending` → `paid`, почта замаскирована (`gre•••@…`); кривой и чужой id – 404. |
| `TestStuckPaymentAlert`                | Зависшая оплата: возврат «успешно» (`?returned=1`), через 5 минут уведомления нет, через 11 – одно (повторный проход не дублирует) с почтой, курсом и суммой; позже пришедший webhook даёт сообщение «подтверждение пришло с опозданием». |
| `TestAbandonedPaymentAlert`            | Брошенная оплата: через 30 минут тихо, через час – по одному уведомлению «Не завершил оплату»; при повторной попытке – только по последнему заказу; вернувшийся с оплаты (`returned=1`) не попадает; заказы старше суток не поднимаются. |
| `TestLoginByCode`                      | Код на почту: на неизвестный адрес ответ тот же, без кода; повтор в течение минуты – без нового кода; неверный код не логинит, верный (с пробелом внутри) – логинит, второй раз не работает; после 5 ошибок – `too_many_attempts` даже с верным; без CSRF – 403. |
| `TestAccessEmailPasswordAndLink`       | Письмо о доступе: человеку без известного пароля – новый пароль (им можно войти), кнопка входа (многоразовая, ведёт на `/cabinet/<slug>`, просроченная → 400) и предложение подписаться; после входа второе письмо пароль не меняет. |
| `TestBuyerEntersAfterPayment`          | Вход сразу после оплаты: до оплаты – на логин; `can_enter` только в браузере покупателя; чужой браузер и подделанная кука → логин с почтой; покупатель → `/cabinet/zhonglirovanie` и залогинен. Галочка в форме сохраняет согласие на рассылку. |
| `TestUnsubscribedIsRemembered`         | Отписка ставит `unsubscribed_at`: отписавшемуся не предлагаем подписаться, в группу `service_all` попадает только не дававший согласия; новое согласие снимает отметку. |
| `TestDripLetterWithoutConsent`         | Письмо цепочки без согласия – без блока `[[promo]]` (отзывы) и с кнопкой подписки; подписанному – с отзывами и без кнопки. |
| `TestResetPasswordLogsIn`              | Сброс пароля по токену подтверждает почту и сразу ставит cookies сессии (`/api/me` → 200). |
| `TestAdminEndpointsRequireAdminRole`   | Юзер без `role=admin` → 403 на `/api/admin/stats`. |
| `TestQuickSignupCreatesAndAuthenticates` | Quick-signup: 201 + `access_granted`, почта не подтверждена, но enrollment в published free выдан сразу и `/api/me` 200 (`can_fix_email:true`); в `/api/me/courses` только published free. Ссылка из письма (`verify-email`) подтверждает почту и логинит в новом браузере. |
| `TestFixEmailAfterQuickSignup` | `fix-email`: аноним 401, кривой адрес 400, занятый 409, исправление меняет email и пароль (вход с новыми данными), после подтверждения почты – 409. |
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

| `TestSiteSettings`                     | Флаги сайта: публичный `GET /api/settings` без auth отдаёт `salut_visible:false` по умолчанию; не-админ на `PATCH /api/admin/settings` не проходит; неизвестный ключ и пустое тело → 400; админ включает → публичный GET отражает, в `audit_log` ровно одна запись `settings_update`; выключение возвращает `false`. |
| `TestAnketa` | Анкета (одна на человека): плохая подпись → 400; GET отдаёт имя и вопросы; без согласия – 400 и сразу список незаполненных; без обязательных – 400; неизвестные ключи не сохраняются; повторная отправка перезаписывает; админ получает ссылку по email (новый человек заводится) и по user_id с отправкой письма, аноним – нет, кривой email – 400. |
| `TestTrafficSource` | Кука `src` при quick-signup пишется в `users.source` (в т.ч. url-кодированная `ref%3Ainstagram`), мусор отбрасывается; `/api/admin/sources` отдаёт сводку админу и закрыт для анонима. |
| `TestDripChain` | Автоцепочка: в день регистрации и до 9:00 МСК – ничего; через час после регистрации – ничего, через 3 часа (день 0) – письмо 1 подписанному новичку и новичку без согласия (отписавшийся и зарегистрированные до `drip_start_at` – мимо); не чаще раза в день; шаги по порядку на дни 3, 5; купившему «Здоровую спину» письма 4–5 – `skipped`; без согласия письмо 4 не уходит, после «Да, присылайте» – уходит; `drip_enabled=false` – тишина. |
| `TestEmailRetry` | Повтор почты: шаг цепочки `failed` повторяется не раньше чем через 30 минут и становится `sent` (attempts=2), отправленный шаг не перезаписывается; письмо в `email_outbox` ждёт 5 минут и уходит; старше 3 суток – не повторяется. Время отправки задаётся от того же `now`, что получает воркер (тест не зависит от времени суток). |
| `TestTelegramNotifications` | Уведомления в Telegram (тестовая прокладка `testRelay`): регистрация на «Мягкий старт» с меткой источника и пометкой «без согласия», заявка с сайта (HTML превращается в текст). |
| `TestGruppa` | Групповое ведение: анкета `k=gruppa` со своими вопросами и замерами (обязательная задержка дыхания), вид сохраняется и подхватывается ссылкой без `k`, админка показывает ответы по вопросам группы; счётчик мест `GET /api/services/gruppa/seats` (7 из 7 → 0, у `start` – 404); оплата 9 900 ₽, после 7 оплаченных – `409 sold_out`; заявка `source=gruppa` – в Telegram ссылка `/gruppa#oplata`. |
| `TestRenderDripBody`, `TestDripStepsRender` | Разметка писем цепочки (списки, цитата, кнопки, ссылки, имя) и что во всех 5 текстах нет сырой разметки и есть подпись. |

| `TestUnsubscribe`                      | Отписка в два шага: `GET` по ссылке → 303 на `/unsubscribe?u=&t=`, согласие на месте; `status` с плохой подписью → 400, с валидной → `subscribed:true`; `POST` с причиной снимает `consent_marketing_at` (ПД остаётся) и пишет причину/комментарий, неизвестная причина не пишется; `undo` с плохой подписью → 400, с валидной – подписка вернулась; one-click `POST` без `X-CSRF-Token` → 200, с плохой подписью → 400. |

| `TestEmailClick`                       | Переход по ссылке из письма: подписанная ссылка → 302 на страницу, записаны клик и открытие; подменённая ссылка → 302 на `/` без записи; ссылки в письме цепочки идут через `/api/c`. |
| `TestEmailPixel`                       | Счётчик открытий: с плохой подписью картинка отдаётся (200, `image/gif`, реальный GIF), но открытие не пишется; с валидной — пишется, повторное открытие даёт вторую строку; `GET /api/admin/email-opens` закрыт от обычного пользователя и отдаёт `people:1` админу. |

| `TestProdamusWebhookOrderNumCarriesUUID` | Реальный формат Продамуса: наш UUID в `order_num`, их номер в `order_id` → заказ становится `paid`, enrollment выдаётся. |
| `TestProdamusWebhookHumanOrderNum`     | Наш человекочитаемый `orders.order_num` в поле `order_num` → заказ становится `paid`. |
| `TestProdamusWebhookNoOrderStaysSafe`  | Оплата по ручной ссылке (в `order_num` имя покупателя): 200 Продамусу, вебхук помечен `no_order`, ничего не выдаётся. |

| `TestCampaignsFlow`                    | Рассылки: группа «все подписанные» считает только согласившихся; не-админ не создаёт рассылку; неизвестная группа и пустые поля → 400; создание фиксирует получателей; карточка отдаёт список; старт/пауза работают, неизвестное действие → 400; отписавшийся после старта не попадает в очередь (`NextRecipient` его пропускает). |

| `TestSubscribeFlow`                    | Согласие на рассылку: подделанная подпись не подписывает (303 на `/subscribed?error=1`); валидная ссылка ставит согласие; повторное нажатие не двигает дату первого согласия; `PATCH /api/me {consent_marketing}` снимает и возвращает галочку; `GET /api/me` отдаёт состояние. |
| `TestServiceSegment`                   | Сервисная группа: человек с платным курсом без согласия не попадает в `all`, попадает в `service_paid` (`service_only: true`); рассылка по этой группе создаётся, адресат выдаётся `NextRecipient` с `Subscribed = false`. |

| `TestProdamusRefundClosesAccess`       | Возврат: заказ становится `refunded`, доступ с `granted_by='purchase'` закрывается, подаренный доступ к другому курсу остаётся. |

## Как добавить тест

1. Unit-тест — в том же пакете, файл `*_test.go`. Не требует БД.
2. Integration-тест — в `api/internal/handlers/integration_test.go` (или соседний файл `*_test.go` в этом же пакете). Используй helpers `setup(t)` и `newClient(srv)` из текущего файла. После любого write-теста полагайся на `TRUNCATE` в начале следующего `setup()` — изоляции между тестами достаточно.
3. Покрытие нового хендлера → обнови этот файл.

## Что НЕ покрыто (известно)

- ffmpeg-конвертация видео (требует длинного фикстурного видео; smoke вручную).
- `/api/internal/video-auth` через nginx (живой smoke в e2e).
- Email-рассылка (отправка скипается без `SMTP_*`, в тестах никогда не идёт реально).
- E2E через Playwright не настроен — добавить при необходимости.

| `TestArticleViewCounter`               | Счётчик чтения статей: маяк принимается без `X-CSRF-Token` и отвечает 204 на `open`/`read`/`cta`; неизвестное событие и несуществующий slug тоже 204, но не считаются; `/api/admin/article-stats` закрыт для обычного пользователя; `ArticleStats` возвращает opens=2, reads=1, cta=1 и непустой `last_open`. |

## Осторожно: роутер в тестах — копия main.go

`setup(t)` в `integration_test.go` собирает **свой** `chi.Router`, повторяя маршруты из `api/cmd/server/main.go`. Добавили эндпоинт в main.go — продублируйте его и в `setup`, иначе тест получит 404 на живом коде. Там же список таблиц для `TRUNCATE` — новую таблицу надо дописать и туда.

| `TestNewsletterSignup`                 | Подписка с сайта: без галочки ПД и с кривым адресом — 400; валидная заявка заводит пользователя и ставит `consent_pd_at`, но **не** `consent_marketing_at`; ссылка из письма (`GET /api/subscribe`) включает рассылку; повторная отправка на тот же адрес не создаёт второго пользователя. |

| `TestServiceCheckout`                  | Оплата «Точки перемен»: без согласия, с кривым адресом, без имени и с неизвестной услугой — 400; валидная заявка заводит покупателя с именем и создаёт заказ `service='start'`, `course_id IS NULL`, 2 990 ₽, `pending`; вебхук переводит заказ в `paid` и при этом **не** создаёт ни одного enrollment. |

| `TestAdminRefundOrder`                 | Ручной возврат: не-админу закрыт; админ переводит `paid` → `refunded`, купленный доступ закрывается (`access_revoked:true`), в `audit_log` одна запись `refund`; повторный возврат — 400. Заказ на услугу (`course_id IS NULL`) виден в `GET /api/admin/orders`. |
| `TestAdminDeleteUser`                  | Удаление пользователя из админки: брошенные заказы (`pending`, `failed`) удаляются вместе с ним; с оплаченным заказом или возвратом – 409, пользователь и заказ остаются. |

Unit: `internal/email` – запасной ящик на фейковом SMTP, `OnAsyncFail`, `Health` (начало сбоя, сброс после успеха); `internal/notify` – HTML → текст, отправка в прокладку; `handlers.TestMailMonitor` – когда слать «письма не отправляются» / «снова работает».
