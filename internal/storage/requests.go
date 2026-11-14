package storage

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/jackc/pgx/v5"
)

func (p *Postgres) RequestsFor(ctx context.Context, actor string) ([]buddy.HelpRequest, error) {
	rows, err := p.Pool.Query(ctx, `SELECT body FROM buddy_requests WHERE requester_id=$1 OR assignee_id=$1 ORDER BY updated_at DESC,id`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []buddy.HelpRequest{}
	for rows.Next() {
		var r buddy.HelpRequest
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (p *Postgres) Request(ctx context.Context, id string) (buddy.HelpRequest, error) {
	var raw []byte
	var r buddy.HelpRequest
	err := p.Pool.QueryRow(ctx, "SELECT body FROM buddy_requests WHERE id=$1", id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, buddy.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(raw, &r)
	return r, err
}

func (p *Postgres) UpdateRequest(ctx context.Context, id string, change func(*buddy.HelpRequest) error, event buddy.Event) (buddy.HelpRequest, error) {
	var r buddy.HelpRequest
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,17))", id); err != nil {
		return r, err
	}
	var raw []byte
	err = tx.QueryRow(ctx, "SELECT body FROM buddy_requests WHERE id=$1 FOR UPDATE", id).Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return r, err
	}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &r); err != nil {
			return r, err
		}
	}
	previous, _ := json.Marshal(r)
	if err = change(&r); err != nil {
		return buddy.HelpRequest{}, err
	}
	if r.ID == "" {
		return r, buddy.ErrNotFound
	}
	updated, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	if string(updated) == string(previous) {
		return r, tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `INSERT INTO buddy_requests(id,requester_id,assignee_id,body,updated_at) VALUES($1,$2,$3,$4,now()) ON CONFLICT(id) DO UPDATE SET assignee_id=$3,body=$4,updated_at=now()`, r.ID, r.RequesterID, r.AssigneeID, updated)
	if err != nil {
		return r, err
	}
	event.OwnerID = r.RequesterID
	event = auditEvent(ctx, event.OwnerID, event)
	if err = appendAudit(ctx, tx, event); err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}
