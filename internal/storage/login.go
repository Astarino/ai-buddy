package storage

import (
	"context"
	"errors"
	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/jackc/pgx/v5"
	"time"

	"github.com/Astarino/ai-buddy/internal/identity"
)

func (p *Postgres) ClaimPrincipal(ctx context.Context, id, issuer, subject string) error {
	if id == "" || issuer == "" || subject == "" {
		return buddy.ErrForbidden
	}
	return pgx.BeginFunc(ctx, p.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "principal:"+id); err != nil {
			return err
		}
		var existingIssuer, existingSubject string
		err := tx.QueryRow(ctx, "SELECT issuer,subject FROM buddy_principals WHERE profile_id=$1", id).Scan(&existingIssuer, &existingSubject)
		if err == nil {
			if existingIssuer != issuer || existingSubject != subject {
				return buddy.ErrForbidden
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM buddy_states WHERE user_id=$1)", id).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return buddy.ErrForbidden
		}
		_, err = tx.Exec(ctx, "INSERT INTO buddy_principals(profile_id,issuer,subject) VALUES($1,$2,$3)", id, issuer, subject)
		return err
	})
}

func (p *Postgres) SaveLogin(ctx context.Context, state string, attempt identity.Attempt) error {
	_, err := p.Pool.Exec(ctx, `WITH expired AS (DELETE FROM buddy_logins WHERE expires_at < now()) INSERT INTO buddy_logins(state_hash,nonce,verifier,expires_at) VALUES($1,$2,$3,$4)`, hash(state), attempt.Nonce, attempt.Verifier, attempt.ExpiresAt)
	return err
}

func (p *Postgres) ConsumeLogin(ctx context.Context, state string) (identity.Attempt, error) {
	var attempt identity.Attempt
	err := p.Pool.QueryRow(ctx, `DELETE FROM buddy_logins WHERE state_hash=$1 AND expires_at > now() RETURNING nonce,verifier,expires_at`, hash(state)).Scan(&attempt.Nonce, &attempt.Verifier, &attempt.ExpiresAt)
	return attempt, err
}

func (p *Postgres) BindSession(ctx context.Context, token, binding string, expiry time.Time) error {
	_, err := p.Pool.Exec(ctx, `UPDATE buddy_sessions SET expires_at=LEAST(expires_at,$2),context_hash=$3 WHERE token_hash=$1`, hash(token), expiry, binding)
	return err
}

func (p *Postgres) SessionBinding(ctx context.Context, token string) (string, error) {
	var binding string
	err := p.Pool.QueryRow(ctx, `SELECT context_hash FROM buddy_sessions WHERE token_hash=$1 AND expires_at > now()`, hash(token)).Scan(&binding)
	return binding, err
}
