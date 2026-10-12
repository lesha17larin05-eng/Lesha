package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/leshalarin/api/internal/auth"
)

// Повторная отправка разовых писем. Если письмо из Mail.Async не ушло ни
// через основной, ни через запасной ящик, оно попадает в email_outbox
// (см. main.go), и этот воркер пробует снова: через 5 минут, потом с паузой
// +15 минут на каждую попытку (максимум 2 часа), до 3 суток.

const outboxTick = time.Minute

// RunOutboxWorker – фоновая повторная отправка. Останавливается по отмене контекста.
func RunOutboxWorker(ctx context.Context, a *App) {
	t := time.NewTicker(outboxTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := a.retryOutbox(ctx, time.Now()); err != nil {
				slog.Warn("outbox worker", "err", err)
			}
		}
	}
}

// retryOutbox – одна порция повторов. Если первое письмо порции снова не
// ушло, остальные не трогаем: почта всё ещё лежит, попробуем на следующем тике.
func (a *App) retryOutbox(ctx context.Context, now time.Time) error {
	due, err := a.Repo.DueEmails(ctx, now, 20)
	if err != nil {
		return err
	}
	for _, e := range due {
		if err := a.Mail.Send(e.To, e.Subject, e.Body); err != nil {
			slog.Warn("outbox retry failed", "to", e.To, "attempt", e.Attempts+1, "err", err)
			return a.Repo.MarkEmailFailed(ctx, e.ID, err.Error())
		}
		slog.Info("outbox retry sent", "to", e.To, "attempt", e.Attempts+1)
		if err := a.Repo.MarkEmailSent(ctx, e.ID); err != nil {
			return err
		}
	}
	return nil
}

// ResendFreeAccess – заново выдаёт пароль и отправляет письмо «доступ к
// «Мягкому старту» открыт» (на случай, если первое письмо потерялось).
// Отправка синхронная: вызывающий видит, ушло ли письмо.
// Запуск на сервере: docker compose exec api /app/server resend-access a@b.ru c@d.ru
func (a *App) ResendFreeAccess(ctx context.Context, email string) error {
	u, err := a.Repo.GetUserByEmail(ctx, strings.TrimSpace(strings.ToLower(email)))
	if err != nil {
		return fmt.Errorf("пользователь %s: %w", email, err)
	}
	rawPwd, _, err := auth.RandomToken(9)
	if err != nil {
		return err
	}
	password := rawPwd[:14]
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := a.Repo.UpdateUserPassword(ctx, u.ID, hash); err != nil {
		return err
	}
	subject, body := a.freeAccessEmail(ctx, u.ID, u.Email, password)
	return a.Mail.Send(u.Email, subject, body)
}
