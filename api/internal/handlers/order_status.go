package handlers

import (
	"net/http"
	"strings"

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
	hint := ""
	if u, err := a.Repo.GetUser(r.Context(), o.UserID); err == nil {
		hint = maskEmail(u.Email)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]string{"status": o.Status, "email_hint": hint})
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
