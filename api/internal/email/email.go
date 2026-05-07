package email

import (
	"fmt"
	"log/slog"
	"net/smtp"
)

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
	addr := s.Host + ":" + s.Port
	auth := smtp.PlainAuth("", s.User, s.Pass, s.Host)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		s.From, to, subject, body)
	return smtp.SendMail(addr, auth, s.From, []string{to}, []byte(msg))
}

// Async wraps Send into a goroutine; errors logged.
func (s *Sender) Async(to, subject, body string) {
	go func() {
		if err := s.Send(to, subject, body); err != nil {
			slog.Error("email send failed", "err", err, "to", to)
		}
	}()
}
