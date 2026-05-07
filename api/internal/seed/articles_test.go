package seed

import (
	"strings"
	"testing"
)

const sampleArticle = `<!DOCTYPE html>
<html lang="ru"><head>
<meta name="description" content="Краткое описание статьи.">
<title>X</title></head><body>
<nav>...</nav>
<div class="article-header">
  <span class="article-tag">Начало пути</span>
  <h1>Заголовок – <em>с акцентом</em></h1>
  <div class="article-meta"><span>Автор</span><span>· 7 мин чтения</span></div>
</div>
<div class="article-body">
  <a href="../blog.html" class="back-link">← Все статьи</a>
  <p>Первый абзац.</p>
  <h2>Раздел</h2>
  <p>Второй абзац.</p>
  <div class="article-cta"><p>CTA</p><a href="x">link</a></div>
</div>
<footer>...</footer>
</body></html>`

func TestParseArticleHTML(t *testing.T) {
	title, tag, excerpt, content, mins := parseArticleHTML(sampleArticle)
	if title != "Заголовок – с акцентом" {
		t.Errorf("title = %q", title)
	}
	if tag != "Начало пути" {
		t.Errorf("tag = %q", tag)
	}
	if excerpt != "Краткое описание статьи." {
		t.Errorf("excerpt = %q", excerpt)
	}
	if mins != 7 {
		t.Errorf("mins = %d", mins)
	}
	if !strings.Contains(content, "Первый абзац") || !strings.Contains(content, "Раздел") {
		t.Errorf("content missing expected text: %s", content)
	}
	if strings.Contains(content, "back-link") || strings.Contains(content, "Все статьи") {
		t.Errorf("content must strip back-link: %s", content)
	}
	if strings.Contains(content, "article-cta") || strings.Contains(content, "CTA") {
		t.Errorf("content must strip cta: %s", content)
	}
}
