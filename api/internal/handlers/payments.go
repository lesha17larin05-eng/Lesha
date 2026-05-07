package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/middleware"
)

func (a *App) Checkout(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	c, err := a.Repo.GetCourseBySlug(r.Context(), slug)
	if err != nil || c.Kind != "paid" || c.PriceRub == nil {
		writeErr(w, 404, "not_found")
		return
	}
	uid, _ := middleware.UserID(r.Context())
	u, err := a.Repo.GetUser(r.Context(), uid)
	if err != nil {
		writeErr(w, 401, "no_user")
		return
	}
	if u.EmailVerifiedAt == nil {
		writeErr(w, 400, "email_not_verified")
		return
	}
	has, _ := a.Repo.HasEnrollment(r.Context(), uid, c.ID)
	if has {
		writeErr(w, 400, "already_enrolled")
		return
	}
	o, err := a.Repo.CreateOrder(r.Context(), uid, c.ID, *c.PriceRub)
	if err != nil {
		writeErr(w, 500, "order_failed")
		return
	}
	params := map[string]any{
		"do":              "pay",
		"order_id":        o.ID.String(),
		"order_num":       strconv.FormatInt(o.OrderNum, 10),
		"customer_email":  u.Email,
		"products[0][name]":     c.Title,
		"products[0][price]":    strconv.Itoa(*c.PriceRub),
		"products[0][quantity]": "1",
		"urlReturn":      a.Cfg.AppHost + "/cabinet/courses",
		"urlSuccess":     a.Cfg.AppHost + "/cabinet/courses?paid=" + c.Slug,
		"urlNotification": a.Cfg.AppHost + "/api/webhooks/prodamus",
	}
	if a.Cfg.ProdamusTestMode {
		// in test mode return our fake-payment endpoint
		writeJSON(w, 200, map[string]string{
			"payment_url": a.Cfg.AppHost + "/api/dev/fake-payment?order_id=" + o.ID.String(),
			"order_id":    o.ID.String(),
		})
		return
	}
	url, err := a.Prodamus.PaymentURL(params)
	if err != nil {
		writeErr(w, 500, "sign_failed")
		return
	}
	writeJSON(w, 200, map[string]string{"payment_url": url, "order_id": o.ID.String()})
}

// FakePayment — dev-only endpoint that simulates a successful Prodamus webhook.
func (a *App) FakePayment(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.AppEnv == "production" {
		writeErr(w, 404, "not_found")
		return
	}
	idStr := r.URL.Query().Get("order_id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	o, err := a.Repo.GetOrder(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	if o.Status == "pending" {
		_ = a.Repo.MarkOrderPaid(r.Context(), o.ID, "DEV-"+o.ID.String())
		_ = a.Repo.Grant(r.Context(), o.UserID, o.CourseID, "purchase", nil)
	}
	http.Redirect(w, r, a.Cfg.AppHost+"/cabinet/courses?paid=1", http.StatusFound)
}

func (a *App) ProdamusWebhook(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	headers := map[string]any{}
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ",")
	}
	parsed := parseWebhook(r.Header.Get("Content-Type"), body)
	sig := r.Header.Get("Sign")
	if sig == "" {
		if v, ok := parsed["signature"].(string); ok {
			sig = v
		}
	}
	valid := a.Prodamus.PayformURL != "" && sig != "" && verify(a.Cfg.ProdamusSecret, parsed, sig)
	bodyJSON := map[string]any{}
	for k, v := range parsed {
		bodyJSON[k] = v
	}
	whID, _ := a.Repo.LogWebhook(r.Context(), headers, bodyJSON, valid)
	if !valid {
		slog.Warn("invalid webhook signature")
		w.WriteHeader(200)
		return
	}
	// match order
	var orderID uuid.UUID
	if v, ok := parsed["order_id"].(string); ok {
		if id, err := uuid.Parse(v); err == nil {
			orderID = id
		}
	}
	if orderID == uuid.Nil {
		if v, ok := parsed["order_num"].(string); ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				if o, err := a.Repo.GetOrderByNum(r.Context(), n); err == nil {
					orderID = o.ID
				}
			}
		}
	}
	if orderID == uuid.Nil {
		a.Repo.MarkWebhookProcessed(r.Context(), whID, nil, "no_order")
		w.WriteHeader(200)
		return
	}
	o, err := a.Repo.GetOrder(r.Context(), orderID)
	if err != nil {
		a.Repo.MarkWebhookProcessed(r.Context(), whID, &orderID, "order_not_found")
		w.WriteHeader(200)
		return
	}
	status, _ := parsed["payment_status"].(string)
	if status == "" {
		status, _ = parsed["status"].(string)
	}
	if status == "success" || status == "paid" {
		if o.Status == "pending" {
			pid, _ := parsed["prodamus_order_id"].(string)
			_ = a.Repo.MarkOrderPaid(r.Context(), o.ID, pid)
			_ = a.Repo.Grant(r.Context(), o.UserID, o.CourseID, "purchase", nil)
			u, _ := a.Repo.GetUser(r.Context(), o.UserID)
			c, _ := a.Repo.GetCourseByID(r.Context(), o.CourseID)
			if u != nil && c != nil {
				a.Mail.Async(u.Email, "Доступ к курсу открыт",
					"<p>Спасибо за оплату! Курс \""+c.Title+"\" доступен в личном кабинете.</p>")
			}
		}
	}
	a.Repo.MarkWebhookProcessed(r.Context(), whID, &orderID, "")
	w.WriteHeader(200)
}

func parseWebhook(ct string, body []byte) map[string]any {
	out := map[string]any{}
	if strings.Contains(ct, "application/json") {
		_ = json.Unmarshal(body, &out)
		return out
	}
	// urlencoded
	values := strings.Split(string(body), "&")
	for _, kv := range values {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		k, _ := urlDecode(kv[:eq])
		v, _ := urlDecode(kv[eq+1:])
		out[k] = v
	}
	return out
}

func urlDecode(s string) (string, error) {
	// minimal decode using net/url-style; pull in to avoid extra import
	return decodeQuery(s)
}

func decodeQuery(s string) (string, error) {
	r := strings.NewReplacer("+", " ")
	s = r.Replace(s)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			h, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err == nil {
				b.WriteByte(byte(h))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

func verify(secret string, data map[string]any, sig string) bool {
	// import-free shim, calls into prodamus package
	return verifySig(secret, data, sig)
}
