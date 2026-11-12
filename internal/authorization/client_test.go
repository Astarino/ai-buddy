package authorization

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
)

func TestResourceDecisionsAreBoundToActorActionAndResource(t *testing.T) {
	principal := buddy.Profile{ID: "employee", Issuer: "https://idp.example", Subject: "subject-17"}
	resource := buddy.Resource{System: "confluence", Kind: "document", ID: "page-42"}
	for _, mode := range []string{"allow", "deny", "actor", "resource", "permission", "expired", "version", "malformed", "extra", "forbidden", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request Request
				if r.URL.Path != "/authorize" || r.Header.Get("Authorization") != "Bearer service-secret" || json.NewDecoder(r.Body).Decode(&request) != nil || request.ActorID != principal.ID || request.Subject != principal.Subject || request.Issuer != principal.Issuer || request.Resource != resource {
					t.Error("invalid authorization request")
				}
				if mode == "forbidden" {
					w.WriteHeader(403)
					return
				}
				if mode == "unavailable" {
					w.WriteHeader(503)
					return
				}
				if mode == "malformed" {
					w.Write([]byte("not-json"))
					return
				}
				if mode == "extra" {
					w.Write([]byte(`{"unexpected":true}`))
					return
				}
				decision := Decision{ActorID: principal.ID, Resource: resource, Permission: "read", Allowed: true, ExpiresAt: time.Now().Add(time.Minute), PolicyVersion: "test-policy-v1"}
				switch mode {
				case "deny":
					decision.Allowed = false
				case "actor":
					decision.ActorID = "someone-else"
				case "resource":
					decision.Resource.ID = "another-page"
				case "permission":
					decision.Permission = "assign_self"
				case "expired":
					decision.ExpiresAt = time.Now().Add(-time.Minute)
				case "version":
					decision.PolicyVersion = ""
				}
				json.NewEncoder(w).Encode(decision)
			}))
			defer server.Close()
			client, err := New(server.URL, "service-secret")
			if err != nil {
				t.Fatal(err)
			}
			err = client.Check(context.Background(), principal, resource, "read")
			if mode == "allow" && err != nil {
				t.Fatal(err)
			}
			if mode != "allow" && err == nil {
				t.Fatal("mismatched or failed decision accepted", mode)
			}
			if mode == "deny" && !errors.Is(err, buddy.ErrForbidden) {
				t.Fatal(err)
			}
			server.Close()
			if err := client.Check(context.Background(), principal, resource, "read"); err == nil {
				t.Fatal("closed authority allowed access")
			}
		})
	}
	if _, err := New("http://outside.example", "secret"); err == nil {
		t.Fatal("unsafe authority URL accepted")
	}
	if _, err := New("https://authority.example", ""); err == nil {
		t.Fatal("empty token accepted")
	}
	client, _ := New("http://localhost", "test")
	for _, p := range []buddy.Profile{{}, principal} {
		if err := client.Check(context.Background(), p, buddy.Resource{}, "read"); !errors.Is(err, buddy.ErrForbidden) {
			t.Fatal(err)
		}
	}
	if err := client.Check(context.Background(), principal, resource, "execute_shell"); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	client.BaseURL = "http://outside.example"
	if err := client.Check(context.Background(), principal, resource, "read"); !errors.Is(err, buddy.ErrUnavailable) {
		t.Fatal(err)
	}
}
