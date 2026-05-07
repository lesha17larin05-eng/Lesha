package prodamus

import "testing"

func TestSignAndVerify(t *testing.T) {
	secret := "test-secret"
	data := map[string]any{
		"order_id":  "abc-123",
		"order_num": "42",
		"products": []any{
			map[string]any{"name": "Курс", "price": "9900", "quantity": "1"},
		},
		"customer_email": "u@example.com",
	}
	sig, err := Sign(secret, data)
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(secret, data, sig) {
		t.Fatal("signature verify failed for valid data")
	}
	// modifying any field invalidates signature
	data2 := map[string]any{}
	for k, v := range data {
		data2[k] = v
	}
	data2["order_num"] = "43"
	if Verify(secret, data2, sig) {
		t.Fatal("signature should not validate after change")
	}
}

func TestSignDeterministic(t *testing.T) {
	a, _ := Sign("k", map[string]any{"a": "1", "b": "2"})
	b, _ := Sign("k", map[string]any{"b": "2", "a": "1"})
	if a != b {
		t.Fatal("sign must be deterministic regardless of map order")
	}
}

func TestSignStripsSignatureField(t *testing.T) {
	a, _ := Sign("k", map[string]any{"x": "1"})
	b, _ := Sign("k", map[string]any{"x": "1", "signature": "junk"})
	if a != b {
		t.Fatal("signature key must be stripped")
	}
}

func TestPaymentURLContainsSignature(t *testing.T) {
	c := New("https://example.payform.ru", "k")
	u, err := c.PaymentURL(map[string]any{"do": "pay", "order_id": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if u == "" || !contains(u, "signature=") {
		t.Fatalf("url missing signature: %s", u)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
