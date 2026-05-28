package email

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
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

	msg := "From: " + s.From + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"\r\n" + body

	if _, err = fmt.Fprint(wc, msg); err != nil {
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err = wc.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return c.Quit()
}

// Async запускает Send в горутине; ошибки логируются.
func (s *Sender) Async(to, subject, body string) {
	go func() {
		if err := s.Send(to, subject, body); err != nil {
			slog.Error("email send failed", "err", err, "to", to)
		}
	}()
}
