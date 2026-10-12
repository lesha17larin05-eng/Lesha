// Package notify – уведомления Алексею в Telegram.
//
// С российского сервера api.telegram.org недоступен, поэтому сообщения идут
// через прокладку – веб-приложение Google Apps Script в аккаунте Алексея
// (код – docs/07-deploy-ops.md, «Уведомления в Telegram»). Сервер шлёт
// POST {"secret": ..., "text": ...}, скрипт пересылает текст боту.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Telegram struct {
	URL, Secret string
	client      *http.Client
}

func New(url, secret string) *Telegram {
	return &Telegram{URL: url, Secret: secret, client: &http.Client{
		Timeout: 30 * time.Second,
		// Apps Script отвечает на POST редиректом на googleusercontent.com.
		// Скрипт к этому моменту уже выполнился – редирект не нужен.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Enabled – прокладка настроена.
func (t *Telegram) Enabled() bool { return t != nil && t.URL != "" }

// Send отправляет сообщение синхронно.
func (t *Telegram) Send(text string) error {
	if !t.Enabled() {
		return nil
	}
	if len([]rune(text)) > 3800 {
		text = string([]rune(text)[:3800]) + "…"
	}
	b, _ := json.Marshal(map[string]string{"secret": t.Secret, "text": text})
	resp, err := t.client.Post(t.URL, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 302 {
		return fmt.Errorf("telegram relay: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Async – Send в горутине, ошибки только в лог.
func (t *Telegram) Async(text string) {
	if !t.Enabled() {
		return
	}
	go func() {
		if err := t.Send(text); err != nil {
			slog.Warn("telegram notify failed", "err", err)
		}
	}()
}

var (
	tagRe   = regexp.MustCompile(`<[^>]+>`)
	breakRe = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</li>|</tr>|</h\d>|<hr\s*/?>`)
	cellRe  = regexp.MustCompile(`(?i)</td>\s*<td[^>]*>`)
	spaceRe = regexp.MustCompile(`[ \t]+`)
	blankRe = regexp.MustCompile(`\n{3,}`)
)

// HTMLToText – простой текст из HTML письма-уведомления (для Telegram).
func HTMLToText(s string) string {
	s = breakRe.ReplaceAllString(s, "\n")
	s = cellRe.ReplaceAllString(s, ": ")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaceRe.ReplaceAllString(l, " "))
	}
	s = strings.Join(lines, "\n")
	return strings.TrimSpace(blankRe.ReplaceAllString(s, "\n\n"))
}
