package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Автоцепочка писем после «Мягкого старта». Логика выбора адресата – здесь,
// тексты и отправка – в handlers/drip.go.

// DripRecipient – кому отправить очередной шаг цепочки.
type DripRecipient struct {
	UserID uuid.UUID
	Email  string
	Name   string
}

// DripStartAt – момент запуска цепочки (из миграции 017). Люди,
// зарегистрированные раньше, цепочку не получают.
func (r *Repo) DripStartAt(ctx context.Context) (time.Time, error) {
	var v string
	err := r.Pool.QueryRow(ctx, `SELECT value FROM site_settings WHERE key='drip_start_at'`).Scan(&v)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, v)
}

// NextDripRecipient – один человек, которому пора отправить шаг step:
//   - есть доступ к «Мягкому старту» и согласие на рассылку;
//   - зарегистрирован после запуска цепочки;
//   - с регистрации прошло не меньше day календарных дней (по Москве);
//   - этот шаг ещё не обработан, предыдущий – обработан (идём по порядку);
//   - за последние 20 часов ему не уходило письмо цепочки (не чаще раза в день).
func (r *Repo) NextDripRecipient(ctx context.Context, step, day int, startAt, now time.Time) (*DripRecipient, error) {
	rec := &DripRecipient{}
	err := r.Pool.QueryRow(ctx, `
		SELECT u.id, u.email, COALESCE(u.name, '')
		  FROM users u
		  JOIN enrollments e ON e.user_id = u.id
		  JOIN courses c ON c.id = e.course_id AND c.slug = 'myagkiy-start'
		 WHERE u.role = 'user'
		   AND u.consent_marketing_at IS NOT NULL
		   AND u.created_at >= $3
		   AND ($4::timestamptz AT TIME ZONE 'Europe/Moscow')::date
		       - (u.created_at AT TIME ZONE 'Europe/Moscow')::date >= $2
		   AND NOT EXISTS (SELECT 1 FROM drip_sends d WHERE d.user_id = u.id AND d.step = $1)
		   AND ($1 = 1 OR EXISTS (SELECT 1 FROM drip_sends d WHERE d.user_id = u.id AND d.step = $1 - 1))
		   AND NOT EXISTS (SELECT 1 FROM drip_sends d WHERE d.user_id = u.id AND d.status = 'sent'
		                     AND d.sent_at > $4::timestamptz - interval '20 hours')
		 ORDER BY u.created_at
		 LIMIT 1`, step, day, startAt, now).Scan(&rec.UserID, &rec.Email, &rec.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rec, err
}

// HasPaidProduct – у человека уже есть «Здоровая спина» (куплена или выдана)
// или оплачена «Точка перемен». Таким письма с предложением не шлём.
// Другие платные курсы (жонглирование) не считаются – они про другое.
func (r *Repo) HasPaidProduct(ctx context.Context, userID uuid.UUID) (bool, error) {
	var ok bool
	err := r.Pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM enrollments e JOIN courses c ON c.id = e.course_id
		                WHERE e.user_id = $1 AND c.slug = 'zdorovaya-spina')
		    OR EXISTS (SELECT 1 FROM orders o
		                WHERE o.user_id = $1 AND o.status = 'paid' AND o.service IS NOT NULL)`,
		userID).Scan(&ok)
	return ok, err
}

// RecordDrip фиксирует результат шага (sent / skipped / failed).
func (r *Repo) RecordDrip(ctx context.Context, userID uuid.UUID, step int, status, errText string) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO drip_sends(user_id, step, status, error) VALUES ($1, $2, $3, NULLIF($4, ''))
		 ON CONFLICT (user_id, step) DO NOTHING`, userID, step, status, errText)
	return err
}

// DripSentLast24h – сколько писем цепочки ушло за сутки (общий лимит ящика).
func (r *Repo) DripSentLast24h(ctx context.Context) (int, error) {
	var n int
	err := r.Pool.QueryRow(ctx,
		`SELECT count(*) FROM drip_sends WHERE status = 'sent' AND sent_at > now() - interval '24 hours'`).Scan(&n)
	return n, err
}

// DripStepStats – сводка по шагу для админки.
type DripStepStats struct {
	Step    int `json:"step"`
	Sent    int `json:"sent"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
	Opened  int `json:"opened"` // людей, открывших письмо (по пикселю)
}

func (r *Repo) DripStats(ctx context.Context, steps int) ([]DripStepStats, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT s.step,
		       count(d.*) FILTER (WHERE d.status = 'sent'),
		       count(d.*) FILTER (WHERE d.status = 'skipped'),
		       count(d.*) FILTER (WHERE d.status = 'failed'),
		       (SELECT count(DISTINCT o.user_id) FROM email_opens o WHERE o.campaign = 'drip-' || s.step)
		  FROM generate_series(1, $1) AS s(step)
		  LEFT JOIN drip_sends d ON d.step = s.step
		 GROUP BY s.step ORDER BY s.step`, steps)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DripStepStats
	for rows.Next() {
		var s DripStepStats
		if err := rows.Scan(&s.Step, &s.Sent, &s.Skipped, &s.Failed, &s.Opened); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
