package storage

import (
	"context"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/jackc/pgx/v5"
)

func auditEvent(ctx context.Context, owner string, event buddy.Event) buddy.Event {
	actor, correlation := buddy.AuditIdentity(ctx)
	if event.ID == "" {
		event.ID = buddy.NewID()
	}
	if event.ActorID == "" {
		event.ActorID = actor
	}
	if event.ActorID == "" {
		event.ActorID = owner
	}
	if event.OwnerID == "" {
		event.OwnerID = owner
	}
	if event.CorrelationID == "" {
		event.CorrelationID = correlation
	}
	if event.CorrelationID == "" {
		event.CorrelationID = buddy.NewID()
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	if event.Decision == "" {
		event.Decision = "allow"
	}
	if event.Outcome == "" {
		event.Outcome = "success"
	}
	if event.PolicyVersion == "" {
		event.PolicyVersion = "buddy-security-v3"
	}
	return event
}

func appendAudit(ctx context.Context, tx pgx.Tx, e buddy.Event) error {
	_, err := tx.Exec(ctx, `INSERT INTO buddy_audit(id,actor_id,owner_id,event_type,object_id,decision,outcome,correlation_id,policy_version,at,duration_ms) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, e.ID, e.ActorID, e.OwnerID, e.Type, e.ObjectID, e.Decision, e.Outcome, e.CorrelationID, e.PolicyVersion, e.At, e.DurationMS)
	return err
}

func (p *Postgres) AppendAudit(ctx context.Context, event buddy.Event) error {
	return pgx.BeginFunc(ctx, p.Pool, func(tx pgx.Tx) error {
		return appendAudit(ctx, tx, auditEvent(ctx, event.OwnerID, event))
	})
}

func (p *Postgres) AuditOperation(ctx context.Context, tx pgx.Tx, owner, object, kind, outcome string) error {
	return appendAudit(ctx, tx, auditEvent(ctx, owner, buddy.Event{Type: kind, ObjectID: object, Outcome: outcome}))
}

func (p *Postgres) AuditEvents(ctx context.Context, owner string, limit int) ([]buddy.Event, error) {
	if limit < 1 || limit > 200 {
		limit = 200
	}
	rows, err := p.Pool.Query(ctx, `SELECT id,actor_id,owner_id,event_type,object_id,decision,outcome,correlation_id,policy_version,at,duration_ms FROM buddy_audit WHERE owner_id=$1 ORDER BY at DESC,id LIMIT $2`, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []buddy.Event{}
	for rows.Next() {
		var e buddy.Event
		if err := rows.Scan(&e.ID, &e.ActorID, &e.OwnerID, &e.Type, &e.ObjectID, &e.Decision, &e.Outcome, &e.CorrelationID, &e.PolicyVersion, &e.At, &e.DurationMS); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
