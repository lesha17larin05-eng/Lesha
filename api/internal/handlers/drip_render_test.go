package handlers

import (
	"strings"
	"testing"
)

func TestRenderDripBody(t *testing.T) {
	body := "{имя}, привет!\n\n- **Спина** – урок 3\n- Шея\n\n1. Анкета\n2. Занятие\n\n> Цитата <b>\n\n[Открыть уроки →](https://leshalarin.ru/cabinet)\n\nСм. [канал](https://t.me/x) и **жирное**."
	out := renderDripBody(dripGreeting(body, "Анна Петрова"))
	for _, want := range []string{
		"<p style=\"margin:0 0 16px;\">Анна, привет!</p>",
		"<ul", "<li style=\"margin:0 0 8px;\"><b>Спина</b> – урок 3</li>",
		"<ol", "<li style=\"margin:0 0 8px;\">Занятие</li>",
		"<blockquote", "Цитата &lt;b&gt;",
		`<a href="https://leshalarin.ru/cabinet" style="display:inline-block`, "Открыть уроки&nbsp;→</a>",
		`<a href="https://t.me/x" style="color:#e8652a;">канал</a>`, "<b>жирное</b>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if got := dripGreeting("{имя}, привет! Текст", "  "); got != "Привет! Текст" {
		t.Fatalf("greeting without name: %q", got)
	}
}

// Все тексты цепочки собираются без сырой разметки и с подписью.
func TestDripStepsRender(t *testing.T) {
	if len(dripSteps) != 5 {
		t.Fatalf("expected 5 steps, got %d", len(dripSteps))
	}
	prevDay := 0
	for i, st := range dripSteps {
		if st.Step != i+1 || st.Day <= prevDay || st.Subject == "" {
			t.Fatalf("bad step %+v", st)
		}
		prevDay = st.Day
		out := renderDripBody(dripGreeting(st.Body, "Анна"))
		if strings.Contains(out, "**") || strings.Contains(out, "](") || strings.Contains(out, "{имя}") {
			t.Fatalf("step %d: raw markup left:\n%s", st.Step, out)
		}
		if !strings.Contains(out, "Алексей Ларин") {
			t.Fatalf("step %d: no signature", st.Step)
		}
	}
}
