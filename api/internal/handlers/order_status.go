package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// OrderStatus – статус заказа для страницы «после оплаты» (/kurs-oplachen).
//
// GET /api/orders/{id}/status → {status, email_hint}
//
// Продамус возвращает покупателя на сайт сразу после оплаты, а подтверждение
// (webhook) может прийти позже или не прийти вовсе. Страница опрашивает этот
// эндпоинт и честно показывает, открыт ли уже доступ.
//
// Публичный: id заказа – случайный UUID, который знает только покупатель
// (Продамус кладёт его в ссылку возврата). Почта отдаётся замаскированной –
// чтобы покупатель увидел, на какой адрес ушло письмо, и заметил опечатку.
func (a *App) OrderStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	o, err := a.Repo.GetOrder(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	// Страница после оплаты передаёт returned=1, если Продамус вернул
	// покупателя с отметкой «успешно» – от этого момента сторож считает,
	// сколько ждём подтверждения (payments_watch.go).
	if r.URL.Query().Get("returned") == "1" && o.Status == "pending" {
		_ = a.Repo.MarkOrderReturned(r.Context(), o.ID)
	}
	hint := ""
	if u, err := a.Repo.GetUser(r.Context(), o.UserID); err == nil {
		hint = maskEmail(u.Email)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"status": o.Status, "email_hint": hint,
		// Можно войти сразу, без письма: заказ оформлен в этом браузере.
		"can_enter": a.buyerCookieOK(r, o.ID)})
}

// ─── Вход сразу после оплаты ───────────────────────────────────────────
//
// Покупка в один шаг (QuickCheckout) заводит аккаунт без пароля, и раньше
// человек после оплаты шёл в почту за ссылкой. Теперь при оформлении заказа
// браузер получает куку buyer_order (id заказа + подпись), и после оплаты
// GET /api/orders/{id}/enter сразу пускает к урокам.
//
// Пускаем только в аккаунт, в который человек ещё ни разу не входил
// (Repo.PasswordUnknown): иначе, вписав чужую почту и заплатив, можно было бы
// войти в чужой кабинет. Остальных ведём на обычный вход с подставленной почтой.
const buyerCookie = "buyer_order"

func (a *App) buyerCookieSig(orderID uuid.UUID) string {
	mac := hmac.New(sha256.New, []byte(a.Cfg.JWTSecret))
	mac.Write([]byte("buyer:" + orderID.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

// setBuyerCookie – вызывается при оформлении заказа гостем.
func (a *App) setBuyerCookie(w http.ResponseWriter, r *http.Request, orderID uuid.UUID) {
	http.SetCookie(w, &http.Cookie{
		Name: buyerCookie, Value: orderID.String() + "." + a.buyerCookieSig(orderID),
		Path: "/", HttpOnly: true, Secure: IsHTTPSRequest(r), SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(24 * time.Hour),
	})
}

func (a *App) buyerCookieOK(r *http.Request, orderID uuid.UUID) bool {
	c, err := r.Cookie(buyerCookie)
	if err != nil {
		return false
	}
	id, sig, ok := strings.Cut(c.Value, ".")
	if !ok || id != orderID.String() {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(a.buyerCookieSig(orderID)))
}

// OrderEnter – GET /api/orders/{id}/enter: вход к урокам после оплаты.
func (a *App) OrderEnter(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	o, err := a.Repo.GetOrder(r.Context(), id)
	if err != nil || o.Status != "paid" || o.CourseID == uuid.Nil {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	next := "/cabinet"
	if c, err := a.Repo.GetCourseByID(r.Context(), o.CourseID); err == nil {
		next = "/cabinet/" + c.Slug
	}
	u, err := a.Repo.GetUser(r.Context(), o.UserID)
	if err != nil {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	unknown, _ := a.Repo.PasswordUnknown(r.Context(), u.ID)
	if !a.buyerCookieOK(r, o.ID) || !unknown || u.Role != "user" {
		http.Redirect(w, r, "/auth/login?email="+url.QueryEscape(u.Email)+"&next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	if err := a.startSession(w, r, u.ID, u.Role); err != nil {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: buyerCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// maskEmail: great-happy17@yandex.ru → gre•••@yandex.ru
func maskEmail(e string) string {
	at := strings.LastIndex(e, "@")
	if at <= 0 {
		return ""
	}
	name := []rune(e[:at])
	keep := 3
	if len(name) <= 3 {
		keep = 1
	}
	return string(name[:keep]) + "•••" + e[at:]
}
