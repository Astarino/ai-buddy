package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/identity"
	"github.com/Astarino/ai-buddy/internal/storage"
	"github.com/Astarino/ai-buddy/internal/testsupport"
)

func TestLoginAttemptsAreOneTimeAndSessionsAreBound(t *testing.T) {
	db := testsupport.Database(t)
	ctx := context.Background()
	state := "opaque-login-state"
	attempt := identity.Attempt{Nonce: "synthetic-nonce", Verifier: "synthetic-verifier", ExpiresAt: time.Now().Add(time.Minute)}
	if err := db.SaveLogin(ctx, state, attempt); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.Pool.QueryRow(ctx, "SELECT state_hash FROM buddy_logins").Scan(&stored); err != nil || stored == state {
		t.Fatal("login state not hashed", err)
	}
	loaded, err := db.ConsumeLogin(ctx, state)
	if err != nil || loaded.Nonce != attempt.Nonce || loaded.Verifier != attempt.Verifier {
		t.Fatal(loaded, err)
	}
	if _, err := db.ConsumeLogin(ctx, state); err == nil {
		t.Fatal("login replay accepted")
	}
	attempt.ExpiresAt = time.Now().Add(-time.Minute)
	if err := db.SaveLogin(ctx, state, attempt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConsumeLogin(ctx, state); err == nil {
		t.Fatal("expired login consumed")
	}
	if err := db.SaveLogin(ctx, "another-state", identity.Attempt{ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM buddy_logins WHERE expires_at < now()").Scan(&count); err != nil || count != 0 {
		t.Fatal("expired PKCE state retained", count, err)
	}
	token, err := db.CreateSession(ctx, "employee")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BindSession(ctx, token, "profile-binding", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	binding, err := db.SessionBinding(ctx, token)
	if err != nil || binding != "profile-binding" {
		t.Fatal(binding, err)
	}
	if err := db.BindSession(ctx, token, "profile-binding", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SessionBinding(ctx, token); err == nil {
		t.Fatal("expired bound session accepted")
	}
	if err := db.ClaimPrincipal(ctx, "employee", "https://idp.example", "subject-17"); err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimPrincipal(ctx, "employee", "https://idp.example", "subject-17"); err != nil {
		t.Fatal("stable identity could not sign in again", err)
	}
	if err := db.ClaimPrincipal(ctx, "employee", "https://idp.example", "different-person"); err == nil {
		t.Fatal("profile ID reassigned to another identity")
	}
	if _, err := db.Update(ctx, "legacy-owner", func(*buddy.State) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimPrincipal(ctx, "legacy-owner", "https://idp.example", "subject-legacy"); err == nil {
		t.Fatal("legacy history assigned without identity proof")
	}
	if err := db.ClaimPrincipal(ctx, "", "", ""); err == nil {
		t.Fatal("empty identity accepted")
	}
	existing, err := storage.OpenExisting(ctx, db.Pool.Config().ConnString())
	if err != nil {
		t.Fatal("prepared schema not usable without DDL", err)
	}
	existing.Close()
	if _, err := db.Pool.Exec(ctx, "DROP TABLE buddy_logins"); err != nil {
		t.Fatal(err)
	}
	if instance, err := storage.OpenExisting(ctx, db.Pool.Config().ConnString()); err == nil {
		instance.Close()
		t.Fatal("runtime recreated a missing schema")
	}
}

func TestAuditIsSeparateAtomicAndAttributed(t *testing.T) {
	db := testsupport.Database(t)
	ctx := buddy.WithAudit(context.Background(), "initiator-17", "correlation-42")
	_, err := db.Update(ctx, "intern-3", func(state *buddy.State) error {
		state.Events = append(state.Events, buddy.Event{Type: "action_completed", ObjectID: "request-123", Outcome: "granted"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := db.AuditEvents(ctx, "intern-3", 500)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	e := events[0]
	if e.ActorID != "initiator-17" || e.OwnerID != "intern-3" || e.CorrelationID != "correlation-42" || e.Outcome != "granted" || e.Decision != "allow" || e.PolicyVersion == "" || e.At.IsZero() {
		t.Fatal(e)
	}
	if other, err := db.AuditEvents(ctx, "another-user", 10); err != nil || len(other) != 0 {
		t.Fatal("foreign audit exposed", other, err)
	}
	if _, err := db.Pool.Exec(ctx, "DELETE FROM buddy_states WHERE user_id='intern-3'"); err != nil {
		t.Fatal(err)
	}
	if events, err := db.AuditEvents(ctx, "intern-3", -1); err != nil || len(events) != 1 {
		t.Fatal("audit disappeared with mutable state", events, err)
	}
	if err := db.AppendAudit(ctx, buddy.Event{OwnerID: "intern-3", Type: "denied", Decision: "deny", Outcome: "403", ObjectID: "resource-17"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "ALTER TABLE buddy_audit ADD CONSTRAINT reject_new_audit CHECK (false) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	_, err = db.Update(ctx, "intern-3", func(state *buddy.State) error {
		state.Messages = append(state.Messages, buddy.Message{ID: "should-roll-back", Text: "test"})
		state.Events = append(state.Events, buddy.Event{Type: "action_completed"})
		return nil
	})
	if err == nil {
		t.Fatal("action committed without its audit record")
	}
	stateAfter, err := db.Read(ctx, "intern-3")
	if err != nil || len(stateAfter.Messages) != 0 {
		t.Fatal("audit failure did not roll back state", stateAfter, err)
	}
}
