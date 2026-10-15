package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Вход без пароля: код из письма и ссылка из письма о доступе.
// Логика (лимиты, проверка) – в handlers/login_code.go.

// CreateLoginCode сохраняет новый код. Прежние неиспользованные коды
// человека гасим: действует только последний присланный.
func (r *Repo) CreateLoginCode(ctx context.Context, userID uuid.UUID, hash string, ttl time.Duration) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`UPDATE login_codes SET used_at = now() WHERE user_id = $1 AND used_at IS NULL`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO login_codes(user_id, code_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hash, time.Now().Add(ttl)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// LoginCodesSince – сколько кодов человеку отправлено с момента since
// (ограничение частоты: не засыпать почту письмами).
func (r *Repo) LoginCodesSince(ctx context.Context, userID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := r.Pool.QueryRow(ctx,
		`SELECT count(*) FROM login_codes WHERE user_id = $1 AND created_at > $2`, userID, since).Scan(&n)
	return n, err
}

// ErrTooManyAttempts – код исчерпал попытки.
var ErrTooManyAttempts = errors.New("too many attempts")

// UseLoginCode проверяет код человека: действует последний неиспользованный
// и непросроченный, не больше maxAttempts неверных вводов. Верный код гасится.
func (r *Repo) UseLoginCode(ctx context.Context, userID uuid.UUID, hash string, maxAttempts int) error {
	var id uuid.UUID
	var stored string
	var attempts int
	err := r.Pool.QueryRow(ctx,
		`SELECT id, code_hash, attempts FROM login_codes
		  WHERE user_id = $1 AND used_at IS NULL AND expires_at > now()
		  ORDER BY created_at DESC LIMIT 1`, userID).Scan(&id, &stored, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if attempts >= maxAttempts {
		return ErrTooManyAttempts
	}
	if stored != hash {
		_, _ = r.Pool.Exec(ctx, `UPDATE login_codes SET attempts = attempts + 1 WHERE id = $1`, id)
		if attempts+1 >= maxAttempts {
			return ErrTooManyAttempts
		}
		return ErrNotFound
	}
	_, err = r.Pool.Exec(ctx, `UPDATE login_codes SET used_at = now() WHERE id = $1 AND used_at IS NULL`, id)
	return err
}

// CreateLoginLink – ссылка входа для письма о доступе.
func (r *Repo) CreateLoginLink(ctx context.Context, userID uuid.UUID, hash, next string, ttl time.Duration) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO login_links(user_id, token_hash, next_path, expires_at) VALUES ($1, $2, $3, $4)`,
		userID, hash, next, time.Now().Add(ttl))
	return err
}

// UseLoginLink – по действующей ссылке возвращает человека и страницу,
// куда вести. Ссылка многоразовая до истечения срока.
func (r *Repo) UseLoginLink(ctx context.Context, hash string) (uuid.UUID, string, error) {
	var uid uuid.UUID
	var next string
	err := r.Pool.QueryRow(ctx,
		`UPDATE login_links SET last_used_at = now(), uses = uses + 1
		  WHERE token_hash = $1 AND expires_at > now()
		  RETURNING user_id, next_path`, hash).Scan(&uid, &next)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", ErrNotFound
	}
	return uid, next, err
}

// PasswordUnknown – человек не знает пароль от аккаунта: аккаунт заведён
// за него (покупка в один шаг, выдача доступа админом, подписка) и он ни
// разу в него не входил. Такому можно выдать новый пароль в письме, ничего
// у него не сломав.
func (r *Repo) PasswordUnknown(ctx context.Context, userID uuid.UUID) (bool, error) {
	var ok bool
	err := r.Pool.QueryRow(ctx,
		`SELECT u.password_hash LIKE '!unusable!%'
		     OR (u.last_seen_at IS NULL
		         AND NOT EXISTS (SELECT 1 FROM sessions s WHERE s.user_id = u.id))
		   FROM users u WHERE u.id = $1`, userID).Scan(&ok)
	return ok, err
}

// SubscribeOfferAllowed – можно предложить подписаться: согласия нет и
// человек от писем не отписывался (отписавшихся не уговариваем).
func (r *Repo) SubscribeOfferAllowed(ctx context.Context, userID uuid.UUID) (bool, error) {
	var ok bool
	err := r.Pool.QueryRow(ctx,
		`SELECT consent_marketing_at IS NULL AND unsubscribed_at IS NULL FROM users WHERE id = $1`,
		userID).Scan(&ok)
	return ok, err
}
