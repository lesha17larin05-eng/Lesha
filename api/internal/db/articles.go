package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ArticleInput struct {
	Slug           string
	Title          string
	Tag            string
	Excerpt        string
	CoverImageURL  string
	ContentHTML    string
	ReadingMinutes int
	IsPublished    bool
	PublishedAt    *time.Time
	SortOrder      int
	AuthorID       *uuid.UUID
}

const articleCols = `id,slug,title,tag,excerpt,cover_image_url,content_html,
	reading_minutes,is_published,published_at,sort_order,author_id,created_at,updated_at`

func scanArticle(rows pgx.Row) (*Article, error) {
	a := &Article{}
	err := rows.Scan(&a.ID, &a.Slug, &a.Title, &a.Tag, &a.Excerpt, &a.CoverImageURL, &a.ContentHTML,
		&a.ReadingMinutes, &a.IsPublished, &a.PublishedAt, &a.SortOrder, &a.AuthorID, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *Repo) ListArticles(ctx context.Context, onlyPublished bool, limit, offset int) ([]*Article, error) {
	q := `SELECT ` + articleCols + ` FROM articles`
	if onlyPublished {
		q += ` WHERE is_published=true`
	}
	q += ` ORDER BY sort_order ASC, COALESCE(published_at, created_at) DESC LIMIT $1 OFFSET $2`
	rows, err := r.Pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Article
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (r *Repo) CountArticles(ctx context.Context, onlyPublished bool) (int, error) {
	q := `SELECT count(*) FROM articles`
	if onlyPublished {
		q += ` WHERE is_published=true`
	}
	var n int
	err := r.Pool.QueryRow(ctx, q).Scan(&n)
	return n, err
}

func (r *Repo) GetArticleBySlug(ctx context.Context, slug string) (*Article, error) {
	row := r.Pool.QueryRow(ctx, `SELECT `+articleCols+` FROM articles WHERE slug=$1`, slug)
	a, err := scanArticle(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (r *Repo) GetArticleByID(ctx context.Context, id uuid.UUID) (*Article, error) {
	row := r.Pool.QueryRow(ctx, `SELECT `+articleCols+` FROM articles WHERE id=$1`, id)
	a, err := scanArticle(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (r *Repo) CreateArticle(ctx context.Context, in ArticleInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO articles(slug,title,tag,excerpt,cover_image_url,content_html,reading_minutes,is_published,published_at,sort_order,author_id)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		in.Slug, in.Title, in.Tag, in.Excerpt, in.CoverImageURL, in.ContentHTML,
		in.ReadingMinutes, in.IsPublished, in.PublishedAt, in.SortOrder, in.AuthorID,
	).Scan(&id)
	return id, err
}

func (r *Repo) UpdateArticle(ctx context.Context, id uuid.UUID, in ArticleInput) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE articles SET slug=$1,title=$2,tag=$3,excerpt=$4,cover_image_url=$5,content_html=$6,
		   reading_minutes=$7,is_published=$8,published_at=$9,sort_order=$10,updated_at=now() WHERE id=$11`,
		in.Slug, in.Title, in.Tag, in.Excerpt, in.CoverImageURL, in.ContentHTML,
		in.ReadingMinutes, in.IsPublished, in.PublishedAt, in.SortOrder, id,
	)
	return err
}

func (r *Repo) DeleteArticle(ctx context.Context, id uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM articles WHERE id=$1`, id)
	return err
}

// ─── Счётчик чтения статей ──────────────────────────────────────────────
//
// Пишем только факт события и время. Ни IP, ни user-agent, ни идентификатора
// посетителя — поэтому персональных данных здесь нет и согласие не нужно.

// ArticleEvents — допустимые события. Whitelist, а не произвольная строка:
// значение приходит из браузера.
var ArticleEvents = map[string]bool{"open": true, "read": true, "cta": true}

// LogArticleView записывает событие. Ошибку возвращаем, но вызывающий код
// её только логирует: статистика не должна ломать выдачу страницы.
func (r *Repo) LogArticleView(ctx context.Context, articleID uuid.UUID, event string) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO article_views (article_id, event) VALUES ($1, $2)`, articleID, event)
	return err
}

// ArticleStat — строка сводки для админки.
type ArticleStat struct {
	ID        uuid.UUID  `json:"id"`
	Slug      string     `json:"slug"`
	Title     string     `json:"title"`
	Published bool       `json:"is_published"`
	Opens     int        `json:"opens"`
	Reads     int        `json:"reads"`
	CTA       int        `json:"cta"`
	Opens30   int        `json:"opens_30d"`
	LastOpen  *time.Time `json:"last_open"`
}

// ArticleStats — сводка по всем статьям, самые читаемые сверху.
func (r *Repo) ArticleStats(ctx context.Context) ([]ArticleStat, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT a.id, a.slug, a.title, a.is_published,
		       count(*) FILTER (WHERE v.event = 'open') AS opens,
		       count(*) FILTER (WHERE v.event = 'read') AS reads,
		       count(*) FILTER (WHERE v.event = 'cta')  AS cta,
		       count(*) FILTER (WHERE v.event = 'open'
		                          AND v.created_at > now() - interval '30 days') AS opens_30d,
		       max(v.created_at) FILTER (WHERE v.event = 'open') AS last_open
		  FROM articles a
		  LEFT JOIN article_views v ON v.article_id = a.id
		 GROUP BY a.id, a.slug, a.title, a.is_published
		 ORDER BY opens DESC, a.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ArticleStat{}
	for rows.Next() {
		var s ArticleStat
		if err := rows.Scan(&s.ID, &s.Slug, &s.Title, &s.Published,
			&s.Opens, &s.Reads, &s.CTA, &s.Opens30, &s.LastOpen); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
