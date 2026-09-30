package handlers

import (
	"context"
	"time"
)

// SendOneDripEmailForTest открывает отправщик цепочки для интеграционных тестов.
func (a *App) SendOneDripEmailForTest(ctx context.Context, now time.Time) error {
	return a.sendOneDripEmail(ctx, now)
}
