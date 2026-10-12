package db

import (
	"context"
	"time"
)

// Очередь повторной отправки разовых писем (миграция 021).

// OutboxEmail – письмо, которое ждёт повторной отправки.
type OutboxEmail struct {
	ID       int64
	To       string
	Subject  string
	Body     string
	Attempts int
}

const (
	outboxMaxAttempts = 15
	outboxMaxAge      = "3 days"
)

// EnqueueEmail кладёт неотправленное письмо в очередь; первая повторная
// попытка – через 5 минут.
func (r *Repo) EnqueueEmail(ctx context.Context, to, subject, body, errText string) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO email_outbox(to_addr, subject, body, last_error, next_at)
		 VALUES ($1, $2, $3, NULLIF($4, ''), now() + interval '5 minutes')`,
		to, subject, body, errText)
	return err
}

// DueEmails – письма, которым пора повторную попытку.
func (r *Repo) DueEmails(ctx context.Context, now time.Time, limit int) ([]OutboxEmail, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT id, to_addr, subject, body, attempts FROM email_outbox
		 WHERE sent_at IS NULL AND next_at <= $1 AND attempts < $2
		   AND created_at > $1::timestamptz - interval '`+outboxMaxAge+`'
		 ORDER BY next_at LIMIT $3`, now, outboxMaxAttempts, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxEmail
	for rows.Next() {
		var e OutboxEmail
		if err := rows.Scan(&e.ID, &e.To, &e.Subject, &e.Body, &e.Attempts); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkEmailSent – письмо наконец ушло.
func (r *Repo) MarkEmailSent(ctx context.Context, id int64) error {
	_, err := r.Pool.Exec(ctx, `UPDATE email_outbox SET sent_at = now(), last_error = NULL WHERE id = $1`, id)
	return err
}

// MarkEmailFailed – очередная попытка не удалась: пауза растёт на 15 минут
// с каждой попыткой, но не больше 2 часов.
func (r *Repo) MarkEmailFailed(ctx context.Context, id int64, errText string) error {
	_, err := r.Pool.Exec(ctx, `
		UPDATE email_outbox
		   SET attempts = attempts + 1, last_error = $2,
		       next_at = now() + least(attempts * interval '15 minutes', interval '2 hours')
		 WHERE id = $1`, id, errText)
	return err
}

// OutboxPending – сколько писем ждут повторной отправки (для админки).
func (r *Repo) OutboxPending(ctx context.Context) (int, error) {
	var n int
	err := r.Pool.QueryRow(ctx, `
		SELECT count(*) FROM email_outbox
		 WHERE sent_at IS NULL AND attempts < $1 AND created_at > now() - interval '`+outboxMaxAge+`'`,
		outboxMaxAttempts).Scan(&n)
	return n, err
}
