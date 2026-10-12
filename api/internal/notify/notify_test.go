package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTMLToText(t *testing.T) {
	got := HTMLToText(`<p>Новая <b>заявка</b></p><table><tr><td>Имя</td><td>Анна &amp; Co</td></tr></table><p>a<br>b</p>`)
	want := "Новая заявка\nИмя: Анна & Co\na\nb"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSendThroughRelay(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		http.Redirect(w, r, "https://script.googleusercontent.com/x", http.StatusFound)
	}))
	defer srv.Close()
	tg := New(srv.URL, "s3cret")
	if err := tg.Send("привет"); err != nil {
		t.Fatal(err)
	}
	if got["secret"] != "s3cret" || got["text"] != "привет" {
		t.Fatalf("relay got %v", got)
	}
	if New("", "").Send("x") != nil || New("", "").Enabled() {
		t.Fatal("empty URL must be a no-op")
	}
}
