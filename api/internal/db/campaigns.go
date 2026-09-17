package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Рассылки по базе: группы получателей, кампании и очередь отправки.
//
// Главное правило: в любую группу попадают ТОЛЬКО пользователи с
// `consent_marketing_at` — согласием на рассылку. Без него письмо с
// предложением отправлять нельзя.

// Segment — группа получателей для рассылки.
type Segment struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Hint  string `json:"hint"`
	Count int    `json:"count"`
}

// Segments — белый список групп. Ключи используются в campaigns.segment;
// SQL для каждой группы написан отдельным запросом (без склейки строк).
var Segments = []Segment{
	{Key: "all", Title: "Все подписанные", Hint: "все, кто согласился получать письма"},
	{Key: "paid", Title: "Купившие «Здоровую спину»", Hint: "есть доступ к платному курсу"},
	{Key: "free", Title: "Только бесплатный курс", Hint: "зарегистрировались, но ничего не покупали"},
	{Key: "sleeping", Title: "Не заходили больше месяца", Hint: "для писем «вернитесь к занятиям»"},
}

// SegmentExists — проверка ключа группы (валидация входа).
func SegmentExists(key string) bool {
	for _, s := range Segments {
		if s.Key == key {
			return true
		}
	}
	return false
}

const segmentBase = `
	FROM users u
	WHERE u.consent_marketing_at IS NOT NULL`

const segmentPaid = segmentBase + `
	  AND EXISTS (SELECT 1 FROM enrollments e
	                JOIN courses c ON c.id = e.course_id
	               WHERE e.user_id = u.id AND c.kind = 'paid')`

const segmentFree = segmentBase + `
	  AND NOT EXISTS (SELECT 1 FROM enrollments e
	                    JOIN courses c ON c.id = e.course_id
	                   WHERE e.user_id = u.id AND c.kind = 'paid')`

const segmentSleeping = segmentBase + `
	  AND (u.last_seen_at IS NULL OR u.last_seen_at < now() - interval '30 days')`

func segmentWhere(key string) string {
	switch key {
	case "paid":
		return segmentPaid
	case "free":
		return segmentFree
	case "sleeping":
		return segmentSleeping
	default:
		return segmentBase
	}
}

// SegmentsWithCounts — список групп с количеством людей в каждой.
func (r *Repo) SegmentsWithCounts(ctx context.Context) ([]Segment, error) {
	out := make([]Segment, 0, len(Segments))
	for _, s := range Segments {
		var n int
		// segmentWhere возвращает одну из четырёх констант, пользовательский
		// ввод в запрос не попадает.
		if err := r.Pool.QueryRow(ctx, `SELECT count(*) `+segmentWhere(s.Key)).Scan(&n); err != nil {
			return nil, err
		}
		s.Count = n
		out = append(out, s)
	}
	return out, nil
}

// ---- CAMPAIGNS ----

type Campaign struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Subject    string     `json:"subject"`
	Body       string     `json:"body"`
	Segment    string     `json:"segment"`
	DailyLimit int        `json:"daily_limit"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	// агрегаты
	Total    int `json:"total"`
	Sent     int `json:"sent"`
	Failed   int `json:"failed"`
	Pending  int `json:"pending"`
	SentToday int `json:"sent_today"`
	Opened   int `json:"opened"`
}

// CreateCampaign создаёт рассылку и фиксирует список получателей по группе.
func (r *Repo) CreateCampaign(ctx context.Context, name, subject, body, segment string, dailyLimit int) (uuid.UUID, int, error) {
	var id uuid.UUID
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO campaigns(name, subject, body, segment, daily_limit)
		 VALUES($1,$2,$3,$4,$5) RETURNING id`,
		name, subject, body, segment, dailyLimit).Scan(&id)
	if err != nil {
		return uuid.Nil, 0, err
	}
	tag, err := r.Pool.Exec(ctx,
		`INSERT INTO campaign_recipients(campaign_id, user_id, email, name)
		 SELECT $1, u.id, u.email, coalesce(u.name,'') `+segmentWhere(segment)+`
		 ON CONFLICT DO NOTHING`, id)
	if err != nil {
		return uuid.Nil, 0, err
	}
	return id, int(tag.RowsAffected()), nil
}

func scanCampaign(row pgx.Row) (*Campaign, error) {
	c := &Campaign{}
	err := row.Scan(&c.ID, &c.Name, &c.Subject, &c.Body, &c.Segment, &c.DailyLimit,
		&c.Status, &c.CreatedAt, &c.StartedAt, &c.FinishedAt,
		&c.Total, &c.Sent, &c.Failed, &c.Pending, &c.SentToday, &c.Opened)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

const campaignSelect = `
	SELECT c.id, c.name, c.subject, c.body, c.segment, c.daily_limit,
	       c.status, c.created_at, c.started_at, c.finished_at,
	       (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id = c.id),
	       (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id = c.id AND r.status = 'sent'),
	       (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id = c.id AND r.status = 'failed'),
	       (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id = c.id AND r.status = 'pending'),
	       (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id = c.id AND r.sent_at > now() - interval '24 hours'),
	       (SELECT count(DISTINCT o.user_id) FROM email_opens o WHERE o.campaign = c.id::text)
	  FROM campaigns c`

func (r *Repo) GetCampaign(ctx context.Context, id uuid.UUID) (*Campaign, error) {
	return scanCampaign(r.Pool.QueryRow(ctx, campaignSelect+` WHERE c.id = $1`, id))
}

func (r *Repo) ListCampaigns(ctx context.Context) ([]Campaign, error) {
	rows, err := r.Pool.Query(ctx, campaignSelect+` ORDER BY c.created_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Campaign
	for rows.Next() {
		c := Campaign{}
		if err := rows.Scan(&c.ID, &c.Name, &c.Subject, &c.Body, &c.Segment, &c.DailyLimit,
			&c.Status, &c.CreatedAt, &c.StartedAt, &c.FinishedAt,
			&c.Total, &c.Sent, &c.Failed, &c.Pending, &c.SentToday, &c.Opened); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetCampaignStatus меняет статус рассылки (старт, пауза, завершение).
func (r *Repo) SetCampaignStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE campaigns
		    SET status = $2,
		        started_at  = CASE WHEN $2 = 'sending' AND started_at IS NULL THEN now() ELSE started_at END,
		        finished_at = CASE WHEN $2 = 'done' THEN now() ELSE finished_at END
		  WHERE id = $1`, id, status)
	return err
}

// CampaignRecipient — один адресат в очереди.
type CampaignRecipient struct {
	ID         uuid.UUID
	CampaignID uuid.UUID
	UserID     uuid.UUID
	Email      string
	Name       string
}

// NextCampaignToSend возвращает рассылку в статусе sending, у которой
// сегодня ещё не исчерпан дневной лимит и остались неотправленные письма.
func (r *Repo) NextCampaignToSend(ctx context.Context) (*Campaign, error) {
	return scanCampaign(r.Pool.QueryRow(ctx, campaignSelect+`
		 WHERE c.status = 'sending'
		   AND EXISTS (SELECT 1 FROM campaign_recipients r
		                WHERE r.campaign_id = c.id AND r.status = 'pending')
		   AND (SELECT count(*) FROM campaign_recipients r
		         WHERE r.campaign_id = c.id AND r.sent_at > now() - interval '24 hours') < c.daily_limit
		 ORDER BY c.created_at LIMIT 1`))
}

// NextRecipient — следующий адресат рассылки, у которого согласие ещё в силе.
// Отписавшихся помечает skipped и переходит дальше.
func (r *Repo) NextRecipient(ctx context.Context, campaignID uuid.UUID) (*CampaignRecipient, error) {
	for i := 0; i < 50; i++ {
		rec := &CampaignRecipient{}
		var stillSubscribed bool
		err := r.Pool.QueryRow(ctx,
			`SELECT r.id, r.campaign_id, r.user_id, r.email, r.name,
			        (u.consent_marketing_at IS NOT NULL)
			   FROM campaign_recipients r
			   JOIN users u ON u.id = r.user_id
			  WHERE r.campaign_id = $1 AND r.status = 'pending'
			  ORDER BY r.id LIMIT 1`, campaignID).Scan(
			&rec.ID, &rec.CampaignID, &rec.UserID, &rec.Email, &rec.Name, &stillSubscribed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if stillSubscribed {
			return rec, nil
		}
		// отписался после старта рассылки — не шлём
		if err := r.MarkRecipient(ctx, rec.ID, "skipped", "отписался"); err != nil {
			return nil, err
		}
	}
	return nil, ErrNotFound
}

// MarkRecipient фиксирует результат отправки одному адресату.
func (r *Repo) MarkRecipient(ctx context.Context, id uuid.UUID, status, errText string) error {
	if len(errText) > 300 {
		errText = errText[:300]
	}
	_, err := r.Pool.Exec(ctx,
		`UPDATE campaign_recipients
		    SET status = $2,
		        sent_at = CASE WHEN $2 = 'sent' THEN now() ELSE sent_at END,
		        error = nullif($3,'')
		  WHERE id = $1`, id, status, errText)
	return err
}

// CampaignRecipientRows — список адресатов для страницы рассылки в админке.
func (r *Repo) CampaignRecipientRows(ctx context.Context, campaignID uuid.UUID, limit int) ([]map[string]any, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT r.email, r.name, r.status, r.sent_at, coalesce(r.error,''),
		        EXISTS (SELECT 1 FROM email_opens o
		                 WHERE o.user_id = r.user_id AND o.campaign = r.campaign_id::text)
		   FROM campaign_recipients r
		  WHERE r.campaign_id = $1
		  ORDER BY r.sent_at DESC NULLS LAST, r.email
		  LIMIT $2`, campaignID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var email, name, status, errText string
		var sentAt *time.Time
		var opened bool
		if err := rows.Scan(&email, &name, &status, &sentAt, &errText, &opened); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"email": email, "name": name, "status": status,
			"sent_at": sentAt, "error": errText, "opened": opened,
		})
	}
	return out, rows.Err()
}

// SentLast24h — сколько писем рассылок ушло за сутки по всем кампаниям.
// Нужно, чтобы не упереться в суточный лимит почтового провайдера.
func (r *Repo) SentLast24h(ctx context.Context) (int, error) {
	var n int
	err := r.Pool.QueryRow(ctx,
		`SELECT count(*) FROM campaign_recipients WHERE sent_at > now() - interval '24 hours'`).Scan(&n)
	return n, err
}
