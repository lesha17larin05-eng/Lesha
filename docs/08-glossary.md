# Глоссарий

| Термин            | Что это                                                                 |
|-------------------|-------------------------------------------------------------------------|
| **Курс**          | Запись в `courses`. `kind = free` (доступ за один клик) или `paid` (через Продамус). |
| **Модуль**        | Группа уроков внутри платного курса (`modules`).                       |
| **Урок**          | Запись в `lessons`: текст (markdown) + опц. видео.                     |
| **Превью-урок**   | `lessons.is_preview = TRUE` — пробный, доступен без оплаты.            |
| **Enrollment**    | Запись в `enrollments`: связка юзер ↔ курс. `granted_by` = `purchase`/`admin`/`free`. |
| **Order**         | Заказ в `orders`. Статусы: `pending` → `paid`/`failed`/`refunded`.     |
| **Прогресс**      | `lesson_progress`: `completed_at` + `last_position_sec` для видео.     |
| **Argon2id**      | Алгоритм хеширования паролей. Параметры: `t=2, m=64MiB, p=4, keyLen=32, saltLen=16`. |
| **Access JWT**    | Короткоживущий токен (15 мин) в httpOnly cookie `access_token`.        |
| **Refresh token** | 32-байтный random hex, хеш в `sessions`, TTL 30 дней, в cookie `refresh_token` на `/api/auth`. |
| **CSRF token**    | Random hex в cookie `csrf` (НЕ httpOnly). На не-GET клиент шлёт `X-CSRF-Token`. |
| **Видео-JWT**     | Короткоживущий signed URL (TTL 2h) с claims `vid`/`uid`. Валидируется в `/api/internal/video-auth`. |
| **HLS**           | Apple HTTP Live Streaming. ffmpeg нарезает MP4 → `index.m3u8` + `seg_*.ts`. |
| **X-Accel-Redirect** | Заголовок ответа Go, указывающий nginx внутренне отдать файл из `internal` location. |
| **Bind mount**    | Каталог хоста, примонтированный в контейнер (vs named volume). Используем для всех данных. |
| **Прoдамус**      | Российский провайдер платежей (payform). Подпись HMAC-SHA256 над canonical JSON. |
| **Сидер**         | `api/internal/seed/seed.go` — идемпотентно создаёт админа, юзера, free + paid курс. Запускается при старте api. |
| **Audit log**     | `audit_log` таблица — все write-действия админки (`grant`, `revoke`, `create/update/delete course`). |
