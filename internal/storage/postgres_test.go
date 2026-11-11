package storage_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/storage"
	"github.com/Astarino/ai-buddy/internal/testsupport"
)

func TestConcurrentStartupOnEmptySchema(t *testing.T) {
	db := testsupport.Database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Database gives this test its own disposable schema.
	if _, err := db.Pool.Exec(ctx, "DROP TABLE buddy_states, buddy_sessions, corporate_operations"); err != nil {
		t.Fatal(err)
	}
	const processes = 8
	start := make(chan struct{})
	results := make(chan error, processes)
	for range processes {
		go func() {
			<-start
			instance, err := storage.Open(ctx, db.Pool.Config().ConnString())
			if err == nil {
				_, err = instance.CreateSession(ctx, "startup-user")
				instance.Close()
			}
			results <- err
		}()
	}
	close(start)
	for range processes {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
}

func TestPostgresPersistenceSessionsAndConcurrentUpdates(t *testing.T) {
	db := testsupport.Database(t)
	ctx := context.Background()
	state, err := db.Read(ctx, "user")
	if err != nil || len(state.Messages) != 0 {
		t.Fatal(state, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := db.Update(ctx, "user", func(state *buddy.State) error {
				state.Events = append(state.Events, buddy.Event{Type: "tested"})
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	state, err = db.Read(ctx, "user")
	if err != nil || len(state.Events) != 12 {
		t.Fatal(state, err)
	}
	if _, err = db.Update(ctx, "user", func(*buddy.State) error { return buddy.ErrForbidden }); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	metrics, err := db.Metrics(ctx)
	if err != nil || metrics["tested"] != 12 {
		t.Fatal(metrics, err)
	}
	token, err := db.CreateSession(ctx, "user")
	if err != nil || len(token) != 64 {
		t.Fatal(token, err)
	}
	owner, err := db.Session(ctx, token)
	if err != nil || owner != "user" {
		t.Fatal(owner, err)
	}
	if err = db.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Session(ctx, token); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "INSERT INTO buddy_states(user_id,body) VALUES('bad','[]'::jsonb)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Read(ctx, "bad"); err == nil {
		t.Fatal("corrupt state accepted")
	}
	if _, err = db.Update(ctx, "bad", func(*buddy.State) error { return nil }); err == nil {
		t.Fatal("corrupt update accepted")
	}
	if _, err = db.Metrics(ctx); err == nil {
		t.Fatal("corrupt events accepted")
	}
}

func TestPostgresConfigurationAndUnavailableDatabase(t *testing.T) {
	ctx := context.Background()
	if _, err := storage.Open(ctx, "://bad dsn"); err == nil {
		t.Fatal("accepted invalid dsn")
	}
	if _, err := storage.Open(ctx, "postgres://127.0.0.1:1/missing?connect_timeout=1"); err == nil {
		t.Fatal("unreachable database accepted")
	}
	db := testsupport.Database(t)
	db.Close()
	if _, err := db.Read(ctx, "user"); err == nil {
		t.Fatal("read succeeded")
	}
	if _, err := db.Update(ctx, "user", func(*buddy.State) error { return nil }); err == nil {
		t.Fatal("update succeeded")
	}
	if _, err := db.CreateSession(ctx, "user"); err == nil {
		t.Fatal("session succeeded")
	}
	if _, err := db.Session(ctx, "x"); err == nil {
		t.Fatal("session lookup succeeded")
	}
	if err := db.DeleteSession(ctx, "x"); err == nil {
		t.Fatal("session deletion succeeded")
	}
	if _, err := db.Metrics(ctx); err == nil {
		t.Fatal("metrics succeeded")
	}
}
