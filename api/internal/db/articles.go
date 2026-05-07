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
	q += ` ORDER BY COALESCE(published_at, created_at) DESC, sort_order LIMIT $1 OFFSET $2`
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
