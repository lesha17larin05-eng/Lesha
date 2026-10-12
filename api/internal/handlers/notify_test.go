package handlers

import (
	"strings"
	"testing"
	"time"
)

func TestMailMonitor(t *testing.T) {
	m := &mailMonitor{}
	t0 := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	if m.check(t0, time.Time{}, "", 0) != "" {
		t.Fatal("healthy: no message")
	}
	if m.check(t0.Add(10*time.Minute), t0, "timeout", 2) != "" {
		t.Fatal("short outage: no message yet")
	}
	msg := m.check(t0.Add(31*time.Minute), t0, "timeout", 2)
	if !strings.Contains(msg, "не отправляются с 12.10 12:00") || !strings.Contains(msg, "Ждут отправки: 2") {
		t.Fatalf("alert: %q", msg)
	}
	if m.check(t0.Add(2*time.Hour), t0, "timeout", 3) != "" {
		t.Fatal("no repeat within 6 hours")
	}
	if m.check(t0.Add(7*time.Hour), t0, "timeout", 3) == "" {
		t.Fatal("repeat after 6 hours")
	}
	if !strings.Contains(m.check(t0.Add(8*time.Hour), time.Time{}, "", 0), "снова работает") {
		t.Fatal("recovery message")
	}
	if m.check(t0.Add(9*time.Hour), time.Time{}, "", 0) != "" {
		t.Fatal("recovery only once")
	}
}
