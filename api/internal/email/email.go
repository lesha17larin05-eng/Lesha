package email

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Sender отправляет HTML-письма через SMTP.
// Порт 465 — implicit TLS (SMTPS); 587/25 — STARTTLS.
type Sender struct {
	Host, Port, User, Pass, From string
}

func New(host, port, user, pass, from string) *Sender {
	return &Sender{Host: host, Port: port, User: user, Pass: pass, From: from}
}

func (s *Sender) Send(to, subject, body string) error {
	if s.Host == "" {
		slog.Info("email skipped (no SMTP configured)", "to", to, "subject", subject)
		return nil
	}

	addr := net.JoinHostPort(s.Host, s.Port)

	var c *smtp.Client

	if s.Port == "465" {
		// Implicit TLS (SMTPS) — соединение уже в TLS с первого байта.
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: s.Host})
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
		var err error
		c, err = smtp.Dial(addr)
		if err != nil {
			return fmt.Errorf("smtp dial %s: %w", addr, err)
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
	// - From: display name в B-encoding если есть не-ASCII; address — RFC 5321
	// - Subject: тоже Q-encoded если не-ASCII
	// - Date в формате RFC 1123Z (требуется для проверки спам-фильтров)
	// - Message-ID — уникальный, иначе антиспам недоволен
	// - Reply-To = адрес отправителя (по умолчанию)
	fromHeader := encodeAddress(s.From, s.User)
	domain := domainFromEmail(s.User)
	msg := "From: " + fromHeader + "\r\n" +
		"To: " + to + "\r\n" +
		"Reply-To: " + s.User + "\r\n" +
		"Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n" +
		"Message-ID: <" + randID() + "@" + domain + ">\r\n" +
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" + body

	if _, err = fmt.Fprint(wc, msg); err != nil {
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err = wc.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return c.Quit()
}

// encodeAddress кодирует From-заголовок: "Имя <addr@host>"
// Если name содержит не-ASCII — кодирует через RFC 2047 B-encoding,
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
	// (mime.WordEncoder) Encode сам решит — Q или B, и оставит ASCII как есть.
	encoded := mime.BEncoding.Encode("UTF-8", addr.Name)
	return encoded + " <" + addr.Address + ">"
}

func domainFromEmail(addr string) string {
	i := strings.LastIndexByte(addr, '@')
	if i < 0 || i == len(addr)-1 {
		return "localhost"
	}
	return addr[i+1:]
}

func randID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Async запускает Send в горутине; ошибки логируются.
func (s *Sender) Async(to, subject, body string) {
	go func() {
		if err := s.Send(to, subject, body); err != nil {
			slog.Error("email send failed", "err", err, "to", to)
		}
	}()
}
