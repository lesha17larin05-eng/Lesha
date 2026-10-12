package handlers

import (
	"context"
	"html"
	"log/slog"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// ─── Зависшие оплаты ─────────────────────────────────────────────────────
//
// Продамус возвращает покупателя на /kurs-oplachen сразу после оплаты, а
// подтверждение (webhook) шлёт отдельно. При сбоях у Продамуса подтверждение
// не приходит – деньги списаны, а доступ не открыт. Страница после оплаты
// отмечает возврат (GET /api/orders/{id}/status?returned=1 → returned_at),
// а этот сторож раз в минуту ищет заказы, по которым покупатель вернулся
// больше stuckAfter назад, а подтверждения так и нет, и пишет Алексею.

const stuckAfter = 10 * time.Minute

func (a *App) checkStuckPayments(ctx context.Context, now time.Time) error {
	list, err := a.Repo.StuckOrders(ctx, now.Add(-stuckAfter))
	if err != nil {
		return err
	}
	for _, s := range list {
		what := "Курс «" + s.CourseTitle + "»"
		grant := a.Cfg.AppHost + "/admin/grant?email=" + url.QueryEscape(s.Email)
		action := "<p>Скорее всего, сбой у Продамуса. Проверьте платёж в кабинете Продамуса – если деньги пришли, " +
			"<a href=\"" + grant + "\">откройте доступ вручную</a> (курс «" + html.EscapeString(s.CourseTitle) + "») " +
			"и напишите покупателю. Если подтверждение придёт позже – доступ откроется сам, письмо уйдёт само.</p>"
		if s.CourseSlug == "" {
			what = "Услуга «" + s.Service + "»"
			action = "<p>Скорее всего, сбой у Продамуса. Проверьте платёж в кабинете Продамуса и, если деньги пришли, " +
				"свяжитесь с покупателем напрямую.</p>"
		}
		a.notifyAdmin("⏳ ", "Оплата без подтверждения – заказ №"+strconv.FormatInt(s.OrderNum, 10),
			"<p>Покупатель вернулся с оплаты с отметкой «успешно» в "+
				moscowNow(s.ReturnedAt).Format("15:04")+" по Москве, но подтверждения от Продамуса нет уже "+
				strconv.Itoa(int(stuckAfter.Minutes()))+" минут. Доступ пока не открыт.</p>"+
				"<p><b>"+html.EscapeString(what)+"</b><br>"+
				"<b>Покупатель:</b> "+html.EscapeString(s.Name)+" "+html.EscapeString(s.Email)+"<br>"+
				"<b>Сумма:</b> "+strconv.Itoa(s.AmountRub)+" ₽</p>"+action)
		if err := a.Repo.MarkStuckAlerted(ctx, s.ID); err != nil {
			return err
		}
	}
	return nil
}

// RunStuckPaymentWatcher – раз в минуту ищет зависшие оплаты.
func RunStuckPaymentWatcher(ctx context.Context, a *App) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := a.checkStuckPayments(ctx, time.Now()); err != nil {
				slog.Warn("stuck payments check failed", "err", err)
			}
			if err := a.checkAbandonedPayments(ctx, time.Now()); err != nil {
				slog.Warn("abandoned payments check failed", "err", err)
			}
		}
	}
}

// ─── Сбой при переходе к оплате ──────────────────────────────────────────
//
// Если сайт не смог создать заказ или ссылку на оплату, покупатель видит
// ошибку и, скорее всего, уходит. Сообщаем сразу – но не чаще раза в
// checkoutAlertEvery, чтобы при массовом сбое не завалить Телеграм.

const checkoutAlertEvery = 30 * time.Minute

var checkoutAlert struct {
	mu   sync.Mutex
	last time.Time
}

func (a *App) alertCheckoutFail(what, email, reason string) {
	slog.Warn("checkout failed", "what", what, "reason", reason)
	checkoutAlert.mu.Lock()
	if !checkoutAlert.last.IsZero() && time.Since(checkoutAlert.last) < checkoutAlertEvery {
		checkoutAlert.mu.Unlock()
		return
	}
	checkoutAlert.last = time.Now()
	checkoutAlert.mu.Unlock()
	who := ""
	if email != "" {
		who = "\nПокупатель: " + email
	}
	a.TG.Async("🛑 Покупатель не смог перейти к оплате\n" + what + who + "\nОшибка: " + reason +
		"\n\nПокупатель увидел сообщение об ошибке. Если повторится – напишите разработчику.")
}

// ─── Брошенная оплата ────────────────────────────────────────────────────
//
// Человек ввёл имя и почту, перешёл к оплате – и не оплатил за
// abandonedAfter. Это тёплый покупатель: Алексей может написать лично.
// Смотрим только заказы последних суток (старые «висящие» заказы не
// поднимаем) и не трогаем тех, кто потом попробовал ещё раз или купил.

const abandonedAfter = time.Hour

func (a *App) checkAbandonedPayments(ctx context.Context, now time.Time) error {
	list, err := a.Repo.AbandonedOrders(ctx, now.Add(-24*time.Hour), now.Add(-abandonedAfter))
	if err != nil {
		return err
	}
	for _, s := range list {
		what := "Курс «" + s.CourseTitle + "»"
		if s.CourseSlug == "" {
			what = "Услуга «" + s.Service + "»"
		}
		who := s.Email
		if s.Name != "" {
			who = s.Name + ", " + s.Email
		}
		a.TG.Async("🛒 Не завершил оплату – заказ №" + strconv.FormatInt(s.OrderNum, 10) + "\n" +
			who + "\n" + what + ", " + strconv.Itoa(s.AmountRub) + " ₽\n" +
			"Перешёл к оплате в " + moscowNow(s.CreatedAt).Format("15:04") + " по Москве и не оплатил.\n\n" +
			"Можно написать лично: спросить, что остановило, и помочь с оплатой.")
		if err := a.Repo.MarkAbandonedAlerted(ctx, s.ID); err != nil {
			return err
		}
	}
	return nil
}
