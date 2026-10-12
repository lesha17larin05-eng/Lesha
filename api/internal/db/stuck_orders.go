package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// StuckOrder – оплата, по которой покупатель вернулся «успешно», а
// подтверждения от Продамуса нет.
type StuckOrder struct {
	ID          uuid.UUID
	OrderNum    int64
	AmountRub   int
	Email       string
	Name        string
	CourseSlug  string // пусто – услуга
	CourseTitle string
	Service     string
	ReturnedAt  time.Time
}

// MarkOrderReturned – покупатель вернулся с оплаты (первый раз). Только для
// заказов, которые ещё ждут подтверждения.
func (r *Repo) MarkOrderReturned(ctx context.Context, id uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE orders SET returned_at = now() WHERE id=$1 AND status='pending' AND returned_at IS NULL`, id)
	return err
}

// StuckOrders – вернулись с оплаты раньше, чем before, подтверждения нет,
// уведомление ещё не отправлялось.
func (r *Repo) StuckOrders(ctx context.Context, before time.Time) ([]StuckOrder, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT o.id, o.order_num, o.amount_rub, u.email, COALESCE(u.name,''),
		       COALESCE(c.slug,''), COALESCE(c.title,''), COALESCE(o.service,''), o.returned_at
		FROM orders o
		JOIN users u ON u.id = o.user_id
		LEFT JOIN courses c ON c.id = o.course_id
		WHERE o.status='pending' AND o.stuck_alerted_at IS NULL
		  AND o.returned_at IS NOT NULL AND o.returned_at < $1
		ORDER BY o.returned_at`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StuckOrder
	for rows.Next() {
		var s StuckOrder
		if err := rows.Scan(&s.ID, &s.OrderNum, &s.AmountRub, &s.Email, &s.Name,
			&s.CourseSlug, &s.CourseTitle, &s.Service, &s.ReturnedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkStuckAlerted – уведомление о зависшей оплате отправлено.
func (r *Repo) MarkStuckAlerted(ctx context.Context, id uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `UPDATE orders SET stuck_alerted_at = now() WHERE id=$1`, id)
	return err
}

// WasStuckAlerted – по заказу уже предупреждали, что подтверждения нет.
func (r *Repo) WasStuckAlerted(ctx context.Context, id uuid.UUID) bool {
	var ok bool
	_ = r.Pool.QueryRow(ctx, `SELECT stuck_alerted_at IS NOT NULL FROM orders WHERE id=$1`, id).Scan(&ok)
	return ok
}
