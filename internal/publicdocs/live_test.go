package publicdocs

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveOfficialDocumentation(t *testing.T) {
	if os.Getenv("RUN_PUBLIC_SEARCH_EVAL") != "true" {
		t.Skip("opt-in network evaluation")
	}
	started := time.Now()
	docs, err := New().Search(context.Background(), "go", "go test ./... pattern directory prefix does not contain main module go.mod")
	if err != nil || len(docs) == 0 {
		t.Fatal(docs, err)
	}
	matched := false
	for _, doc := range docs {
		if strings.Contains(doc.Body, "go.mod") {
			matched = true
		}
	}
	if !matched {
		t.Fatal("module root evidence not retrieved", docs)
	}
	report := map[string]any{"evaluated_at": time.Now().UTC(), "elapsed_ms": time.Since(started).Milliseconds(), "network": true, "scope": "live official Go/Git documentation snapshots; local passage ranking; question never sent to websites", "documents": docs, "passed": matched}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile("../../reports/public-search.json", append(raw, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}
