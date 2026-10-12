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
	ReturnedAt  time.Time // StuckOrders: когда вернулся с оплаты
	CreatedAt   time.Time // AbandonedOrders: когда перешёл к оплате
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

// AbandonedOrders – заказы, созданные в окне [from, to), которые так и не
// оплачены, покупатель не вернулся с оплаты (иначе это «зависшая» оплата),
// и после которых тот же человек не делал новых попыток купить то же самое
// (повторный заказ – значит, вернулся сам).
func (r *Repo) AbandonedOrders(ctx context.Context, from, to time.Time) ([]StuckOrder, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT o.id, o.order_num, o.amount_rub, u.email, COALESCE(u.name,''),
		       COALESCE(c.slug,''), COALESCE(c.title,''), COALESCE(o.service,''), o.created_at
		FROM orders o
		JOIN users u ON u.id = o.user_id
		LEFT JOIN courses c ON c.id = o.course_id
		WHERE o.status='pending' AND o.abandoned_alerted_at IS NULL AND o.returned_at IS NULL
		  AND o.created_at >= $1 AND o.created_at < $2
		  AND NOT EXISTS (
		    SELECT 1 FROM orders o2
		    WHERE o2.user_id = o.user_id AND o2.id <> o.id
		      AND o2.course_id IS NOT DISTINCT FROM o.course_id
		      AND o2.service IS NOT DISTINCT FROM o.service
		      AND (o2.created_at > o.created_at OR o2.status = 'paid'))
		  AND NOT EXISTS (
		    SELECT 1 FROM enrollments e WHERE e.user_id = o.user_id AND e.course_id = o.course_id)
		ORDER BY o.created_at`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StuckOrder
	for rows.Next() {
		var s StuckOrder
		if err := rows.Scan(&s.ID, &s.OrderNum, &s.AmountRub, &s.Email, &s.Name,
			&s.CourseSlug, &s.CourseTitle, &s.Service, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkAbandonedAlerted – уведомление о брошенной оплате отправлено.
func (r *Repo) MarkAbandonedAlerted(ctx context.Context, id uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `UPDATE orders SET abandoned_alerted_at = now() WHERE id=$1`, id)
	return err
}
