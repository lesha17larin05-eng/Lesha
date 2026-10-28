package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AnketaReminder – оплативший группу, который ещё не прислал анкету.
type AnketaReminder struct {
	OrderID uuid.UUID
	UserID  uuid.UUID
	Email   string
	Name    string
}

// DueAnketaReminders – оплаченные заказы группы старше paidBefore, без анкеты
// и без отправленного напоминания.
func (r *Repo) DueAnketaReminders(ctx context.Context, paidBefore time.Time) ([]AnketaReminder, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT o.id, o.user_id, u.email, u.name
		  FROM orders o JOIN users u ON u.id = o.user_id
		 WHERE o.service = 'gruppa' AND o.status = 'paid'
		   AND o.paid_at <= $1 AND o.anketa_reminded_at IS NULL
		   AND NOT EXISTS (SELECT 1 FROM questionnaires q WHERE q.user_id = o.user_id)`, paidBefore)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnketaReminder
	for rows.Next() {
		var a AnketaReminder
		if err := rows.Scan(&a.OrderID, &a.UserID, &a.Email, &a.Name); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repo) MarkAnketaReminded(ctx context.Context, orderID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `UPDATE orders SET anketa_reminded_at = now() WHERE id=$1`, orderID)
	return err
}
