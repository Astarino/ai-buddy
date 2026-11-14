package storage_test

import (
	"context"
	"testing"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/testsupport"
)

func TestRequestFailuresDoNotCommitWithoutAudit(t *testing.T) {
	ctx := context.Background()
	db := testsupport.Database(t)
	if _, err := db.UpdateRequest(ctx, "missing", func(*buddy.HelpRequest) error { return nil }, buddy.Event{}); err == nil {
		t.Fatal("empty request stored")
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO buddy_requests(id,requester_id,assignee_id,body) VALUES('bad','intern','mentor','[]'::jsonb)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Request(ctx, "bad"); err == nil {
		t.Fatal("invalid card accepted")
	}
	if _, err := db.RequestsFor(ctx, "intern"); err == nil {
		t.Fatal("invalid list accepted")
	}
	if _, err := db.UpdateRequest(ctx, "bad", func(*buddy.HelpRequest) error { return nil }, buddy.Event{}); err == nil {
		t.Fatal("invalid card changed")
	}
	if _, err := db.Pool.Exec(ctx, "DROP TABLE buddy_audit"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateRequest(ctx, "new", func(r *buddy.HelpRequest) error {
		*r = buddy.HelpRequest{ID: "new", RequesterID: "intern", AssigneeID: "mentor", Status: "draft", Version: 1}
		return nil
	}, buddy.Event{}); err == nil {
		t.Fatal("unaudited mutation committed")
	}
	if _, err := db.Request(ctx, "new"); err == nil {
		t.Fatal("request survived failed audit")
	}
	db.Close()
	if _, err := db.RequestsFor(ctx, "intern"); err == nil {
		t.Fatal("closed database list")
	}
	if _, err := db.Request(ctx, "new"); err == nil {
		t.Fatal("closed database card")
	}
	if _, err := db.UpdateRequest(ctx, "new", func(*buddy.HelpRequest) error { return nil }, buddy.Event{}); err == nil {
		t.Fatal("closed database update")
	}
}
