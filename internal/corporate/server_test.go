package corporate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/testsupport"
)

func TestLostResponseAndIdempotentCorporateContract(t *testing.T) {
	db := testsupport.Database(t)
	sim := NewServer(db, "service", "owner")
	sim.ResponseDelay = 100 * time.Millisecond
	server := httptest.NewServer(sim)
	defer server.Close()
	client := NewClient(server.URL, "service", "owner")
	client.HTTP.Timeout = 30 * time.Millisecond
	ctx := context.Background()
	a := buddy.Action{ID: buddy.NewID(), Kind: "access", Resource: "CATALOG", Level: "Чтение", Reason: "Изучить руководство команды"}
	if err := client.Event(ctx, "anton", "", "lost_response"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Execute(ctx, "anton", a); !errors.Is(err, buddy.ErrUnavailable) {
		t.Fatal(err)
	}
	ops, err := client.List(ctx, "anton")
	if err != nil || len(ops) != 1 {
		t.Fatal(ops, err)
	}
	if err = client.Event(ctx, "anton", "", "normal"); err != nil {
		t.Fatal(err)
	}
	op, err := client.Execute(ctx, "anton", a)
	if err != nil || op.ID != ops[0].ID {
		t.Fatal(op, err)
	}
	a.Reason = "Другое обоснование изменяет действие"
	if _, err = client.Execute(ctx, "anton", a); !errors.Is(err, buddy.ErrConflict) {
		t.Fatal(err)
	}
	a.ID = buddy.NewID()
	if _, err = client.Execute(ctx, "anton", a); !errors.Is(err, buddy.ErrConflict) {
		t.Fatal(err)
	}
	a.Kind = "issue"
	a.Resource = "CAT-101"
	a.Level = "Назначить на себя"
	if _, err = client.Execute(ctx, "anton", a); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	a.Resource = "DELIVERY"
	if _, err = client.Execute(ctx, "anton", a); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err = client.Execute(ctx, "unknown", a); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	a.ID = "x"
	if _, err = client.Execute(ctx, "anton", a); err == nil {
		t.Fatal("short key accepted")
	}
	if err = client.Event(ctx, "anton", op.Key, "pending"); err != nil {
		t.Fatal(err)
	}
	if err = client.Event(ctx, "anton", op.Key, "done"); !errors.Is(err, buddy.ErrConflict) {
		t.Fatal(err)
	}
	if err = client.Event(ctx, "unknown", "", "normal"); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal(err)
	}
	if err = client.Event(ctx, "irina", op.Key, "approved"); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestCorporateHTTPBoundaryAndClientFailures(t *testing.T) {
	db := testsupport.Database(t)
	sim := NewServer(db, "service", "owner")
	server := httptest.NewServer(sim)
	defer server.Close()
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
	}{
		{"GET", "/health", "", "", 200}, {"GET", "/operations?user=anton", "", "", 403}, {"GET", "/missing", "", "service", 404}, {"DELETE", "/operations", "", "service", 405},
		{"GET", "/events", "", "owner", 405}, {"POST", "/events", "invalid", "owner", 400}, {"POST", "/events", `{}`, "wrong", 403},
		{"POST", "/operations", "invalid", "service", 400}, {"POST", "/operations", `{"role":"admin"}`, "service", 400},
	} {
		req, _ := http.NewRequest(tc.method, server.URL+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatal(tc, resp.StatusCode)
		}
	}
	ctx := context.Background()
	client := NewClient(server.URL, "service", "owner")
	if err := client.Event(ctx, "anton", "", "unavailable"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.List(ctx, "anton"); !errors.Is(err, buddy.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := client.Execute(ctx, "anton", buddy.Action{}); !errors.Is(err, buddy.ErrUnavailable) {
		t.Fatal(err)
	}
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("invalid json")) }))
	bad := NewClient(broken.URL, "service", "owner")
	if _, err := bad.List(ctx, "anton"); !errors.Is(err, buddy.ErrUnavailable) {
		t.Fatal(err)
	}
	broken.Close()
	if _, err := bad.List(ctx, "anton"); !errors.Is(err, buddy.ErrUnavailable) {
		t.Fatal(err)
	}
	bad.BaseURL = ":wrong"
	if _, err := bad.List(ctx, "anton"); !errors.Is(err, buddy.ErrUnavailable) {
		t.Fatal(err)
	}
	if err := bad.call(ctx, "POST", "/", "", make(chan int), nil); err == nil {
		t.Fatal("unmarshalable input accepted")
	}
}
