package prodamus

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
)

type Client struct {
	PayformURL string
	Secret     string
}

func New(payformURL, secret string) *Client {
	return &Client{PayformURL: payformURL, Secret: secret}
}

// Sign computes Prodamus HMAC-SHA256 over the canonical JSON representation
// of the payload (recursively ksort'd, no signature key, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES).
func Sign(secret string, data map[string]any) (string, error) {
	cleaned := stripSig(data)
	canon, err := canonicalJSON(cleaned)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(canon)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func Verify(secret string, data map[string]any, sig string) bool {
	expected, err := Sign(secret, data)
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(expected), []byte(sig))
}

func stripSig(d map[string]any) map[string]any {
	out := make(map[string]any, len(d))
	for k, v := range d {
		if k == "signature" || k == "sign" {
			continue
		}
		switch vv := v.(type) {
		case map[string]any:
			out[k] = stripSig(vv)
		default:
			out[k] = vv
		}
	}
	return out
}

// canonicalJSON marshals with sorted keys and without HTML escaping.
func canonicalJSON(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			vb, err := canonicalJSON(t[k])
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, x := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			vb, err := canonicalJSON(x)
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	default:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(t); err != nil {
			return nil, err
		}
		// Encoder appends \n; trim
		return bytes.TrimRight(buf.Bytes(), "\n"), nil
	}
}

// PaymentURL builds a signed payment URL for redirecting a user to Prodamus.
func (c *Client) PaymentURL(params map[string]any) (string, error) {
	sig, err := Sign(c.Secret, params)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(c.PayformURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for k, v := range flatten("", params) {
		q.Set(k, v)
	}
	q.Set("signature", sig)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// flatten turns nested maps into bracket-style query keys (products[0][name]).
func flatten(prefix string, v any) map[string]string {
	out := map[string]string{}
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			key := k
			if prefix != "" {
				key = prefix + "[" + k + "]"
			}
			for kk, vv := range flatten(key, val) {
				out[kk] = vv
			}
		}
	case []any:
		for i, x := range t {
			key := prefix + "[" + strconv.Itoa(i) + "]"
			for kk, vv := range flatten(key, x) {
				out[kk] = vv
			}
		}
	default:
		out[prefix] = toString(t)
	}
	return out
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "1"
		}
		return "0"
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
