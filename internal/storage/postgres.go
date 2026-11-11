package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

type Postgres struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, dsn string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 8
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		// Both processes may start against an empty database at the same time.
		// IF NOT EXISTS alone does not serialize PostgreSQL's catalog writes.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(684173205)"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, schema)
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Postgres{db}, nil
}

func (p *Postgres) Close() { p.Pool.Close() }

func (p *Postgres) Read(ctx context.Context, owner string) (buddy.State, error) {
	state := buddy.EmptyState()
	var raw []byte
	err := p.Pool.QueryRow(ctx, "SELECT body FROM buddy_states WHERE user_id=$1", owner).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(raw, &state)
	return state, err
}

func (p *Postgres) Update(ctx context.Context, owner string, change func(*buddy.State) error) (buddy.State, error) {
	state := buddy.EmptyState()
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return state, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	raw, err := json.Marshal(state)
	if err != nil {
		return state, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO buddy_states(user_id,body) VALUES($1,$2) ON CONFLICT DO NOTHING", owner, raw); err != nil {
		return state, err
	}
	if err = tx.QueryRow(ctx, "SELECT body FROM buddy_states WHERE user_id=$1 FOR UPDATE", owner).Scan(&raw); err != nil {
		return state, err
	}
	if err = json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	if err = change(&state); err != nil {
		return state, err
	}
	raw, err = json.Marshal(state)
	if err != nil {
		return state, err
	}
	if _, err = tx.Exec(ctx, "UPDATE buddy_states SET body=$2,updated_at=now() WHERE user_id=$1", owner, raw); err != nil {
		return state, err
	}
	return state, tx.Commit(ctx)
}

func hash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func (p *Postgres) CreateSession(ctx context.Context, owner string) (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	value := hex.EncodeToString(token)
	_, err := p.Pool.Exec(ctx, "INSERT INTO buddy_sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)", hash(value), owner, time.Now().Add(7*24*time.Hour))
	return value, err
}

func (p *Postgres) Session(ctx context.Context, token string) (string, error) {
	var owner string
	err := p.Pool.QueryRow(ctx, "SELECT user_id FROM buddy_sessions WHERE token_hash=$1 AND expires_at > now()", hash(token)).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", buddy.ErrForbidden
	}
	return owner, err
}

func (p *Postgres) DeleteSession(ctx context.Context, token string) error {
	_, err := p.Pool.Exec(ctx, "DELETE FROM buddy_sessions WHERE token_hash=$1", hash(token))
	return err
}

func (p *Postgres) Metrics(ctx context.Context) (map[string]int, error) {
	rows, err := p.Pool.Query(ctx, "SELECT body->'events' FROM buddy_states")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var events []buddy.Event
		if err = json.Unmarshal(raw, &events); err != nil {
			return nil, err
		}
		for _, event := range events {
			counts[event.Type]++
		}
	}
	return counts, rows.Err()
}
