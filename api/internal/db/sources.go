package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Метки источников трафика (миграция 018).

// SetUserSource записывает источник, если он ещё не записан (первое касание).
func (r *Repo) SetUserSource(ctx context.Context, userID uuid.UUID, source string) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE users SET source = $1 WHERE id = $2 AND source IS NULL`, source, userID)
	return err
}

func (r *Repo) SetOrderSource(ctx context.Context, orderID uuid.UUID, source string) error {
	_, err := r.Pool.Exec(ctx, `UPDATE orders SET source = $1 WHERE id = $2`, source, orderID)
	return err
}

func (r *Repo) UserSource(ctx context.Context, userID uuid.UUID) (string, error) {
	var s *string
	err := r.Pool.QueryRow(ctx, `SELECT source FROM users WHERE id = $1`, userID).Scan(&s)
	if s == nil {
		return "", err
	}
	return *s, err
}

// SourceStat – строка сводки «откуда пришли».
type SourceStat struct {
	Source     string `json:"source"`
	Signups    int    `json:"signups"`    // регистрации за период
	Subscribed int    `json:"subscribed"` // из них с согласием на письма
	Paid       int    `json:"paid"`       // оплаченных заказов за период (по метке заказа)
	Revenue    int    `json:"revenue"`    // их сумма, ₽
}

// SourceStats – сводка по источникам с момента since. Люди и заказы без
// метки собраны в строку с пустым source.
func (r *Repo) SourceStats(ctx context.Context, since time.Time) ([]SourceStat, error) {
	rows, err := r.Pool.Query(ctx, `
		WITH u AS (
			SELECT COALESCE(source, '') AS source, count(*) AS signups,
			       count(*) FILTER (WHERE consent_marketing_at IS NOT NULL) AS subscribed
			  FROM users WHERE role = 'user' AND created_at >= $1 GROUP BY 1
		), o AS (
			SELECT COALESCE(source, '') AS source, count(*) AS paid, COALESCE(sum(amount_rub), 0) AS revenue
			  FROM orders WHERE status = 'paid' AND COALESCE(paid_at, created_at) >= $1 GROUP BY 1
		)
		SELECT COALESCE(u.source, o.source), COALESCE(u.signups, 0), COALESCE(u.subscribed, 0),
		       COALESCE(o.paid, 0), COALESCE(o.revenue, 0)
		  FROM u FULL OUTER JOIN o ON o.source = u.source
		 ORDER BY 2 DESC, 5 DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceStat
	for rows.Next() {
		var s SourceStat
		if err := rows.Scan(&s.Source, &s.Signups, &s.Subscribed, &s.Paid, &s.Revenue); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
