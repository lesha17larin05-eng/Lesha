package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/middleware"
)

// tariffPreset — пресет тарифа для курса с несколькими ценами.
type tariffPreset struct {
	PriceRub int
	Title    string
}

// tariffPresets — мапа курсов с несколькими тарифами.
// Если у курса есть запись здесь — Checkout требует ?tariff=<key> и берёт
// цену и название отсюда вместо courses.price_rub / courses.title.
// Сделано без отдельной таблицы tariffs пока тарифов мало; легко мигрировать.
var tariffPresets = map[string]map[string]tariffPreset{
	"zdorovaya-spina": {
		"self":    {PriceRub: 3990, Title: "Здоровая спина — Самостоятельный"},
		"support": {PriceRub: 12990, Title: "Здоровая спина — С поддержкой"},
		// Временный тариф для проверки интеграции с Продамусом — после успешной
		// тестовой оплаты + возврата эту строку удалим.
		"test10": {PriceRub: 10, Title: "ТЕСТ — проверка интеграции"},
	},
}

func (a *App) Checkout(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	c, err := a.Repo.GetCourseBySlug(r.Context(), slug)
	if err != nil || c.Kind != "paid" {
		writeErr(w, 404, "not_found")
		return
	}
	// Выбор тарифа: либо пресет (для курсов в tariffPresets), либо courses.price_rub.
	price := 0
	title := c.Title
	if presets, ok := tariffPresets[c.Slug]; ok {
		tariffKey := r.URL.Query().Get("tariff")
		if tariffKey == "" {
			writeErr(w, 400, "tariff_required")
			return
		}
		p, ok := presets[tariffKey]
		if !ok {
			writeErr(w, 400, "bad_tariff")
			return
		}
		price = p.PriceRub
		title = p.Title
	} else {
		if c.PriceRub == nil {
			writeErr(w, 404, "not_found")
			return
		}
		price = *c.PriceRub
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
	o, err := a.Repo.CreateOrder(r.Context(), uid, c.ID, price)
	if err != nil {
		writeErr(w, 500, "order_failed")
		return
	}
	// Продамус считает HMAC от json_encode(ksort_recursive($data)).
	// Структура должна быть ВЛОЖЕННАЯ (products → array of objects),
	// а не плоская "products[0][name]". flatten() в prodamus.PaymentURL
	// сама превратит её в правильные query-ключи products%5B0%5D%5Bname%5D=…
	// Все значения — строки, как они будут в query string (иначе JSON и query
	// дадут разный HMAC).
	params := map[string]any{
		"do":             "pay",
		"order_id":       o.ID.String(),
		"order_num":      strconv.FormatInt(o.OrderNum, 10),
		"customer_email": u.Email,
		"products": []any{
			map[string]any{
				"name":     title,
				"price":    strconv.Itoa(price),
				"quantity": "1",
			},
		},
		"urlReturn":       a.Cfg.AppHost + "/cabinet/courses",
		"urlSuccess":      a.Cfg.AppHost + "/cabinet/courses?paid=" + c.Slug,
		"urlNotification": a.Cfg.AppHost + "/api/webhooks/prodamus",
		// ВАЖНО: касса в режиме самозанятого подмешивает npd_income_type в подпись
		// ДО проверки. Если этого параметра нет в наших params — Продамус считает
		// подпись с ним, а наша подпись посчитана без него → «Ошибка подписи».
		"npd_income_type": "FROM_INDIVIDUAL",
		// sys согласован с Продамусом ("leshalarin" 15.06.2026). Включаем в HMAC,
		// Продамус считает подпись с этим параметром — без него форма открывается
		// с пустым полем суммы.
		"sys": "leshalarin",
		// Рекомендация поддержки: callbackType=json упрощает сверку подписи
		// в webhook'ах (тело придёт в JSON, а не в php-keys formdata).
		"callbackType": "json",
	}
	// Имя и телефон из профиля — чтобы Продамус не подставлял UUID order_id в поле «Имя».
	if n := strings.TrimSpace(u.Name); n != "" {
		params["customer_name"] = n
	}
	if p := strings.TrimSpace(u.Phone); p != "" {
		params["customer_phone"] = p
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
	// TEMP debug: лог итогового URL чтобы понять что отправляется Продамусу.
	slog.Info("prodamus checkout url", "email", u.Email, "order_id", o.ID, "url", url)
	writeJSON(w, 200, map[string]string{"payment_url": url, "order_id": o.ID.String()})
}

// PayShortcut — короткая ссылка /pay/{order_id}. По order_id ищет заказ,
// генерит свежий подписанный payment_url и делает HTTP 302 на Продамус.
// Нужен потому что полный payment_url длиной 600+ символов плохо переживает
// копирование через мессенджеры и чаты — обрезается, и подпись ломается.
func (a *App) PayShortcut(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "order_id"))
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	o, err := a.Repo.GetOrder(r.Context(), id)
	if err != nil {
		http.Error(w, "order not found", 404)
		return
	}
	if o.Status != "pending" {
		// уже оплачен или отменён — отправим в кабинет
		http.Redirect(w, r, a.Cfg.AppHost+"/cabinet/courses", http.StatusFound)
		return
	}
	u, err := a.Repo.GetUser(r.Context(), o.UserID)
	if err != nil {
		http.Error(w, "user not found", 404)
		return
	}
	c, err := a.Repo.GetCourseByID(r.Context(), o.CourseID)
	if err != nil {
		http.Error(w, "course not found", 404)
		return
	}
	params := map[string]any{
		"do":             "pay",
		"order_id":       o.ID.String(),
		"order_num":      strconv.FormatInt(o.OrderNum, 10),
		"customer_email": u.Email,
		"products": []any{
			map[string]any{
				"name":     c.Title,
				"price":    strconv.Itoa(o.AmountRub),
				"quantity": "1",
			},
		},
		"urlReturn":       a.Cfg.AppHost + "/cabinet/courses",
		"urlSuccess":      a.Cfg.AppHost + "/cabinet/courses?paid=" + c.Slug,
		"urlNotification": a.Cfg.AppHost + "/api/webhooks/prodamus",
		"npd_income_type": "FROM_INDIVIDUAL",
		"sys":             "leshalarin",
		"callbackType":    "json",
	}
	if n := strings.TrimSpace(u.Name); n != "" {
		params["customer_name"] = n
	}
	if p := strings.TrimSpace(u.Phone); p != "" {
		params["customer_phone"] = p
	}
	url, err := a.Prodamus.PaymentURL(params)
	if err != nil {
		http.Error(w, "sign_failed", 500)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
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
	// urlencoded: сначала разбираем плоские ключи foo, foo[0][bar],
	// затем превращаем PHP-style индексы во вложенный объект — Продамус
	// подписывает именно вложенную структуру (см. python-prodamus → php2dict).
	flat := map[string]string{}
	for _, kv := range strings.Split(string(body), "&") {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		k, _ := urlDecode(kv[:eq])
		v, _ := urlDecode(kv[eq+1:])
		flat[k] = v
	}
	return php2dict(flat)
}

// php2dict превращает плоские ключи вида "foo", "products[0][name]" в
// вложенный map/slice. Если индексы чисто числовые — собираем slice
// (как PHP/JSON массив); иначе — map. Сохраняем порядок индексов
// через sort.
func php2dict(flat map[string]string) map[string]any {
	root := map[string]any{}
	bracketRe := regexp.MustCompile(`\[([^\]]*)\]`)
	for fullKey, val := range flat {
		head := fullKey
		var parts []string
		if i := strings.IndexByte(fullKey, '['); i > 0 {
			head = fullKey[:i]
			matches := bracketRe.FindAllStringSubmatch(fullKey[i:], -1)
			for _, m := range matches {
				parts = append(parts, m[1])
			}
		}
		if len(parts) == 0 {
			root[head] = val
			continue
		}
		// Идём вглубь, создавая map'ы по ключам. Числовые → потом превратим в slice.
		var cur any = root
		curMap := root
		path := append([]string{head}, parts...)
		for i, k := range path {
			if i == len(path)-1 {
				curMap[k] = val
				break
			}
			next, ok := curMap[k].(map[string]any)
			if !ok {
				next = map[string]any{}
				curMap[k] = next
			}
			curMap = next
			cur = next
		}
		_ = cur
	}
	// Рекурсивно: map, у которого все ключи — целые подряд от 0 → slice
	var convert func(v any) any
	convert = func(v any) any {
		m, ok := v.(map[string]any)
		if !ok {
			return v
		}
		for k, vv := range m {
			m[k] = convert(vv)
		}
		// проверка: все ключи числовые и заполнены подряд от 0
		allNum := len(m) > 0
		maxIdx := -1
		for k := range m {
			n, err := strconv.Atoi(k)
			if err != nil || n < 0 {
				allNum = false
				break
			}
			if n > maxIdx {
				maxIdx = n
			}
		}
		if !allNum || maxIdx+1 != len(m) {
			return m
		}
		arr := make([]any, len(m))
		for k, vv := range m {
			n, _ := strconv.Atoi(k)
			arr[n] = vv
		}
		return arr
	}
	result := convert(root).(map[string]any)
	return result
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
