package httpapi

import (
	"context"
	"fmt"
	"testing"

	"github.com/Astarino/ai-buddy/internal/buddy"
)

type searchStub struct{}

func (searchStub) Search(context.Context, string, string) ([]buddy.Document, error) {
	return []buddy.Document{{Title: "Go modules", Body: "A module root contains go.mod.", URL: "https://go.dev/ref/mod", Scope: "public", DataClass: "public", Version: "v1"}}, nil
}

func TestSharedRequestsHTTPFlow(t *testing.T) {
	b, a := apiWorld(t)
	b.expect("GET", "/api/requests", "", 401)
	b.expect("POST", "/api/session", `{"profile_id":"anton"}`, 200)
	b.expect("POST", "/api/requests", `{"id":"api-request-1","kind":"mentor","subject":"Тесты не идут","body":"Проверил команду и папку","assignee_id":"irina"}`, 400)
	r := readJSON[buddy.HelpRequest](t, b.expect("POST", "/api/requests", `{"id":"api-request-1","kind":"mentor","subject":"Тесты не идут","body":"Проверил команду и папку"}`, 200))
	b.expect("POST", "/api/requests/"+r.ID+"/change", `{"command":"send","version":1}`, 200)
	b.expect("POST", "/api/session", `{"profile_id":"irina"}`, 200)
	b.expect("GET", "/api/requests/"+r.ID, "", 404)
	b.expect("POST", "/api/requests/"+r.ID+"/replies", `{"id":"foreign-reply","text":"Чужой ответ","version":2}`, 404)
	b.expect("POST", "/api/session", `{"profile_id":"dasha"}`, 200)
	rows := readJSON[[]buddy.HelpRequest](t, b.expect("GET", "/api/requests", "", 200))
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	r = readJSON[buddy.HelpRequest](t, b.expect("POST", "/api/requests/"+r.ID+"/replies", `{"id":"valid-reply","text":"Открой папку проекта","version":2}`, 200))
	if r.Status != "answered" {
		t.Fatal(r)
	}
	b.expect("POST", "/api/requests/"+r.ID+"/decision", fmt.Sprintf(`{"status":"granted","version":%d}`, r.Version), 403)
	b.expect("POST", "/api/session", `{"profile_id":"anton"}`, 200)
	b.expect("POST", "/api/requests/"+r.ID+"/change", fmt.Sprintf(`{"command":"resolve","version":%d}`, r.Version), 200)
	a.Service.Searcher = searchStub{}
	run := readJSON[buddy.PublicSearch](t, b.expect("POST", "/api/search", `{"id":"api-search-one","topic":"go","query":"go.mod module"}`, 200))
	b.expect("GET", "/api/documents/"+run.Documents[0].ID, "", 200)
	b.expect("POST", "/api/session", `{"profile_id":"irina"}`, 200)
	b.expect("GET", "/api/documents/"+run.Documents[0].ID, "", 404)
	for _, path := range []string{"/api/search", "/api/requests", "/api/requests/unknown/change", "/api/requests/unknown/replies", "/api/requests/unknown/decision"} {
		b.expect("POST", path, `{"unknown":true}`, 400)
	}
	b.expect("GET", "/api/requests/unknown", "", 404)
	b.expect("POST", "/api/search", `{"id":"invalid-topic","topic":"private","query":"go.mod"}`, 400)
}
