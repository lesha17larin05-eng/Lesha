package db

import (
	"context"

	"github.com/google/uuid"
)

// LogEmailClick – переход по ссылке из письма. Best-effort: ошибка записи
// не должна мешать перевести человека на страницу.
func (r *Repo) LogEmailClick(ctx context.Context, userID uuid.UUID, campaign, url, userAgent string) {
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	if len(url) > 1000 {
		url = url[:1000]
	}
	_, _ = r.Pool.Exec(ctx,
		`INSERT INTO email_clicks(user_id, campaign, url, user_agent) VALUES ($1, $2, $3, $4)`,
		userID, campaign, url, userAgent)
}

// LinkClicks – по каким ссылкам письма переходили: сколько людей (без
// админов – их тестовые письма не считаем) на каждую ссылку.
type LinkClicks struct {
	URL    string `json:"url"`
	People int    `json:"people"`
}

func (r *Repo) CampaignLinkClicks(ctx context.Context, campaign string) ([]LinkClicks, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT k.url, count(DISTINCT k.user_id)
		   FROM email_clicks k JOIN users u ON u.id = k.user_id AND u.role = 'user'
		  WHERE k.campaign = $1
		  GROUP BY k.url ORDER BY 2 DESC, 1`, campaign)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LinkClicks{}
	for rows.Next() {
		var l LinkClicks
		if err := rows.Scan(&l.URL, &l.People); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
