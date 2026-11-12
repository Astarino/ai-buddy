package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/corporate"
	"github.com/Astarino/ai-buddy/internal/model"
	"github.com/Astarino/ai-buddy/internal/testsupport"
)

type browser struct {
	t      *testing.T
	client *http.Client
	base   string
}

func (b browser) call(method, path, body string, headers ...map[string]string) (int, []byte) {
	b.t.Helper()
	req, err := http.NewRequest(method, b.base+path, strings.NewReader(body))
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", b.base)
	for _, group := range headers {
		for key, value := range group {
			req.Header.Set(key, value)
		}
	}
	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		b.t.Fatal(err)
	}
	return resp.StatusCode, raw
}
func (b browser) expect(method, path, body string, status int, headers ...map[string]string) []byte {
	b.t.Helper()
	got, raw := b.call(method, path, body, headers...)
	if got != status {
		b.t.Fatalf("%s %s: got %d expected %d: %s", method, path, got, status, raw)
	}
	return raw
}
func readJSON[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err, string(raw))
	}
	return result
}

func apiWorld(t *testing.T) (browser, *API) {
	t.Helper()
	db := testsupport.Database(t)
	sim := httptest.NewServer(corporate.NewServer(db, "service-token", "owner-token"))
	t.Cleanup(sim.Close)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>AI Buddy</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("window.ready=true"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &API{Service: buddy.NewService(db, corporate.NewClient(sim.URL, "service-token", "owner-token"), model.Fixture{}), Sessions: db, DemoToken: "owner-token", StaticDir: dir}
	server := httptest.NewServer(a.Handler())
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	return browser{t, &http.Client{Jar: jar}, server.URL}, a
}

func TestHTTPJourneyAndPersistedState(t *testing.T) {
	b, a := apiWorld(t)
	b.expect("GET", "/api/health", "", 200)
	b.expect("GET", "/api/bootstrap", "", 200)
	b.expect("GET", "/api/state", "", 401)
	b.expect("POST", "/api/session", `{"profile_id":"anton"}`, 200)
	view := readJSON[buddy.View](t, b.expect("GET", "/api/state", "", 200))
	if view.Profile.ID != "anton" || view.Model != "fixture" {
		t.Fatal(view)
	}
	b.expect("GET", "/api/state", "", 409, map[string]string{"X-Buddy-Context": "irina"})
	b.expect("GET", "/api/documents/catalog-guide", "", 404)
	b.expect("GET", "/api/documents/delivery-welcome", "", 404)
	b.expect("GET", "/api/documents/catalog-mentor-private", "", 404)
	b.expect("GET", "/api/documents/checklist", "", 200)
	b.expect("POST", "/api/messages", `{"id":"question-001","text":"Не открывается вики"}`, 200)
	view = readJSON[buddy.View](t, b.expect("POST", "/api/refresh", `{}`, 200))
	if len(view.Actions) != 1 {
		t.Fatal(view)
	}
	id := view.Actions[0].ID
	repeated, _ := json.Marshal(map[string]string{"kind": "access", "reason": view.Actions[0].Reason})
	same := readJSON[buddy.Action](t, b.expect("POST", "/api/actions", string(repeated), 200))
	if same.ID != id {
		t.Fatal("identical parameters changed confirmation", same)
	}
	op := readJSON[buddy.Operation](t, b.expect("POST", "/api/actions/"+id+"/confirm", `{}`, 200))
	b.expect("POST", "/api/actions/"+id+"/confirm", `{}`, 200)
	b.expect("POST", "/api/demo/events", `{"key":"`+id+`","status":"approved"}`, 403)
	for _, status := range []string{"approved", "granted"} {
		b.expect("POST", "/api/demo/events", `{"key":"`+id+`","status":"`+status+`"}`, 200, map[string]string{"X-Demo-Token": "owner-token"})
	}
	b.expect("GET", "/api/documents/catalog-guide", "", 200)
	b.expect("POST", "/api/tasks/read-guide/complete", `{}`, 200)
	b.expect("POST", "/api/tasks/space-access/complete", `{}`, 403)
	b.expect("POST", "/api/draft", `{"question":"Хочу уточнить критерии задачи"}`, 200)
	b.expect("GET", "/api/diagnostics", "", 200)
	b.expect("GET", "/api/demo/metrics", "", 403)
	counts := readJSON[map[string]int](t, b.expect("GET", "/api/demo/metrics", "", 200, map[string]string{"X-Demo-Token": "owner-token"}))
	if counts["action_completed"] != 1 {
		t.Fatal(counts)
	}
	state, err := a.Service.Repo.Read(context.Background(), "anton")
	if err != nil || state.Actions[0].ResultID != op.ID {
		t.Fatal(state, err)
	}
	jar, _ := cookiejar.New(nil)
	other := browser{t, &http.Client{Jar: jar}, b.base}
	other.expect("POST", "/api/session", `{"profile_id":"irina"}`, 200)
	other.expect("POST", "/api/actions/"+id+"/confirm", `{}`, 404)
	otherView := readJSON[buddy.View](t, other.expect("GET", "/api/state", "", 200))
	if len(otherView.Messages) != 0 || len(otherView.Operations) != 0 {
		t.Fatal(otherView)
	}
	b.expect("DELETE", "/api/session", `{}`, 200)
	b.expect("GET", "/api/state", "", 401)
	b.expect("DELETE", "/api/session", `{}`, 200)
}

func TestHTTPValidationAndStaticContent(t *testing.T) {
	b, a := apiWorld(t)
	for _, body := range []string{`invalid`, `{"profile_id":"unknown"}`, `{"profile_id":"anton","role":"mentor"}`, `{"profile_id":"anton"} {}`} {
		b.expect("POST", "/api/session", body, 400)
	}
	b.expect("POST", "/api/session", `{"profile_id":"anton"}`, 403, map[string]string{"Origin": "https://untrusted.example"})
	b.expect("POST", "/api/session", `{"profile_id":"anton"}`, 400, map[string]string{"Content-Type": "text/plain"})
	b.expect("POST", "/api/session", `{"profile_id":"anton"}`, 200)
	for _, path := range []string{"/api/messages", "/api/actions", "/api/draft", "/api/demo/events", "/api/actions/x/confirm", "/api/tasks/x/complete"} {
		b.expect("POST", path, `invalid`, 400, map[string]string{"X-Demo-Token": "owner-token"})
	}
	b.expect("POST", "/api/messages", `{"id":"bad","text":""}`, 400)
	b.expect("POST", "/api/actions", `{"kind":"issue","reason":"Хочу взять первую задачу"}`, 403)
	b.expect("POST", "/api/tasks/unknown/complete", `{}`, 404)
	b.expect("POST", "/api/draft", `{"question":"`+strings.Repeat("x", 2100)+`"}`, 400)
	b.expect("POST", "/api/demo/events", `{"key":"missing","status":"granted"}`, 404, map[string]string{"X-Demo-Token": "owner-token"})
	b.expect("GET", "/api/unknown", "", 404)
	if raw := b.expect("GET", "/", "", 200); !strings.Contains(string(raw), "AI Buddy") {
		t.Fatal(string(raw))
	}
	b.expect("GET", "/app.js", "", 200)
	b.expect("GET", "/route", "", 200)
	b.expect("GET", "/missing.css", "", 404)
	b.expect("POST", "/nothing", `{}`, 405)
	b.expect("HEAD", "/", "", 200)
	a.StaticDir = ""
	b.expect("GET", "/", "", 404)
	for _, err := range []error{buddy.ErrNotFound, buddy.ErrForbidden, buddy.ErrConflict, buddy.ErrInvalid, buddy.ErrExpired, buddy.ErrBusy, buddy.ErrUnavailable, context.Canceled, io.EOF} {
		w := httptest.NewRecorder()
		failure(w, err)
		if w.Code < 400 || !strings.Contains(w.Body.String(), `"message"`) {
			t.Fatal(w)
		}
	}
	server := HTTPServer("127.0.0.1:0", a.Handler())
	if server.ReadHeaderTimeout == 0 || server.WriteTimeout == 0 {
		t.Fatal(server)
	}
}
