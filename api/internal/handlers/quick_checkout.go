package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/leshalarin/api/internal/middleware"
)

// QuickCheckout – покупка курса в один шаг: имя + почта → ссылка на оплату.
// Без пароля и подтверждения почты (как «Точка перемен», см. ServiceCheckout).
//
// POST /api/courses/{slug}/quick-checkout {name, email, tariff, consent_pd}
//   - залогиненный пользователь покупает на свой аккаунт, name/email не нужны;
//   - иначе аккаунт находится по почте или создаётся (findOrCreateUser).
//
// Доступ выдаёт webhook Продамуса после оплаты; в письме об оплате – ссылка
// для входа (задать пароль), т.к. пароля у нового аккаунта нет.
func (a *App) QuickCheckout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email     string `json:"email"`
		Name      string `json:"name"`
		Tariff    string `json:"tariff"`
		ConsentPD bool   `json:"consent_pd"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	c, err := a.Repo.GetCourseBySlug(r.Context(), chi.URLParam(r, "slug"))
	if err != nil || c.Kind != "paid" {
		writeErr(w, 404, "not_found")
		return
	}
	// Цена и название – как в Checkout: пресет тарифа или courses.price_rub.
	price, title := 0, c.Title
	if presets, ok := tariffPresets[c.Slug]; ok {
		p, ok := presets[in.Tariff]
		if !ok || adminOnlyTariffs[in.Tariff] {
			writeErr(w, 400, "bad_tariff")
			return
		}
		price, _ = p.current()
		title = p.Title
	} else {
		if c.PriceRub == nil {
			writeErr(w, 404, "not_found")
			return
		}
		price = *c.PriceRub
	}

	uid, authed := middleware.UserID(r.Context())
	if !authed {
		if !c.IsPublished {
			writeErr(w, 404, "not_found")
			return
		}
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if !strings.Contains(email, "@") || !strings.Contains(email, ".") || len(email) < 6 {
			writeErr(w, 400, "bad_email")
			return
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			writeErr(w, 400, "name_required")
			return
		}
		if !in.ConsentPD {
			writeErr(w, 400, "consent_pd_required")
			return
		}
		uid, err = a.findOrCreateUser(r.Context(), email, name)
		if err != nil {
			a.alertCheckoutFail("Курс «"+c.Title+"»", email, "не удалось завести покупателя")
			writeErr(w, 500, "db")
			return
		}
		if err := a.Repo.SaveConsentPD(r.Context(), uid); err != nil {
			slog.Warn("quick checkout consent", "err", err)
		}
		a.tagUser(r.Context(), r, uid)
	}
	u, err := a.Repo.GetUser(r.Context(), uid)
	if err != nil {
		writeErr(w, 401, "no_user")
		return
	}
	if !c.IsPublished && u.Role != "admin" {
		writeErr(w, 404, "not_found")
		return
	}
	if has, _ := a.Repo.HasEnrollment(r.Context(), uid, c.ID); has {
		writeErr(w, 409, "already_enrolled")
		return
	}
	o, err := a.Repo.CreateOrder(r.Context(), uid, c.ID, price)
	if err != nil {
		a.alertCheckoutFail("Курс «"+c.Title+"»", u.Email, "не удалось создать заказ")
		writeErr(w, 500, "order_failed")
		return
	}
	a.tagOrder(r.Context(), r, o.ID)
	payURL, err := a.coursePaymentURL(o, u.Email, u.Name, u.Phone, title, price,
		a.Cfg.AppHost+"/courses/"+c.Slug, a.Cfg.AppHost+"/kurs-oplachen?course="+c.Slug)
	if err != nil {
		a.alertCheckoutFail("Курс «"+c.Title+"»", u.Email, "не удалось подписать ссылку на оплату")
		writeErr(w, 500, "sign_failed")
		return
	}
	writeJSON(w, 200, map[string]string{"payment_url": payURL, "order_id": o.ID.String()})
}
