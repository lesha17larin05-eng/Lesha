package email

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"log/slog"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Sender отправляет HTML-письма через SMTP.
// Порт 465 – implicit TLS (SMTPS); 587/25 – STARTTLS.
type Sender struct {
	Host, Port, User, Pass, From string

	// Fallback – запасной ящик (другой почтовый сервис). Если основной не
	// ответил или отказал, письмо уходит через запасной – от его адреса.
	Fallback *Sender
	// OnAsyncFail вызывается, когда письмо из Async не ушло ни через один
	// ящик. В проде – кладёт письмо в очередь повторной отправки.
	OnAsyncFail func(to, subject, body string, err error)

	mu           sync.Mutex
	failingSince time.Time // с какого момента письма не уходят (ноль – всё хорошо)
	lastErr      string
}

// Health – с какого момента письма не уходят ни через один ящик (ноль –
// последнее письмо ушло) и последняя ошибка.
func (s *Sender) Health() (time.Time, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failingSince, s.lastErr
}

func (s *Sender) track(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.failingSince, s.lastErr = time.Time{}, ""
		return
	}
	if s.failingSince.IsZero() {
		s.failingSince = time.Now()
	}
	s.lastErr = err.Error()
}

func New(host, port, user, pass, from string) *Sender {
	return &Sender{Host: host, Port: port, User: user, Pass: pass, From: from}
}

// Send отправляет письмо со стандартными заголовками.
func (s *Sender) Send(to, subject, body string) error {
	return s.SendWithHeaders(to, subject, body, nil)
}

// SendWithHeaders – то же самое, но с дополнительными заголовками
// (например, List-Unsubscribe для писем рассылки).
func (s *Sender) SendWithHeaders(to, subject, body string, extra map[string]string) error {
	if s.Host == "" || isReservedTestDomain(to) {
		return s.sendOnce(to, subject, body, extra)
	}
	err := s.sendWithFallback(to, subject, body, extra)
	if err != nil && isRecipientError(err) {
		// Отказ по конкретному адресу (нет такого ящика) – почта при этом работает.
		return err
	}
	s.track(err)
	return err
}

// isRecipientError – последний из ящиков дошёл до сервера и вошёл, но
// сервер отверг конкретного адресата. Почта при этом работает.
func isRecipientError(err error) bool {
	msg := err.Error()
	if i := strings.LastIndex(msg, "запасной ящик: "); i >= 0 {
		msg = msg[i:]
	}
	return strings.Contains(msg, "smtp RCPT TO")
}

func (s *Sender) sendWithFallback(to, subject, body string, extra map[string]string) error {
	err := s.sendOnce(to, subject, body, extra)
	if err == nil || s.Fallback == nil || s.Fallback.Host == "" {
		return err
	}
	if err2 := s.Fallback.sendOnce(to, subject, body, extra); err2 != nil {
		return fmt.Errorf("%w; запасной ящик: %v", err, err2)
	}
	slog.Warn("email sent via fallback", "to", to, "primary_err", err)
	return nil
}

// dialTimeout – сколько ждать соединения с почтовым сервером. Без него
// недоступный сервер держит отправку по 2 минуты.
const dialTimeout = 20 * time.Second

func (s *Sender) sendOnce(to, subject, body string, extra map[string]string) error {
	if s.Host == "" {
		slog.Info("email skipped (no SMTP configured)", "to", to, "subject", subject)
		return nil
	}
	// RFC 2606 reserved domains/TLDs – гарантированно никуда не доставятся,
	// а попытка обернётся bounce'ом в inbox отправителя. Не шлём.
	if isReservedTestDomain(to) {
		slog.Info("email skipped (test domain)", "to", to, "subject", subject)
		return nil
	}

	addr := net.JoinHostPort(s.Host, s.Port)

	var c *smtp.Client

	if s.Port == "465" {
		// Implicit TLS (SMTPS) – соединение уже в TLS с первого байта.
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: dialTimeout}, "tcp", addr, &tls.Config{ServerName: s.Host})
		if err != nil {
			return fmt.Errorf("smtp tls dial %s: %w", addr, err)
		}
		c, err = smtp.NewClient(conn, s.Host)
		if err != nil {
			conn.Close()
			return fmt.Errorf("smtp new client: %w", err)
		}
	} else {
		// STARTTLS (порт 587) или plain (25).
		conn, err := net.DialTimeout("tcp", addr, dialTimeout)
		if err != nil {
			return fmt.Errorf("smtp dial %s: %w", addr, err)
		}
		c, err = smtp.NewClient(conn, s.Host)
		if err != nil {
			conn.Close()
			return fmt.Errorf("smtp new client: %w", err)
		}
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err = c.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
				c.Close()
				return fmt.Errorf("starttls: %w", err)
			}
		}
	}
	defer c.Close()

	if err := c.Auth(smtp.PlainAuth("", s.User, s.Pass, s.Host)); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}

	// MAIL FROM принимает только адрес без display name.
	if err := c.Mail(s.User); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}

	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}

	// Корректные заголовки RFC 5322 / 2047:
	// - From: display name в B-encoding если есть не-ASCII; address – RFC 5321
	// - Subject: тоже Q-encoded если не-ASCII
	// - Date в формате RFC 1123Z (требуется для проверки спам-фильтров)
	// - Message-ID – уникальный, иначе антиспам недоволен
	// - Reply-To = адрес отправителя (по умолчанию)
	fromHeader := encodeAddress(s.From, s.User)
	domain := domainFromEmail(s.User)
	msg := "From: " + fromHeader + "\r\n" +
		"To: " + to + "\r\n" +
		"Reply-To: " + s.User + "\r\n" +
		"Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n" +
		"Message-ID: <" + randID() + "@" + domain + ">\r\n" +
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n" +
		"MIME-Version: 1.0\r\n"
	for k, v := range extra {
		if k == "" || v == "" {
			continue
		}
		msg += k + ": " + v + "\r\n"
	}
	msg += multipartBody(body)

	if _, err = fmt.Fprint(wc, msg); err != nil {
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err = wc.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return c.Quit()
}

// encodeAddress кодирует From-заголовок: "Имя <addr@host>"
// Если name содержит не-ASCII – кодирует через RFC 2047 B-encoding,
// чтобы Gmail/Yandex не ругались на RFC 5322 non-compliant header.
func encodeAddress(from, fallbackAddr string) string {
	addr, err := mail.ParseAddress(from)
	if err != nil {
		// from мог быть просто адресом без display name
		return fallbackAddr
	}
	if addr.Name == "" {
		return addr.Address
	}
	// (mime.WordEncoder) Encode сам решит – Q или B, и оставит ASCII как есть.
	encoded := mime.BEncoding.Encode("UTF-8", addr.Name)
	return encoded + " <" + addr.Address + ">"
}

// isReservedTestDomain – true если адрес попадает в зарезервированные RFC 2606
// домены/TLD (example.com/.org/.net, .example, .test, .invalid, .localhost)
// или явные одноразовые алиасы.
func isReservedTestDomain(addr string) bool {
	d := strings.ToLower(domainFromEmail(addr))
	if d == "" {
		return false
	}
	// RFC 2606 reserved TLDs
	for _, tld := range []string{".example", ".test", ".invalid", ".localhost"} {
		if strings.HasSuffix(d, tld) {
			return true
		}
	}
	// RFC 2606 reserved second-level domains
	switch d {
	case "example.com", "example.org", "example.net", "localhost":
		return true
	}
	return false
}

func domainFromEmail(addr string) string {
	i := strings.LastIndexByte(addr, '@')
	if i < 0 || i == len(addr)-1 {
		return "localhost"
	}
	return addr[i+1:]
}

// multipartBody – тело письма multipart/alternative: текстовая версия и HTML.
// Письма только с HTML антиспам-фильтры (SpamAssassin MIME_HTML_ONLY, Mail.ru,
// Gmail) считают подозрительными. Обе части – base64, чтобы длинные строки
// не резались почтовым сервером по лимиту в 998 символов.
func multipartBody(html string) string {
	boundary := "alt-" + randID()
	return "Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64Wrap(HTMLToText(html)) + "\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64Wrap(html) + "\r\n" +
		"--" + boundary + "--\r\n"
}

var (
	reLink    = regexp.MustCompile(`(?is)<a\s[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	reBreak   = regexp.MustCompile(`(?i)<br\s*/?>`)
	reBlock   = regexp.MustCompile(`(?i)</(p|div|h[1-6]|tr|table|blockquote|ul|ol)>`)
	reItem    = regexp.MustCompile(`(?i)<li[^>]*>`)
	reHidden  = regexp.MustCompile(`(?is)<(style|script|head)[^>]*>.*?</(style|script|head)>`)
	reTag     = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpaces  = regexp.MustCompile(`[ \t]+`)
	reNewline = regexp.MustCompile(`\n\s*\n\s*(\n\s*)+`)
)

// HTMLToText – простая текстовая версия письма: ссылки как «текст (адрес)»,
// абзацы и переносы сохраняются, теги убираются.
func HTMLToText(h string) string {
	s := reHidden.ReplaceAllString(h, "")
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := reLink.FindStringSubmatch(m)
		text := strings.TrimSpace(reTag.ReplaceAllString(sub[2], ""))
		href := html.UnescapeString(sub[1])
		if text == "" || strings.Contains(href, text) {
			return href
		}
		if strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "#") {
			return text
		}
		return text + " (" + href + ")"
	})
	s = reBreak.ReplaceAllString(s, "\n")
	s = reBlock.ReplaceAllString(s, "\n\n")
	s = reItem.ReplaceAllString(s, "\n– ")
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(reSpaces.ReplaceAllString(l, " "))
	}
	s = strings.Join(lines, "\n")
	s = reNewline.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s) + "\n"
}

func randID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Async запускает Send в горутине. Если письмо не ушло – логирует и
// передаёт его в OnAsyncFail (очередь повторной отправки).
func (s *Sender) Async(to, subject, body string) {
	go func() {
		if err := s.Send(to, subject, body); err != nil {
			slog.Error("email send failed", "err", err, "to", to)
			if s.OnAsyncFail != nil {
				s.OnAsyncFail(to, subject, body, err)
			}
		}
	}()
}

// base64Wrap кодирует тело письма в base64 строками по 76 символов,
// как требует RFC 2045.
func base64Wrap(body string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(body))
	var sb strings.Builder
	for i := 0; i < len(enc); i += 76 {
		end := i + 76
		if end > len(enc) {
			end = len(enc)
		}
		sb.WriteString(enc[i:end])
		sb.WriteString("\r\n")
	}
	return sb.String()
}
