package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Questionnaire – анкета перед «Точкой перемен» (миграция 020).
type Questionnaire struct {
	OrderID     uuid.UUID         `json:"order_id"`
	OrderNum    int64             `json:"order_num"`
	Answers     map[string]string `json:"answers"`
	SubmittedAt time.Time         `json:"submitted_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// SaveQuestionnaire создаёт или перезаписывает анкету заказа.
func (r *Repo) SaveQuestionnaire(ctx context.Context, orderID, userID uuid.UUID, answers map[string]string) error {
	b, err := json.Marshal(answers)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx, `
		INSERT INTO questionnaires(order_id, user_id, answers, health_consent_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (order_id) DO UPDATE
		   SET answers = EXCLUDED.answers, health_consent_at = now(), updated_at = now()`,
		orderID, userID, b)
	return err
}

// GetQuestionnaire – анкета заказа или ErrNotFound.
func (r *Repo) GetQuestionnaire(ctx context.Context, orderID uuid.UUID) (*Questionnaire, error) {
	q := &Questionnaire{}
	var raw []byte
	err := r.Pool.QueryRow(ctx, `
		SELECT q.order_id, o.order_num, q.answers, q.submitted_at, q.updated_at
		  FROM questionnaires q JOIN orders o ON o.id = q.order_id
		 WHERE q.order_id = $1`, orderID).Scan(&q.OrderID, &q.OrderNum, &raw, &q.SubmittedAt, &q.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &q.Answers)
	return q, nil
}

// QuestionnairesByUser – все анкеты человека, новые сверху (для админки).
func (r *Repo) QuestionnairesByUser(ctx context.Context, userID uuid.UUID) ([]Questionnaire, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT q.order_id, o.order_num, q.answers, q.submitted_at, q.updated_at
		  FROM questionnaires q JOIN orders o ON o.id = q.order_id
		 WHERE q.user_id = $1 ORDER BY q.submitted_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Questionnaire
	for rows.Next() {
		var q Questionnaire
		var raw []byte
		if err := rows.Scan(&q.OrderID, &q.OrderNum, &raw, &q.SubmittedAt, &q.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &q.Answers)
		out = append(out, q)
	}
	return out, rows.Err()
}

// PaidServiceOrdersByUser – id оплаченных заказов услуг (для ссылок на анкету в админке).
func (r *Repo) PaidServiceOrdersByUser(ctx context.Context, userID uuid.UUID) ([]Order, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT id, order_num, service FROM orders
		 WHERE user_id = $1 AND service IS NOT NULL AND status = 'paid' ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.OrderNum, &o.Service); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
