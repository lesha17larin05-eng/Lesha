package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Questionnaire – анкета перед «Точкой перемен» (миграция 020), одна на человека.
type Questionnaire struct {
	Answers     map[string]string `json:"answers"`
	SubmittedAt time.Time         `json:"submitted_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// SaveQuestionnaire создаёт или перезаписывает анкету человека.
func (r *Repo) SaveQuestionnaire(ctx context.Context, userID uuid.UUID, answers map[string]string) error {
	b, err := json.Marshal(answers)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx, `
		INSERT INTO questionnaires(user_id, answers, health_consent_at)
		VALUES ($1, $2, now())
		ON CONFLICT (user_id) DO UPDATE
		   SET answers = EXCLUDED.answers, health_consent_at = now(), updated_at = now()`,
		userID, b)
	return err
}

// GetQuestionnaire – анкета человека или ErrNotFound.
func (r *Repo) GetQuestionnaire(ctx context.Context, userID uuid.UUID) (*Questionnaire, error) {
	q := &Questionnaire{}
	var raw []byte
	err := r.Pool.QueryRow(ctx,
		`SELECT answers, submitted_at, updated_at FROM questionnaires WHERE user_id = $1`, userID).
		Scan(&raw, &q.SubmittedAt, &q.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &q.Answers)
	return q, nil
}
