package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SaveUnsubscribeReason – причина отписки со страницы /unsubscribe.
func (r *Repo) SaveUnsubscribeReason(ctx context.Context, userID uuid.UUID, reason, comment string) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO unsubscribe_reasons(user_id, reason, comment) VALUES ($1, $2, NULLIF($3, ''))`,
		userID, reason, comment)
	return err
}

// UnsubscribeReasonCount – сколько раз выбрали причину за период.
type UnsubscribeReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// UnsubscribeComment – свободный комментарий к отписке.
type UnsubscribeComment struct {
	Reason    string    `json:"reason"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

func (r *Repo) UnsubscribeStats(ctx context.Context, since time.Time) ([]UnsubscribeReasonCount, []UnsubscribeComment, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT reason, count(*) FROM unsubscribe_reasons WHERE created_at >= $1 GROUP BY 1 ORDER BY 2 DESC`, since)
	if err != nil {
		return nil, nil, err
	}
	var counts []UnsubscribeReasonCount
	for rows.Next() {
		var c UnsubscribeReasonCount
		if err := rows.Scan(&c.Reason, &c.Count); err != nil {
			rows.Close()
			return nil, nil, err
		}
		counts = append(counts, c)
	}
	rows.Close()
	crows, err := r.Pool.Query(ctx,
		`SELECT reason, comment, created_at FROM unsubscribe_reasons
		  WHERE created_at >= $1 AND comment IS NOT NULL ORDER BY created_at DESC LIMIT 20`, since)
	if err != nil {
		return counts, nil, err
	}
	defer crows.Close()
	var comments []UnsubscribeComment
	for crows.Next() {
		var c UnsubscribeComment
		if err := crows.Scan(&c.Reason, &c.Comment, &c.CreatedAt); err != nil {
			return counts, nil, err
		}
		comments = append(comments, c)
	}
	return counts, comments, crows.Err()
}
