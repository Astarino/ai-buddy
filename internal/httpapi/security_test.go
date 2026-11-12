package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/identity"
)

type testLogin struct {
	subject    string
	beginError bool
}

type testAuthority struct{}

func (testAuthority) Check(_ context.Context, p buddy.Profile, r buddy.Resource, permission string) error {
	if r.System == "ai-buddy" && r.Kind == "account" && r.ID == p.ID && permission == "read" {
		return nil
	}
	return buddy.ErrForbidden
}

type revokedAccount struct{}

func (revokedAccount) Check(context.Context, buddy.Profile, buddy.Resource, string) error {
	return buddy.ErrForbidden
}

func (l testLogin) Begin(context.Context) (string, string, error) {
	if l.beginError {
		return "", "", buddy.ErrUnavailable
	}
	return "https://idp.example/authorize", strings.Repeat("a", 64), nil
}
func (l testLogin) Complete(_ context.Context, state, cookie, code string) (identity.Identity, error) {
	if state != cookie || code != "verified-by-oidc" {
		return identity.Identity{}, buddy.ErrForbidden
	}
	return identity.Identity{Issuer: "https://idp.example", Subject: l.subject, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func TestCorporateHTTPLoginAndSessionContext(t *testing.T) {
	_, a := apiWorld(t)
	a.Production = true
	a.SecureCookie = true
	a.Service.Demo, a.Service.Operator = false, nil
	a.Service.Authorizer = testAuthority{}
	a.Login = testLogin{subject: "employee-subject"}
	a.Service.Catalog.Profiles[0].Issuer = "https://idp.example"
	a.Service.Catalog.Profiles[0].Subject = "employee-subject"
	server := httptest.NewTLSServer(a.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	b := browser{t, client, server.URL}
	boot := readJSON[map[string]json.RawMessage](t, b.expect("GET", "/api/bootstrap", "", 200))
	if string(boot["profiles"]) != "[]" || string(boot["demo"]) != "false" {
		t.Fatal("demo identity leaked in corporate mode", boot)
	}
	b.expect("POST", "/api/session", `{"profile_id":"anton"}`, 403)
	b.expect("POST", "/api/demo/events", `{}`, 404)
	b.expect("GET", "/api/demo/metrics", "", 404)
	b.expect("GET", "/api/login/callback?state=bad&code=verified-by-oidc", "", 403)
	b.expect("GET", "/api/login", "", 303)
	b.expect("GET", "/api/login/callback?state="+strings.Repeat("a", 64)+"&code=incorrect", "", 403)
	b.expect("GET", "/api/login", "", 303)
	b.expect("GET", "/api/login/callback?state="+strings.Repeat("a", 64)+"&code=verified-by-oidc", "", 303)
	b.expect("GET", "/api/state", "", 200)
	a.Service.Authorizer = revokedAccount{}
	b.expect("GET", "/api/state", "", 403)
	a.Service.Authorizer = testAuthority{}
	// A reassigned team/subject must invalidate an existing cookie rather than reuse its authority.
	a.Service.Catalog.Profiles[0].Team = "changed-team"
	b.expect("GET", "/api/state", "", 401)
	a.Service.Catalog.Profiles[0].Team = "catalog"
	a.Login = testLogin{subject: "not-in-directory"}
	b.expect("GET", "/api/login", "", 303)
	b.expect("GET", "/api/login/callback?state="+strings.Repeat("a", 64)+"&code=verified-by-oidc", "", 403)
	a.Login = testLogin{beginError: true}
	b.expect("GET", "/api/login", "", 503)
}

func TestHTTPRevokedReplayAndAuditDoNotExposeSecrets(t *testing.T) {
	b, a := apiWorld(t)
	b.expect("POST", "/api/session", `{"profile_id":"anton"}`, 200)
	prepared := readJSON[buddy.Action](t, b.expect("POST", "/api/actions", `{"kind":"access","reason":"Изучить руководство команды"}`, 200))
	b.expect("POST", "/api/actions/"+prepared.ID+"/confirm", `{}`, 200)
	for _, status := range []string{"approved", "granted"} {
		b.expect("POST", "/api/demo/events", `{"key":"`+prepared.ID+`","status":"`+status+`"}`, 200, map[string]string{"X-Demo-Token": "owner-token"})
	}
	const request = `{"id":"http-replay-question","text":"Как подготовиться к первому ревью?"}`
	b.expect("POST", "/api/messages", request, 200)
	b.expect("POST", "/api/demo/events", `{"key":"`+prepared.ID+`","status":"revoked"}`, 200, map[string]string{"X-Demo-Token": "owner-token"})
	replayed := readJSON[buddy.Message](t, b.expect("POST", "/api/messages", request, 200))
	if replayed.Kind != "restricted" || len(replayed.SourceIDs) != 0 || len(replayed.Dependencies) != 0 {
		t.Fatal("HTTP replay bypassed revocation", replayed)
	}
	b.expect("GET", "/api/documents/delivery-guide", "", 404)
	b.expect("POST", "/api/messages", `{"id":"http-secret-message","text":"Мой пароль SYNTHETIC_HTTP_SECRET"}`, 200)
	raw := b.expect("GET", "/api/diagnostics", "", 200)
	if strings.Contains(string(raw), "SYNTHETIC_HTTP_SECRET") || strings.Contains(string(raw), "Мой пароль") || strings.Contains(string(raw), "owner-token") {
		t.Fatal("audit contains secret or prompt text")
	}
	audit := a.Service.Repo.(buddy.AuditStore)
	events, err := audit.AuditEvents(context.Background(), "anton", 200)
	if err != nil {
		t.Fatal(err)
	}
	denied, operator := false, false
	for _, e := range events {
		if e.Type == "http_error" && e.ActorID == "anton" && e.Decision == "deny" {
			denied = true
		}
		if e.Type == "resource_transition" && e.ActorID == "demo-operator" && e.Outcome == "revoked" {
			operator = true
		}
	}
	if !denied || !operator {
		t.Fatal("denial/operator attribution missing", denied, operator)
	}
}
