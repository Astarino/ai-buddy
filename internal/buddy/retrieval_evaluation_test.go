package buddy_test

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/model"
)

func TestLiveRetrievalComparison(t *testing.T) {
	if os.Getenv("RUN_RETRIEVAL_LIVE") == "" {
		t.Skip("explicit local embedding evaluation")
	}
	var cases []struct {
		Query    string   `json:"query"`
		Expected []string `json:"expected"`
		Access   bool     `json:"access"`
		Category string   `json:"category"`
	}
	raw, err := os.ReadFile("../../evals/retrieval.json")
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &cases) != nil {
		t.Fatal("invalid evaluation")
	}
	catalog := buddy.LoadCatalog()
	p, _ := catalog.Profile("anton")
	embedding, err := model.NewEmbedder("http://127.0.0.1:11434", "embeddinggemma:300m", "85462619ee721b466c5927d109d4cb765861907d5417b9109caebc4e614679f1", model.Fixture{})
	if err != nil {
		t.Fatal(err)
	}
	type item struct {
		Query           string `json:"query"`
		Expected, Found []string
		Mode            string
		DurationMS      float64
		Recall5, MRR10  float64
		Category        string
		Fallback        bool
	}
	rows := []item{}
	metrics := map[string]any{}
	violations := 0
	for _, mode := range []string{"baseline", "bm25", "embeddings", "hybrid"} {
		r := &buddy.Retriever{Embedding: embedding}
		recall, mrr, total, ms, fallback := 0.0, 0.0, 0, []float64{}, 0
		for _, tc := range cases {
			allowed := catalog.Allowed(p, tc.Access)
			started := time.Now()
			status := mode
			var docs []buddy.Document
			if mode == "baseline" {
				docs = buddy.Search(allowed, tc.Query)
			} else {
				docs, status = r.Search(context.Background(), allowed, tc.Query, 10, mode)
			}
			row := item{Query: tc.Query, Expected: tc.Expected, Mode: mode, Category: tc.Category, DurationMS: float64(time.Since(started).Microseconds()) / 1000, Fallback: status == "bm25_fallback"}
			ms = append(ms, row.DurationMS)
			if row.Fallback {
				fallback++
			}
			for rank, doc := range docs {
				row.Found = append(row.Found, doc.ID)
				if !slices.ContainsFunc(allowed, func(d buddy.Document) bool { return d.ID == doc.ID }) || strings.Contains(doc.Body, "ACL_CANARY") {
					violations++
				}
				if slices.Contains(tc.Expected, doc.ID) {
					if rank < 5 {
						row.Recall5 += 1 / float64(len(tc.Expected))
					}
					if row.MRR10 == 0 && rank < 10 {
						row.MRR10 = 1 / float64(rank+1)
					}
				}
			}
			if len(tc.Expected) > 0 {
				recall += row.Recall5
				mrr += row.MRR10
				total++
			}
			rows = append(rows, row)
		}
		slices.Sort(ms)
		sum := 0.0
		for _, v := range ms {
			sum += v
		}
		metrics[mode] = map[string]any{"answerable_cases": total, "recall_at_5": recall / float64(total), "mrr_at_10": mrr / float64(total), "mean_ms": sum / float64(len(ms)), "p95_ms": ms[int(float64(len(ms)-1)*0.95)], "fallback_count": fallback}
	}
	report := map[string]any{"model": embedding.Name(), "cases_count": len(cases), "acl_violations": violations, "metrics": metrics, "cases": rows, "limitations": "Small synthetic Backend corpus. Baseline returns at most 3 results. No-answer/forbidden cases are excluded from recall and retained for inspection. Ranking alone does not abstain or prove factual answer correctness. Cold document indexing and model load are included in latency."}
	raw, _ = json.MarshalIndent(report, "", "  ")
	path := os.Getenv("RETRIEVAL_REPORT")
	if path == "" {
		t.Fatal("RETRIEVAL_REPORT required")
	}
	if err = os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	if violations != 0 {
		t.Fatal("ACL leak", violations)
	}
	for _, mode := range []string{"embeddings", "hybrid"} {
		if metrics[mode].(map[string]any)["fallback_count"].(int) > 0 {
			t.Error("embedding fallback during benchmark", mode)
		}
	}
}
