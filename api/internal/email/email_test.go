package email

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakeSMTP – минимальный SMTP-сервер без TLS: принимает одно письмо и
// запоминает адресата.
type fakeSMTP struct {
	ln   net.Listener
	mu   sync.Mutex
	rcpt []string
}

func startFakeSMTP(t *testing.T) *fakeSMTP {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	w := func(s string) { c.Write([]byte(s + "\r\n")) }
	w("220 fake")
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line == "." {
				inData = false
				w("250 ok")
			}
			continue
		}
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			w("250-fake")
			w("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH"):
			w("235 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			f.mu.Lock()
			f.rcpt = append(f.rcpt, strings.Trim(line[len("RCPT TO:"):], "<>"))
			f.mu.Unlock()
			w("250 ok")
		case strings.HasPrefix(cmd, "DATA"):
			inData = true
			w("354 go")
		case strings.HasPrefix(cmd, "QUIT"):
			w("221 bye")
			return
		default:
			w("250 ok")
		}
	}
}

func (f *fakeSMTP) port() string {
	_, p, _ := net.SplitHostPort(f.ln.Addr().String())
	return p
}

// closedPort – порт, на котором никто не слушает.
func closedPort(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, p, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	return p
}

func TestFallbackWhenPrimaryDown(t *testing.T) {
	backup := startFakeSMTP(t)
	s := New("127.0.0.1", closedPort(t), "a@main.ru", "x", "Алексей <a@main.ru>")
	s.Fallback = New("127.0.0.1", backup.port(), "b@backup.ru", "y", "Алексей <b@backup.ru>")
	if err := s.Send("client@mail.ru", "Тема", "<p>hi</p>"); err != nil {
		t.Fatalf("fallback must deliver: %v", err)
	}
	if len(backup.rcpt) != 1 || backup.rcpt[0] != "client@mail.ru" {
		t.Fatalf("backup got %v", backup.rcpt)
	}
}

func TestBothDownCallsOnAsyncFail(t *testing.T) {
	s := New("127.0.0.1", closedPort(t), "a@main.ru", "x", "a@main.ru")
	s.Fallback = New("127.0.0.1", closedPort(t), "b@backup.ru", "y", "b@backup.ru")
	if err := s.Send("client@mail.ru", "Тема", "x"); err == nil || !strings.Contains(err.Error(), "запасной ящик") {
		t.Fatalf("expected combined error, got %v", err)
	}
	got := make(chan string, 1)
	s.OnAsyncFail = func(to, subject, body string, err error) { got <- to + "|" + subject }
	s.Async("client@mail.ru", "Доступ", "x")
	if v := <-got; v != "client@mail.ru|Доступ" {
		t.Fatalf("OnAsyncFail got %q", v)
	}
}

func TestPrimaryOKSkipsFallback(t *testing.T) {
	main, backup := startFakeSMTP(t), startFakeSMTP(t)
	s := New("127.0.0.1", main.port(), "a@main.ru", "x", "a@main.ru")
	s.Fallback = New("127.0.0.1", backup.port(), "b@backup.ru", "y", "b@backup.ru")
	if err := s.Send("client@mail.ru", "Тема", "x"); err != nil {
		t.Fatal(err)
	}
	if len(main.rcpt) != 1 || len(backup.rcpt) != 0 {
		t.Fatalf("main=%v backup=%v", main.rcpt, backup.rcpt)
	}
}

func TestHealthTracksOutage(t *testing.T) {
	s := New("127.0.0.1", closedPort(t), "a@main.ru", "x", "a@main.ru")
	if since, _ := s.Health(); !since.IsZero() {
		t.Fatal("fresh sender must be healthy")
	}
	_ = s.Send("client@mail.ru", "x", "x")
	since, lastErr := s.Health()
	if since.IsZero() || lastErr == "" {
		t.Fatal("failed send must mark outage")
	}
	_ = s.Send("client@mail.ru", "x", "x")
	if again, _ := s.Health(); !again.Equal(since) {
		t.Fatal("outage start must not move on repeated failures")
	}
	ok := startFakeSMTP(t)
	s.Port = ok.port()
	if err := s.Send("client@mail.ru", "x", "x"); err != nil {
		t.Fatal(err)
	}
	if since, _ := s.Health(); !since.IsZero() {
		t.Fatal("successful send must clear outage")
	}
}
