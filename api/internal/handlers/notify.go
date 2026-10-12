package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/leshalarin/api/internal/notify"
)

// notifyAdmin – уведомление Алексею: письмо на LEAD_NOTIFY_EMAIL и то же
// самое простым текстом в Telegram (если прокладка настроена).
func (a *App) notifyAdmin(emoji, subject, htmlBody string) {
	a.Mail.Async(a.Cfg.LeadNotifyEmail, subject, htmlBody)
	a.TG.Async(emoji + subject + "\n\n" + notify.HTMLToText(htmlBody))
}

// notifySignup – новая регистрация на «Мягкий старт» (только Telegram).
func (a *App) notifySignup(name, email, source string, marketing bool) {
	msg := "🆕 Подписка на «Мягкий старт»\n" + strings.TrimSpace(name+" "+email)
	if source != "" {
		msg += "\nОткуда: " + source
	}
	if !marketing {
		msg += "\nБез согласия на письма – цепочка не пойдёт"
	}
	a.TG.Async(msg)
}

// Слежение за почтой: если письма не уходят ни через один ящик дольше
// mailAlertAfter – сообщение в Telegram; повтор раз в mailAlertRepeat, пока
// не починится; когда письмо снова ушло – «почта снова работает».
const (
	mailAlertAfter  = 30 * time.Minute
	mailAlertRepeat = 6 * time.Hour
)

type mailMonitor struct {
	alertedAt time.Time // когда последний раз предупредили (ноль – не предупреждали)
}

// check – один шаг слежения; возвращает текст для Telegram или "".
func (m *mailMonitor) check(now, failingSince time.Time, lastErr string, pending int) string {
	if failingSince.IsZero() {
		if m.alertedAt.IsZero() {
			return ""
		}
		m.alertedAt = time.Time{}
		return "✅ Почта с сайта снова работает. Письма, которые не ушли, отправятся сами в ближайшие минуты."
	}
	if now.Sub(failingSince) < mailAlertAfter {
		return ""
	}
	if !m.alertedAt.IsZero() && now.Sub(m.alertedAt) < mailAlertRepeat {
		return ""
	}
	m.alertedAt = now
	return fmt.Sprintf("⚠️ Письма с сайта не отправляются с %s по Москве.\n"+
		"Ждут отправки: %d. Они не потеряются – сайт будет пробовать снова до 3 суток.\n\n"+
		"Ошибка: %s", moscowNow(failingSince).Format("02.01 15:04"), pending, lastErr)
}

// RunMailMonitor – раз в минуту проверяет, уходят ли письма.
func RunMailMonitor(ctx context.Context, a *App) {
	if !a.TG.Enabled() {
		return
	}
	m := &mailMonitor{}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			since, lastErr := a.Mail.Health()
			pending := 0
			if !since.IsZero() {
				pending, _ = a.Repo.OutboxPending(ctx)
			}
			if msg := m.check(time.Now(), since, lastErr, pending); msg != "" {
				if err := a.TG.Send(msg); err != nil {
					slog.Warn("mail monitor notify failed", "err", err)
				}
			}
		}
	}
}
