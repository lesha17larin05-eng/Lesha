package handlers

import (
	"context"
	"time"
)

// SendOneDripEmailForTest открывает отправщик цепочки для интеграционных тестов.
func (a *App) SendOneDripEmailForTest(ctx context.Context, now time.Time) error {
	return a.sendOneDripEmail(ctx, now)
}

// SetNowForTest подменяет «текущее время» для расчёта стартовой цены тарифа.
// Возвращает функцию отката.
func SetNowForTest(t time.Time) func() {
	prev := nowFn
	nowFn = func() time.Time { return t }
	return func() { nowFn = prev }
}

// RetryOutboxForTest – одна порция повторной отправки из email_outbox.
func (a *App) RetryOutboxForTest(ctx context.Context, now time.Time) error {
	return a.retryOutbox(ctx, now)
}
