# Платежи и видео

## Продамус

Документация платформы: https://help.prodamus.ru/payform/integracii/rest-api

### Создание ссылки на оплату

`POST /api/courses/{slug}/checkout`:
1. Проверяем `email_verified_at != NULL`. Если нет → 400 `email_not_verified`.
2. Проверяем нет ли уже enrollment → 400 `already_enrolled`.
3. `INSERT INTO orders (..., status='pending')` — `order_num BIGSERIAL` даёт человекочитаемый номер.
4. Формируем параметры платёжной страницы (см. `api/internal/handlers/payments.go`):
   - `do=pay`, `order_id=<UUID>`, `order_num=<bigint>`, `customer_email`, `products[0][name|price|quantity]`,
   - `urlReturn`, `urlSuccess`, `urlNotification` — все собираются от `APP_HOST`.
5. Подпись HMAC-SHA256 через `prodamus.Sign(secret, params)`:
   - убирает поля `signature`/`sign` из map (рекурсивно),
   - сериализует **canonical JSON** с отсортированными ключами и без HTML-эскейпа (`SetEscapeHTML(false)`),
   - HMAC-SHA256 → hex.
6. `prodamus.PaymentURL(...)` собирает финальный URL с подставленной `signature=...`.
7. Если `PRODAMUS_TEST_MODE=true` → возвращаем `/api/dev/fake-payment?order_id=...` вместо реального URL.

### Webhook `/api/webhooks/prodamus`

Handler в `payments.go::ProdamusWebhook`:
1. Читаем сырое тело **до** парсинга, сохраняем заголовки и body в `payment_webhooks` (raw — для аудита).
2. Достаём `Sign` из заголовка (или `signature` из body).
3. Парсим тело: `application/json` или `application/x-www-form-urlencoded`.
4. **Проверка подписи**: `prodamus.Verify` использует `hmac.Equal` (constant-time).
5. Если подпись невалидна → пишем `signature_valid=false`, отвечаем 200 без обработки (чтобы Продамус не ретраил, но не давать продакшен-секрету угадываться по разнице ответов).
6. Если валидна:
   - находим `order` по `order_id` (UUID, наш) или `order_num` (bigint).
   - Если `payment_status in ('success','paid')` и `order.status='pending'`:
     - `UPDATE orders SET status='paid', paid_at=now()` + `INSERT INTO enrollments (granted_by='purchase')` (на случай гонки — `ON CONFLICT DO NOTHING`).
     - Шлём письмо «Доступ к курсу открыт».
   - Если уже `paid` — no-op (идемпотентно).
7. Отвечаем 200 всегда (после валидной подписи), иначе будут бесконечные ретраи.

### Тестовый режим

`APP_ENV != production` + `PRODAMUS_TEST_MODE=true`:
- Checkout возвращает `/api/dev/fake-payment?order_id=...`.
- При переходе по этому URL хендлер `FakePayment` сразу маркирует order как paid и создаёт enrollment.

### Защита подписи в тестах

`api/internal/prodamus/prodamus_test.go` покрывает: детерминизм подписи, зачистку поля `signature`, инвалидация при изменении данных, наличие подписи в URL.

## Видео — HLS + X-Accel-Redirect

### Загрузка

`POST /api/admin/videos/upload` (multipart, `file`):
1. `parseMultipartForm(64MB)` — заголовок и forms идут в память, тело файла стримится.
2. `INSERT INTO videos (status='uploading')`.
3. Пишем raw файл в `/var/videos/{video_id}/source.<ext>` (внутри контейнера; на хосте — `data/videos/...`).
4. Возвращаем 202 с `{id, status:'processing'}`.
5. **В фоне** (goroutine) запускаем `ffmpeg`:
   ```
   ffmpeg -y -i source.<ext> -codec:v libx264 -codec:a aac \
     -hls_time 10 -hls_playlist_type vod \
     -hls_segment_filename seg_%03d.ts -f hls index.m3u8
   ```
6. По завершению — `UPDATE videos SET status='ready', hls_master_playlist='index.m3u8', duration_sec=...`.

### Защищённая отдача

1. Юзер открывает урок → JS делает `GET /api/videos/{id}/playback`.
2. Backend проверяет: видео `status='ready'`; находит курс владельца через `lessons.video_id`; если курс платный — проверяет `enrollment` (или `role=admin`).
3. Возвращает `{ playback_url: "/video-stream/{vid}/index.m3u8?token=<JWT>" }`. JWT подписан `VIDEO_TOKEN_SECRET`, claims: `vid`, `uid`, `exp` (TTL `VIDEO_TOKEN_TTL`, по умолчанию 2 часа).
4. Плеер (hls.js) делает запросы на `/video-stream/...`.
5. nginx (`location /video-stream/`) переписывает путь на `/api/internal/video-auth` (`rewrite ... break`) и `proxy_pass http://api;`, прокидывая оригинальный URI в `X-Original-URI`.
6. Go-хендлер `InternalVideoAuth` извлекает path из `X-Original-URI`, берёт токен из query (для первого запроса манифеста) **или из cookie `vt_<vid>`** (для последующих сегментов), валидирует JWT (`video.ParseToken`), проверяет что `claims.VideoID == path-видео`. На запросах `*.m3u8` дополнительно ставит httpOnly-cookie `vt_<vid>` со scope `Path=/video-stream/<vid>/` и `Max-Age=VIDEO_TOKEN_TTL`, чтобы плеер мог тянуть сегменты `.ts` без токена в URL. Возвращает 200 + заголовок `X-Accel-Redirect: /protected-videos/{vid}/{file}`.
7. nginx видит `X-Accel-Redirect` → внутренне переходит в `location /protected-videos/` (помечен `internal`, alias `/var/videos/`) → отдаёт файл с диска.

Преимущества: Go валидирует только токен (микросекунды), nginx гонит мегабайты с диска. Прямой `GET /protected-videos/...` извне → 404 (internal).

### Защита от копирования

Базовый уровень — TTL JWT привязанный к user_id. Полная защита от копирования невозможна на HLS, но 95% случаев расшаривания это закрывает. Опционально — добавить watermark с email юзера поверх плеера (CSS overlay).
