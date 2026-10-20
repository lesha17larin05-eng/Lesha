package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"
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

// CheckStuckPaymentsForTest – один проход сторожа зависших оплат.
func (a *App) CheckStuckPaymentsForTest(ctx context.Context, now time.Time) error {
	return a.checkStuckPayments(ctx, now)
}

// CheckAbandonedPaymentsForTest – один проход поиска брошенных оплат.
func (a *App) CheckAbandonedPaymentsForTest(ctx context.Context, now time.Time) error {
	return a.checkAbandonedPayments(ctx, now)
}

// BuildAccessEmailForTest – письмо о доступе (тело и сгенерированный пароль).
func (a *App) BuildAccessEmailForTest(ctx context.Context, userID uuid.UUID, slug string) (string, string, error) {
	u, err := a.Repo.GetUser(ctx, userID)
	if err != nil {
		return "", "", err
	}
	body, pwd := a.buildAccessEmail(ctx, u, slug, accessEmail{Intro: "<p>intro</p>"})
	return body, pwd, nil
}

// DripHTMLForTest – письмо цепочки шага step для подписанного или нет.
func (a *App) DripHTMLForTest(step int, subscribed bool) string {
	return a.dripHTML(dripSteps[step-1], "Анна", uuid.New(), subscribed)
}

// TrackURLForTest – ссылка письма с учётом перехода.
func (a *App) TrackURLForTest(campaign string, userID uuid.UUID, link string) string {
	return a.trackURL(campaign, userID, link)
}

// DripHTMLForUserTest – письмо цепочки конкретному человеку.
func (a *App) DripHTMLForUserTest(step int, userID uuid.UUID) string {
	return a.dripHTML(dripSteps[step-1], "Анна", userID, true)
}
