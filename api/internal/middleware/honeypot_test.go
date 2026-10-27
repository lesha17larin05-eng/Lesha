package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHoneypot(t *testing.T) {
	var gotBody string
	called := false
	h := Honeypot(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(201)
	}))
	do := func(body string) *httptest.ResponseRecorder {
		called, gotBody = false, ""
		req := httptest.NewRequest("POST", "/api/leads", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	// заполненная ловушка → 200 ok, хендлер не вызван
	if rec := do(`{"name":"bot","homepage":"http://spam.example"}`); rec.Code != 200 || called || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("bot: code=%d called=%v body=%s", rec.Code, called, rec.Body.String())
	}
	// пустая ловушка и её отсутствие → хендлер получает тело целиком
	for _, b := range []string{`{"name":"Иван","homepage":""}`, `{"name":"Иван"}`, `{"name":"Иван","homepage":"   "}`} {
		if rec := do(b); rec.Code != 201 || !called || gotBody != b {
			t.Fatalf("human %s: code=%d called=%v body=%q", b, rec.Code, called, gotBody)
		}
	}
	// не JSON – пропускаем как есть
	if rec := do(`not json`); rec.Code != 201 || gotBody != "not json" {
		t.Fatalf("non-json: %d %q", rec.Code, gotBody)
	}
}
