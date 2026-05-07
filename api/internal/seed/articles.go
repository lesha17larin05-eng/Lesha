package seed

import (
	"context"
	"embed"
	"html"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/db"
)

//go:embed articles_html/*.html
var articlesFS embed.FS

var (
	reMetaDesc      = regexp.MustCompile(`(?is)<meta\s+name=["']description["']\s+content=["']([^"']*)["']`)
	reArticleTag    = regexp.MustCompile(`(?is)<span\s+class=["']article-tag["'][^>]*>(.*?)</span>`)
	reArticleH1     = regexp.MustCompile(`(?is)<div\s+class=["']article-header["'][^>]*>.*?<h1[^>]*>(.*?)</h1>`)
	reArticleMeta   = regexp.MustCompile(`(?is)<div\s+class=["']article-meta["'][^>]*>(.*?)</div>`)
	reReadingMin    = regexp.MustCompile(`(\d+)\s*мин`)
	reArticleBody   = regexp.MustCompile(`(?is)<div\s+class=["']article-body["'][^>]*>(.*?)</div>\s*<footer`)
	reBackLink      = regexp.MustCompile(`(?is)<a[^>]*class=["']back-link["'][^>]*>.*?</a>`)
	reArticleCta    = regexp.MustCompile(`(?is)<div\s+class=["']article-cta["'][^>]*>.*?</div>`)
	reTagsAll       = regexp.MustCompile(`<[^>]+>`)
	reMultiSpace    = regexp.MustCompile(`\s+`)
)

// parseArticleHTML extracts structured fields from an HTML article in сайт/blog/*.
// It is tolerant: missing fields produce empty strings rather than errors.
func parseArticleHTML(raw string) (title, tag, excerpt, content string, readingMin int) {
	if m := reMetaDesc.FindStringSubmatch(raw); len(m) == 2 {
		excerpt = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if m := reArticleTag.FindStringSubmatch(raw); len(m) == 2 {
		tag = strings.TrimSpace(html.UnescapeString(stripTags(m[1])))
	}
	if m := reArticleH1.FindStringSubmatch(raw); len(m) == 2 {
		// Replace <br> with space, keep <em> contents but strip the tag.
		t := strings.ReplaceAll(m[1], "<br>", " ")
		t = strings.ReplaceAll(t, "<br/>", " ")
		t = strings.ReplaceAll(t, "<br />", " ")
		title = strings.TrimSpace(reMultiSpace.ReplaceAllString(html.UnescapeString(stripTags(t)), " "))
	}
	if m := reArticleMeta.FindStringSubmatch(raw); len(m) == 2 {
		if mm := reReadingMin.FindStringSubmatch(m[1]); len(mm) == 2 {
			readingMin, _ = strconv.Atoi(mm[1])
		}
	}
	if m := reArticleBody.FindStringSubmatch(raw); len(m) == 2 {
		body := m[1]
		body = reBackLink.ReplaceAllString(body, "")
		body = reArticleCta.ReplaceAllString(body, "")
		content = strings.TrimSpace(body)
	}
	return
}

func stripTags(s string) string { return reTagsAll.ReplaceAllString(s, "") }

// SeedArticles imports all embedded HTML articles into the database.
// Idempotent: skips articles whose slug already exists.
func SeedArticles(ctx context.Context, repo *db.Repo, authorID uuid.UUID) error {
	entries, err := articlesFS.ReadDir("articles_html")
	if err != nil {
		return err
	}
	created := 0
	for i, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".html")
		if _, err := repo.GetArticleBySlug(ctx, slug); err == nil {
			continue
		}
		raw, err := articlesFS.ReadFile("articles_html/" + e.Name())
		if err != nil {
			return err
		}
		title, tag, excerpt, content, mins := parseArticleHTML(string(raw))
		if title == "" {
			title = slug
		}
		if mins == 0 {
			mins = 4
		}
		now := time.Now().Add(-time.Duration(i) * time.Hour)
		_, err = repo.CreateArticle(ctx, db.ArticleInput{
			Slug:           slug,
			Title:          title,
			Tag:            tag,
			Excerpt:        excerpt,
			ContentHTML:    content,
			ReadingMinutes: mins,
			IsPublished:    true,
			PublishedAt:    &now,
			SortOrder:      i,
			AuthorID:       &authorID,
		})
		if err != nil {
			return err
		}
		created++
	}
	if created > 0 {
		slog.Info("articles seeded", "created", created, "total", len(entries))
	}
	return nil
}
