package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/auth"
	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/middleware"
)

// tariffPreset – пресет тарифа для курса с несколькими ценами.
// PromoPriceRub/PromoUntil – стартовая цена со сроком: до PromoUntil
// включительно берётся PromoPriceRub, после – PriceRub (без правок кода).
type tariffPreset struct {
	PriceRub      int
	Title         string
	PromoPriceRub int
	PromoUntil    time.Time
}

// nowFn – текущее время; в тестах подменяется, чтобы проверить срок акции.
var nowFn = time.Now

// current – действующая цена тарифа и признак акции.
func (p tariffPreset) current() (price int, promo bool) {
	if p.PromoPriceRub > 0 && nowFn().Before(p.PromoUntil) {
		return p.PromoPriceRub, true
	}
	return p.PriceRub, false
}

// moscow – часовой пояс сроков акций (сервер может жить в UTC).
var moscow = time.FixedZone("MSK", 3*60*60)

// tariffPresets – мапа курсов с несколькими тарифами.
// Если у курса есть запись здесь – Checkout требует ?tariff=<key> и берёт
// цену и название отсюда вместо courses.price_rub / courses.title.
// Сделано без отдельной таблицы tariffs пока тарифов мало; легко мигрировать.
var tariffPresets = map[string]map[string]tariffPreset{
	"zdorovaya-spina": {
		"self":    {PriceRub: 3990, Title: "Здоровая спина – Самостоятельный"},
		"support": {PriceRub: 12990, Title: "Здоровая спина – С поддержкой"},
		// Временный тариф для проверки интеграции с Продамусом – после успешной
		// тестовой оплаты + возврата эту строку удалим.
		// Доступен ТОЛЬКО админам (см. проверку роли в Checkout).
		// 50 ₽ – минимальная сумма, которую принимает Продамус
		// (на 10 ₽ форма отвечает «сумма не может быть меньше 50»).
		"test10": {PriceRub: 50, Title: "ТЕСТ – проверка интеграции"},
	},
	// «Жонглирование: у вас получится». Оба тарифа открывают один и тот же курс;
	// «С разбором» – 30 дней разборов видео в Телеграме и личная онлайн-консультация 30 минут, вне кода.
	"zhonglirovanie": {
		// Стартовая цена 990 ₽ – до конца 18.10.2026 по Москве.
		"self": {PriceRub: 1490, Title: "Жонглирование – Самостоятельный",
			PromoPriceRub: 990, PromoUntil: time.Date(2026, 10, 19, 0, 0, 0, 0, moscow)},
		"support": {PriceRub: 3990, Title: "Жонглирование – С разбором"},
	},
}

// adminOnlyTariffs – тарифы, которые может оформить только admin
// (служебные/тестовые, не должны быть доступны обычным пользователям).
var adminOnlyTariffs = map[string]bool{"test10": true}

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
	tariffKey := ""
	if presets, ok := tariffPresets[c.Slug]; ok {
		tariffKey = r.URL.Query().Get("tariff")
		if tariffKey == "" {
			writeErr(w, 400, "tariff_required")
			return
		}
		p, ok := presets[tariffKey]
		if !ok {
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
	// Служебные тарифы (test10) доступны только админам – иначе любой,
	// кто узнал ключ тарифа, купит курс за тестовую цену.
	// Черновик курса (is_published=false) купить нельзя – видео может ещё
	// не быть на сервере. Админ может, чтобы проверить оплату до запуска.
	if !c.IsPublished && u.Role != "admin" {
		writeErr(w, 404, "not_found")
		return
	}
	if adminOnlyTariffs[tariffKey] && u.Role != "admin" {
		writeErr(w, 400, "bad_tariff")
		return
	}
	has, _ := a.Repo.HasEnrollment(r.Context(), uid, c.ID)
	if has {
		writeErr(w, 400, "already_enrolled")
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
		a.Cfg.AppHost+"/cabinet/courses", a.Cfg.AppHost+"/cabinet/courses?paid="+c.Slug)
	if err != nil {
		a.alertCheckoutFail("Курс «"+c.Title+"»", u.Email, "не удалось подписать ссылку на оплату")
		writeErr(w, 500, "sign_failed")
		return
	}
	writeJSON(w, 200, map[string]string{"payment_url": payURL, "order_id": o.ID.String()})
}

// coursePaymentURL – подписанная ссылка Продамуса на оплату курса
// (общая для Checkout и QuickCheckout). В тестовом режиме – fake-payment.
func (a *App) coursePaymentURL(o *db.Order, email, name, phone, title string, price int, returnURL, successURL string) (string, error) {
	if a.Cfg.ProdamusTestMode {
		return a.Cfg.AppHost + "/api/dev/fake-payment?order_id=" + o.ID.String(), nil
	}
	// Продамус считает HMAC от json_encode(ksort_recursive($data)).
	// Структура должна быть ВЛОЖЕННАЯ (products → array of objects),
	// а не плоская "products[0][name]". flatten() в prodamus.PaymentURL
	// сама превратит её в правильные query-ключи products%5B0%5D%5Bname%5D=…
	// Все значения – строки, как они будут в query string (иначе JSON и query
	// дадут разный HMAC).
	params := map[string]any{
		"do":             "pay",
		"order_id":       o.ID.String(),
		"order_num":      strconv.FormatInt(o.OrderNum, 10),
		"customer_email": email,
		"products": []any{
			map[string]any{
				"name":     title,
				"price":    strconv.Itoa(price),
				"quantity": "1",
			},
		},
		"urlReturn":       returnURL,
		"urlSuccess":      successURL,
		"urlNotification": a.Cfg.AppHost + "/api/webhooks/prodamus",
		// ВАЖНО: касса в режиме самозанятого подмешивает npd_income_type в подпись
		// ДО проверки. Если этого параметра нет в наших params – Продамус считает
		// подпись с ним, а наша подпись посчитана без него → «Ошибка подписи».
		"npd_income_type": "FROM_INDIVIDUAL",
		// sys согласован с Продамусом ("leshalarin" 15.06.2026). Включаем в HMAC,
		// Продамус считает подпись с этим параметром – без него форма открывается
		// с пустым полем суммы.
		"sys": "leshalarin",
		// Рекомендация поддержки: callbackType=json упрощает сверку подписи
		// в webhook'ах (тело придёт в JSON, а не в php-keys formdata).
		"callbackType": "json",
	}
	// Имя и телефон – чтобы Продамус не подставлял UUID order_id в поле «Имя».
	if n := strings.TrimSpace(name); n != "" {
		params["customer_name"] = n
	}
	if p := strings.TrimSpace(phone); p != "" {
		params["customer_phone"] = p
	}
	url, err := a.Prodamus.PaymentURL(params)
	if err != nil {
		return "", err
	}
	slog.Info("prodamus checkout url", "email", email, "order_id", o.ID)
	return url, nil
}

// PayShortcut – короткая ссылка /pay/{order_id}. По order_id ищет заказ,
// генерит свежий подписанный payment_url и делает HTTP 302 на Продамус.
// Нужен потому что полный payment_url длиной 600+ символов плохо переживает
// копирование через мессенджеры и чаты – обрезается, и подпись ломается.
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
		// уже оплачен или отменён – отправим в кабинет
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

// FakePayment – dev-only endpoint that simulates a successful Prodamus webhook.
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
		if o.CourseID == uuid.Nil {
			a.serviceOrderPaid(r.Context(), o)
			http.Redirect(w, r, a.Cfg.AppHost+"/oplacheno", http.StatusFound)
			return
		}
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
	// Ищем наш заказ.
	//
	// Продамус кладёт СВОЙ внутренний номер в `order_id`, а наш идентификатор
	// возвращает в `order_num` – в обратную сторону от того, как мы их
	// отправляли. Поэтому проверяем оба поля и каждое – и как UUID заказа,
	// и как человекочитаемый номер. Email для матчинга не используем
	// (см. CLAUDE.md): по нему нельзя достоверно понять, что именно оплачено.
	var orderID uuid.UUID
	for _, key := range []string{"order_num", "order_id"} {
		v, _ := parsed[key].(string)
		if v == "" || orderID != uuid.Nil {
			continue
		}
		if id, err := uuid.Parse(v); err == nil {
			orderID = id
			continue
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			if o, err := a.Repo.GetOrderByNum(r.Context(), n); err == nil {
				orderID = o.ID
			}
		}
	}
	if orderID == uuid.Nil {
		// Оплата по ссылке, выставленной вручную в личном кабинете Продамуса:
		// заказа на сайте нет и быть не может. Автоматически выдавать курс
		// по названию товара нельзя (легко перепутать курсы), поэтому шлём
		// Алексею письмо, чтобы он открыл доступ в пару кликов.
		a.Repo.MarkWebhookProcessed(r.Context(), whID, nil, "no_order")
		a.notifyUnmatchedPayment(parsed)
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

	// Возврат денег. Точное значение статуса у Продамуса может отличаться
	// («refund», «refunded», «возврат»), поэтому смотрим по вхождению.
	low := strings.ToLower(status)
	if strings.Contains(low, "refund") || strings.Contains(low, "возврат") {
		a.handleRefund(r, o, status)
		a.Repo.MarkWebhookProcessed(r.Context(), whID, &orderID, "")
		w.WriteHeader(200)
		return
	}

	if status == "success" || status == "paid" {
		if o.Status == "pending" {
			pid, _ := parsed["prodamus_order_id"].(string)
			if pid == "" {
				// номер на стороне Продамуса – для сверки в их кабинете
				pid, _ = parsed["order_id"].(string)
			}
			_ = a.Repo.MarkOrderPaid(r.Context(), o.ID, pid)
			// Заказ на услугу («Точка перемен») курса не открывает:
			// человек получает письмо с анкетой, дальше работаем лично.
			if o.CourseID == uuid.Nil {
				a.serviceOrderPaid(r.Context(), o)
				w.WriteHeader(200)
				return
			}
			_ = a.Repo.Grant(r.Context(), o.UserID, o.CourseID, "purchase", nil)
			u, _ := a.Repo.GetUser(r.Context(), o.UserID)
			c, _ := a.Repo.GetCourseByID(r.Context(), o.CourseID)
			if u != nil && c != nil {
				late := ""
				if a.Repo.WasStuckAlerted(r.Context(), o.ID) {
					late = "\n✅ Подтверждение пришло с опозданием – доступ открылся сам, письмо покупателю ушло. Вручную ничего делать не нужно."
				}
				a.TG.Async("💰 Оплачен курс «" + c.Title + "»\n" + strings.TrimSpace(u.Name+" "+u.Email) +
					"\nСумма: " + strconv.Itoa(o.AmountRub) + " ₽, заказ №" + strconv.FormatInt(o.OrderNum, 10) + late)
				// Письмо о доступе: кнопка «Перейти к урокам» (вход без пароля,
				// 7 дней) и данные для входа. Покупка в один шаг заводит аккаунт
				// без известного человеку пароля – тогда пароль генерируется.
				body, _ := a.buildAccessEmail(r.Context(), u, c.Slug, accessEmail{
					Intro: "<p style=\"margin:0 0 16px;\">Спасибо за оплату! Доступ к курсу <b>«" + html.EscapeString(c.Title) +
						"»</b> уже открыт. Уроки – в личном кабинете, доступ действует год, темп выбираете сами.</p>",
					Outro: "<p style=\"margin:0 0 16px;\">Вопросы по курсу или оплате – просто ответьте на это письмо " +
						"или напишите в Телеграм: <a href=\"https://t.me/larin_lesha\" style=\"" + dripLinkStyle + "\">@larin_lesha</a>.</p>" +
						"<p style=\"margin:0 0 16px;\">Хорошей практики!<br>Алексей Ларин</p>",
				})
				a.Mail.Async(u.Email, "Оплата получена – курс «"+c.Title+"» открыт", body)
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
	// затем превращаем PHP-style индексы во вложенный объект – Продамус
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
// вложенный map/slice. Если индексы чисто числовые – собираем slice
// (как PHP/JSON массив); иначе – map. Сохраняем порядок индексов
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
	// Рекурсивно: map, у которого все ключи – целые подряд от 0 → slice
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

// notifyUnmatchedPayment – письмо Алексею об оплате, которой не нашлось пары
// среди заказов сайта (оплата по ссылке, выставленной вручную).
func (a *App) notifyUnmatchedPayment(parsed map[string]any) {
	status, _ := parsed["payment_status"].(string)
	if status != "success" && status != "paid" {
		return
	}
	str := func(k string) string {
		v, _ := parsed[k].(string)
		return html.EscapeString(v)
	}
	product := ""
	if items, ok := parsed["products"].([]any); ok && len(items) > 0 {
		if first, ok := items[0].(map[string]any); ok {
			if name, ok := first["name"].(string); ok {
				product = html.EscapeString(name)
			}
		}
	}
	email := str("customer_email")
	grantURL := a.Cfg.AppHost + "/admin/grant"
	if email != "" {
		grantURL += "?email=" + url.QueryEscape(email)
	}
	a.notifyAdmin("⚠️ ", "Оплата без заказа на сайте – нужно выдать доступ",
		"<p>Пришла оплата по ссылке, выставленной вручную в кабинете Продамуса. "+
			"На сайте такого заказа нет, поэтому доступ автоматически не выдан.</p>"+
			"<p>Если это курс в лагере – делать ничего не нужно, он идёт офлайн и доступов "+
			"на сайте не требует. Если онлайн-курс – откройте доступ по ссылке внизу.</p>"+
			"<p><b>Почта:</b> "+email+"<br>"+
			"<b>Сумма:</b> "+str("sum")+" ₽<br>"+
			"<b>Товар:</b> "+product+"<br>"+
			"<b>Имя/комментарий:</b> "+str("order_num")+" "+str("customer_extra")+"<br>"+
			"<b>Телефон:</b> "+str("customer_phone")+"</p>"+
			"<p><a href=\""+grantURL+"\">Выдать доступ</a></p>")
}

// handleRefund – деньги вернули: заказ помечаем возвращённым, закрываем
// доступ, выданный за эту покупку, и пишем Алексею.
func (a *App) handleRefund(r *http.Request, o *db.Order, status string) {
	ctx := r.Context()
	if err := a.Repo.MarkOrderRefunded(ctx, o.ID); err != nil {
		slog.Warn("refund: order not marked", "order", o.ID, "err", err)
	}
	revoked, err := a.Repo.RevokePurchasedEnrollment(ctx, o.UserID, o.CourseID)
	if err != nil {
		slog.Warn("refund: enrollment not revoked", "order", o.ID, "err", err)
	}
	slog.Info("refund processed", "order_num", o.OrderNum, "revoked", revoked, "status", status)

	u, _ := a.Repo.GetUser(ctx, o.UserID)
	c, _ := a.Repo.GetCourseByID(ctx, o.CourseID)
	if u == nil || c == nil {
		return
	}
	accessLine := "Доступ к курсу закрыт."
	if !revoked {
		accessLine = "Доступ остался: он был выдан не за эту покупку (подарен или бесплатный), " +
			"поэтому автоматически я его не трогаю."
	}
	a.notifyAdmin("↩️ ", "Возврат оплаты – заказ №"+strconv.FormatInt(o.OrderNum, 10),
		"<p>Продамус сообщил о возврате денег.</p>"+
			"<p><b>Заказ:</b> №"+strconv.FormatInt(o.OrderNum, 10)+"<br>"+
			"<b>Курс:</b> "+html.EscapeString(c.Title)+"<br>"+
			"<b>Покупатель:</b> "+html.EscapeString(u.Email)+"<br>"+
			"<b>Сумма:</b> "+strconv.Itoa(o.AmountRub)+" ₽</p>"+
			"<p>"+accessLine+"</p>"+
			"<p><a href=\""+a.Cfg.AppHost+"/admin/users\">Открыть админку</a></p>")
}

// ─── Оплата услуг ───────────────────────────────────────────────────────
//
// «Точка перемен» – не курс, доступа к урокам она не даёт. Поэтому заказ
// создаётся без course_id (см. миграцию 016), а после оплаты человек
// получает письмо с анкетой, а не запись на курс.
//
// Регистрации не требуем: человек оставляет почту и имя, аккаунт заводится
// сам. Для покупки за 2 990 ₽ заставлять придумывать пароль – значит терять
// половину тех, кто уже решился.
// Услуги с оплатой на сайте (без курса в кабинете).
type service struct {
	Title    string // название в чеке и уведомлениях
	PriceRub int
	Page     string // страница услуги – туда возвращает «Назад» из Продамуса
	Seats    int    // сколько мест (0 – без ограничения)
	// ShowLeftMax – больше скольких свободных мест не показывать на сайте
	// (0 – показывать как есть). Нужен, когда часть мест обещана в личке,
	// но ещё не оплачена: счётчик сразу честный, а когда эти люди оплатят,
	// число не прыгнет вниз второй раз. Оплату не ограничивает.
	ShowLeftMax int
}

var services = map[string]service{
	"start":  {Title: "«Точка перемен» – занятие и неделя сопровождения", PriceRub: 2990, Page: "/start"},
	"gruppa": {Title: "Групповое ведение – 8 недель (поток с 7 ноября)", PriceRub: 9900, Page: "/gruppa", Seats: 7, ShowLeftMax: 4},
}

// ServiceSeats – GET /api/services/{service}/seats: сколько мест всего и
// сколько осталось (по оплаченным заказам). Публичный – страница группы
// показывает «осталось N из M».
func (a *App) ServiceSeats(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "service")
	svc, ok := services[name]
	if !ok || svc.Seats == 0 {
		writeErr(w, 404, "not_found")
		return
	}
	n, err := a.Repo.CountPaidServiceOrders(r.Context(), name)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	left := svc.Seats - n
	if svc.ShowLeftMax > 0 && left > svc.ShowLeftMax {
		left = svc.ShowLeftMax
	}
	if left < 0 {
		left = 0
	}
	writeJSON(w, 200, map[string]int{"seats": svc.Seats, "left": left})
}

func (a *App) ServiceCheckout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Service   string `json:"service"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		ConsentPD bool   `json:"consent_pd"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	svc, ok := services[in.Service]
	if !ok {
		writeErr(w, 400, "unknown_service")
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
	if svc.Seats > 0 {
		if n, err := a.Repo.CountPaidServiceOrders(r.Context(), in.Service); err == nil && n >= svc.Seats {
			writeErr(w, 409, "sold_out")
			return
		}
	}

	// Находим или заводим человека – тем же способом, что и подписка на письма.
	uid, err := a.findOrCreateUser(r.Context(), email, name)
	if err != nil {
		a.alertCheckoutFail(svc.Title, email, "не удалось завести покупателя")
		writeErr(w, 500, "db")
		return
	}
	if err := a.Repo.SaveConsentPD(r.Context(), uid); err != nil {
		slog.Warn("service consent", "err", err)
	}

	o, err := a.Repo.CreateServiceOrder(r.Context(), uid, in.Service, svc.PriceRub)
	if err != nil {
		a.alertCheckoutFail(svc.Title, email, "не удалось создать заказ")
		writeErr(w, 500, "order_failed")
		return
	}
	a.tagUser(r.Context(), r, uid)
	a.tagOrder(r.Context(), r, o.ID)

	// В тестовом режиме Продамуса платёжной ссылки нет – отдаём локальную
	// заглушку, как и для курсов.
	if a.Cfg.ProdamusTestMode && a.Prodamus.PayformURL == "" {
		writeJSON(w, 200, map[string]any{"url": a.Cfg.AppHost + "/fake-payment?order=" + o.ID.String()})
		return
	}

	params := map[string]any{
		"do":             "pay",
		"order_id":       o.ID.String(),
		"order_num":      strconv.FormatInt(o.OrderNum, 10),
		"customer_email": email,
		"customer_name":  name,
		"products": []any{
			map[string]any{
				"name":     svc.Title,
				"price":    strconv.Itoa(svc.PriceRub),
				"quantity": "1",
			},
		},
		"urlReturn":       a.Cfg.AppHost + svc.Page,
		"urlSuccess":      a.Cfg.AppHost + "/oplacheno",
		"urlNotification": a.Cfg.AppHost + "/api/webhooks/prodamus",
		"npd_income_type": "FROM_INDIVIDUAL",
		"sys":             "leshalarin",
		"callbackType":    "json",
	}
	url, err := a.Prodamus.PaymentURL(params)
	if err != nil {
		a.alertCheckoutFail(svc.Title, email, "не удалось подписать ссылку на оплату")
		writeErr(w, 500, "payment_url")
		return
	}
	writeJSON(w, 200, map[string]any{"url": url})
}

// findOrCreateUser – общий помощник для гостевых сценариев (оплата услуги,
// подписка на письма). Пароль случайный: войти по нему нельзя, человек
// восстановит его обычным способом, если захочет в кабинет.
func (a *App) findOrCreateUser(ctx context.Context, email, name string) (uuid.UUID, error) {
	u, err := a.Repo.GetUserByEmail(ctx, email)
	if err == nil {
		if name != "" && strings.TrimSpace(u.Name) == "" {
			_ = a.Repo.UpdateUserName(ctx, u.ID, name)
		}
		return u.ID, nil
	}
	if !errors.Is(err, db.ErrNotFound) {
		return uuid.Nil, err
	}
	raw, _, err := auth.RandomToken(32)
	if err != nil {
		return uuid.Nil, err
	}
	hash, err := auth.HashPassword(raw)
	if err != nil {
		return uuid.Nil, err
	}
	return a.Repo.CreateUser(ctx, email, hash, name, "user")
}

// serviceOrderPaid — что происходит после оплаты услуги: письмо покупателю
// с анкетой и уведомление Алексею. Доступа к курсам здесь не выдаётся.
func (a *App) serviceOrderPaid(ctx context.Context, o *db.Order) {
	svc, ok := services[o.Service]
	if !ok {
		return
	}
	u, _ := a.Repo.GetUser(ctx, o.UserID)
	if u == nil {
		return
	}
	// «Люди»: оплативший – со статусом «купил» (по почте находим или создаём).
	if err := a.Repo.MarkLeadBought(ctx, u.Name, u.Email, o.Service,
		time.Now().In(moscow).Format("02.01.2006")+": оплатил(а) «"+svc.Title+"», "+strconv.Itoa(o.AmountRub)+" ₽"); err != nil {
		slog.Warn("people: mark bought", "email", u.Email, "err", err)
	}
	if o.Service == "gruppa" {
		a.gruppaOrderPaid(u, o, svc)
		return
	}
	anketa := a.anketaURL(o.UserID)
	a.Mail.Async(u.Email, "Оплата получена – «Точка перемен»",
		"<p>Здравствуйте! Оплата получена, спасибо.</p>"+
			"<p><b>Первый шаг – анкета.</b> 5–7 минут: цель, что беспокоит, опыт и удобное время для занятия. "+
			"Она нужна, чтобы не тратить занятие на расспросы и сразу собрать план под вас.</p>"+
			`<p><a href="`+anketa+`" style="display:inline-block;background:#e8652a;color:#fff;text-decoration:none;`+
			`padding:12px 24px;border-radius:100px;font-weight:600">Заполнить анкету&nbsp;→</a></p>`+
			"<p>После анкеты я напишу вам в течение 1&#8209;2 дней и договоримся о времени.</p>"+
			"<p>Если удобнее сразу написать мне самому – "+
			"<a href=\"https://t.me/larin_lesha\">@larin_lesha</a> в Телеграме.</p>"+
			"<p>Если после занятия решите, что это не ваше – верну деньги полностью.</p>"+
			"<p>Алексей Ларин</p>")

	a.notifyAdmin("💰 ", "Оплачена «Точка перемен»",
		"<p><b>Кто:</b> "+html.EscapeString(u.Name)+" &lt;"+html.EscapeString(u.Email)+"&gt;</p>"+
			"<p><b>Что:</b> "+html.EscapeString(svc.Title)+"</p>"+
			"<p><b>Сумма:</b> "+strconv.Itoa(o.AmountRub)+" ₽</p>"+
			"<p><b>Заказ:</b> №"+strconv.FormatInt(o.OrderNum, 10)+"</p>"+
			"<p>Человеку ушло письмо со ссылкой на анкету – ответы придут отдельным письмом. "+
			"Ссылка на его анкету (если нужно переслать): <a href=\""+anketa+"\">"+anketa+"</a></p>")
}

// gruppaOrderPaid – оплачено групповое ведение: письмо с анкетой и замерами,
// уведомление Алексею.
func (a *App) gruppaOrderPaid(u *db.User, o *db.Order, svc service) {
	anketa := a.anketaURLKind(o.UserID, "gruppa")
	a.Mail.Async(u.Email, "Вы в группе – групповое ведение с 7 ноября",
		"<p>Здравствуйте! Оплата получена – вы в группе, я очень рад 🙂</p>"+
			"<p><b>Первый шаг – анкета и замеры.</b> Минут 10: цель, опыт, здоровье, удобные дни "+
			"и простые замеры (рост, вес, талия, бёдра, задержка дыхания). По анкете я соберу ваш план на первую неделю, "+
			"а замеры сравним в конце.</p>"+
			mailButton(anketa, "Заполнить анкету")+
			"<p>Лучше заполнить сразу, сегодня или завтра, – так я спокойно соберу ваш план до старта.</p>"+
			"<p><b>Фото – по желанию.</b> Если хотите видеть изменения не только в цифрах, пришлите мне лично "+
			"в <a href=\"https://t.me/larin_lesha\">Телеграм</a> 3 фото – спереди, сбоку и сзади, в купальнике или облегающей одежде, "+
			"при дневном свете. Их увижу только я.</p>"+
			"<p>Первый созвон – суббота, 7 ноября, в 10:10 по Москве: знакомимся, и каждый получает свой план. "+
			"Дальше созвоны – каждую субботу в это же время. Ссылку на чат группы пришлю ближе к старту, когда все соберутся.</p>"+
			"<p>Если что-то непонятно – просто ответьте на это письмо или напишите мне в "+
			"<a href=\"https://t.me/larin_lesha\">Телеграм</a>.</p>"+
			"<p>Алексей Ларин</p>")

	a.notifyAdmin("💰 ", "Оплачено групповое ведение",
		"<p><b>Кто:</b> "+html.EscapeString(u.Name)+" &lt;"+html.EscapeString(u.Email)+"&gt;</p>"+
			"<p><b>Что:</b> "+html.EscapeString(svc.Title)+"</p>"+
			"<p><b>Сумма:</b> "+strconv.Itoa(o.AmountRub)+" ₽</p>"+
			"<p><b>Заказ:</b> №"+strconv.FormatInt(o.OrderNum, 10)+"</p>"+
			"<p>Человеку ушло письмо с анкетой и замерами. Если за 2 дня не заполнит – сайт сам напомнит письмом. Ссылка на его анкету: <a href=\""+anketa+"\">"+anketa+"</a></p>")
}

// mailButton – оранжевая кнопка-ссылка для писем.
func mailButton(href, text string) string {
	return `<p><a href="` + href + `" style="display:inline-block;background:#e8652a;color:#fff;text-decoration:none;` +
		`padding:12px 24px;border-radius:100px;font-weight:600">` + text + `&nbsp;→</a></p>`
}

// anketaRemindAfter – через сколько после оплаты группы напоминать про анкету.
const anketaRemindAfter = 48 * time.Hour

// checkAnketaReminders – одно письмо-напоминание тем, кто оплатил группу,
// но за 2 дня не заполнил анкету. Шлём днём (9–21 по Москве), не ночью.
func (a *App) checkAnketaReminders(ctx context.Context, now time.Time) error {
	if h := moscowNow(now).Hour(); h < 9 || h >= 21 {
		return nil
	}
	due, err := a.Repo.DueAnketaReminders(ctx, now.Add(-anketaRemindAfter))
	if err != nil {
		return err
	}
	for _, d := range due {
		if err := a.Repo.MarkAnketaReminded(ctx, d.OrderID); err != nil {
			return err
		}
		anketa := a.anketaURLKind(d.UserID, "gruppa")
		a.Mail.Async(d.Email, "Анкета ждёт вас",
			"<p>Здравствуйте! Напоминаю про анкету – 10 минут, и я смогу собрать ваш план на первую неделю.</p>"+
				mailButton(anketa, "Заполнить анкету")+
				"<p>Если что-то неясно – просто ответьте на это письмо.</p><p>Алексей Ларин</p>")
		a.TG.Async("🔔 Напомнил про анкету группы: " + strings.TrimSpace(d.Name+" "+d.Email))
	}
	return nil
}
