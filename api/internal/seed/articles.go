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

var articleCoverImages = map[string]string{
	"kak-nachat-trenirovatsya":                  "/img/blog/kak-nachat.jpg",
	"pochemu-brosaju-trenirovki":                "/img/blog/pochemu-brosaju.jpg",
	"zachem-delat-zaryadku":                     "/img/blog/zaryadka.jpg",
	"bolit-poyasnitsa-sidjachaja-rabota":        "/img/blog/poyasnitsa-new.jpg",
	"pochemu-net-energii":                       "/img/blog/net-energii.jpg",
	"pochemu-ne-hudeu-hotja-treniruus":          "/img/blog/pochemu-ne-hudeu.jpg",
	"kak-uluchshit-son":                         "/img/blog/son.jpg",
	"sustavnaja-gimnastika-dlja-nachinajuschih": "/img/blog/sustavnaya.jpg",
	"kak-uluchshit-osanku":                      "/img/blog/osanka.jpg",
	"kak-snizit-trevozhnost":                    "/img/blog/trevozhnost.jpg",
	"istorija-pereloma-pozvonochnika":           "/img/blog/perelom-new.jpeg",
	"kak-pravilno-podnimat-tyazhesti":           "/img/blog/podnimat.jpg",
	"mpk-chto-eto-i-kak-uluchshit":             "/img/blog/mpk-new.jpeg",
	"kak-sformirovat-privychku-trenirovatsya":   "/img/blog/privychka.jpg",
	"kak-nachat-begat":                          "/img/blog/beg.jpg",
	"trenirovki-i-mentalnoe-zdorove":            "/img/blog/mentalnoe-zdorove.jpg",
	"kak-pitatsya-chtoby-pohudjet":              "/img/blog/pitanie.jpg",
	"sotsialnoe-zdorove":                        "/img/blog/socialnoe-new.jpeg",
	"princip-malenkih-shagov":                   "/img/blog/malenkie-shagi.jpg",
	"uprazhnenija-ot-otekov":                    "/img/blog/oteki.jpg",
	"gimnastika-dlja-glaz":                      "/img/blog/glaza.jpg",
	"uprazhnenija-dlja-shei":                    "/img/blog/sheia.jpg",
	"uprazhnenija-pri-sidjachej-rabote":         "/img/blog/sidyachaya-rabota.jpg",
	"kak-razvit-gibkost":                        "/img/blog/gibkost.jpg",
	"kak-begat-zimoj":                           "/img/blog/beg-zimoj-new.jpeg",
	"banja-i-zdorove":                           "/img/blog/banya-new.jpg",
	"kak-nauchitsja-meditirovat":                "/img/blog/meditaciya.jpg",
	"akrojoga-chto-eto":                         "/img/blog/akrojoga-new.jpeg",
	"obuchenie-novomu-navyku":                   "/img/blog/obuchenie.jpg",
	"dyhatelnye-tehniki":                        "/img/blog/dykhanie.jpg",
	"kak-trenirujet-detej":                      "/img/blog/deti-new.jpg",
	"muzhskoe-zhenskoe-zdorove":                 "/img/blog/zdorove.jpg",
}

// articleSortOrder defines the display order for articles (lower = shown first).
// Articles not listed here get a high sort_order and appear at the end.
var articleSortOrder = map[string]int{
	"bolit-poyasnitsa-sidjachaja-rabota":         1,
	"pochemu-net-energii":                        2,
	"pochemu-brosaju-trenirovki":                 3,
	"kak-nachat-trenirovatsya":                   4,
	"pochemu-ne-hudeu-hotja-treniruus":           5,
	"sustavnaja-gimnastika-dlja-nachinajuschih":  6,
	"kak-uluchshit-son":                          7,
	"kak-uluchshit-osanku":                       8,
	"zachem-delat-zaryadku":                      9,
	"kak-snizit-trevozhnost":                     10,
	"trenirovki-i-mentalnoe-zdorove":             11,
	"kak-sformirovat-privychku-trenirovatsya":    12,
	"kak-pitatsya-chtoby-pohudjet":               13,
	"princip-malenkih-shagov":                    14,
	"istorija-pereloma-pozvonochnika":            15,
	"uprazhnenija-pri-sidjachej-rabote":          16,
	"uprazhnenija-dlja-shei":                     17,
	"uprazhnenija-ot-otekov":                     18,
	"kak-pravilno-podnimat-tyazhesti":            19,
	"kak-nachat-begat":                           20,
	"kak-razvit-gibkost":                         21,
	"dyhatelnye-tehniki":                         22,
	"kak-nauchitsja-meditirovat":                 23,
	"banja-i-zdorove":                            24,
	"mpk-chto-eto-i-kak-uluchshit":              25,
	"gimnastika-dlja-glaz":                       26,
	"obuchenie-novomu-navyku":                    27,
	"sotsialnoe-zdorove":                         28,
	"muzhskoe-zhenskoe-zdorove":                  29,
	"kak-trenirujet-detej":                       30,
	"kak-begat-zimoj":                            31,
	"akrojoga-chto-eto":                          32,
}

// SeedArticles syncs all embedded HTML articles into the database.
// Creates new articles, updates existing ones, and deletes articles
// whose HTML files have been removed.
func SeedArticles(ctx context.Context, repo *db.Repo, authorID uuid.UUID) error {
	entries, err := articlesFS.ReadDir("articles_html")
	if err != nil {
		return err
	}

	created, updated := 0, 0
	seenSlugs := make(map[string]bool)

	for i, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".html")
		seenSlugs[slug] = true

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
		sortOrder, ok := articleSortOrder[slug]
		if !ok {
			sortOrder = 1000 + i
		}

		coverURL := articleCoverImages[slug]

		existing, err := repo.GetArticleBySlug(ctx, slug)
		if err != nil {
			// New article — create it.
			now := time.Now().Add(-time.Duration(i) * time.Hour)
			_, err = repo.CreateArticle(ctx, db.ArticleInput{
				Slug:           slug,
				Title:          title,
				Tag:            tag,
				Excerpt:        excerpt,
				CoverImageURL:  coverURL,
				ContentHTML:    content,
				ReadingMinutes: mins,
				IsPublished:    true,
				PublishedAt:    &now,
				SortOrder:      sortOrder,
				AuthorID:       &authorID,
			})
			if err != nil {
				return err
			}
			created++
		} else {
			// Existing article — update content, reading time, sort order.
			// Prefer the mapped cover; fall back to whatever is already stored.
			if coverURL == "" {
				coverURL = existing.CoverImageURL
			}
			err = repo.UpdateArticle(ctx, existing.ID, db.ArticleInput{
				Slug:           slug,
				Title:          title,
				Tag:            tag,
				Excerpt:        excerpt,
				CoverImageURL:  coverURL,
				ContentHTML:    content,
				ReadingMinutes: mins,
				IsPublished:    existing.IsPublished,
				PublishedAt:    existing.PublishedAt,
				SortOrder:      sortOrder,
			})
			if err != nil {
				return err
			}
			updated++
		}
	}

	// Remove articles whose HTML files no longer exist.
	all, err := repo.ListArticles(ctx, false, 1000, 0)
	if err != nil {
		return err
	}
	deleted := 0
	for _, a := range all {
		if !seenSlugs[a.Slug] {
			if err := repo.DeleteArticle(ctx, a.ID); err != nil {
				return err
			}
			deleted++
		}
	}

	if created+updated+deleted > 0 {
		slog.Info("articles seeded", "created", created, "updated", updated, "deleted", deleted)
	}
	return nil
}
