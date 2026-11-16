package buddy

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type embeddingStub struct {
	calls  int
	inputs []string
	fail   bool
	bad    int
}

func (e *embeddingStub) Name() string { return "test-embedding@1" }
func (e *embeddingStub) Embed(_ context.Context, inputs []string) ([][]float64, error) {
	e.calls++
	e.inputs = append(e.inputs, inputs...)
	if e.fail {
		return nil, errors.New("offline")
	}
	result := [][]float64{}
	for _, input := range inputs {
		v := []float64{0.1, 1}
		if strings.Contains(input, "ревью") || strings.Contains(input, "проверки кода") {
			v = []float64{1, 0.1}
		}
		result = append(result, v)
	}
	if e.bad == 1 {
		return nil, nil
	}
	if e.bad == 2 {
		result[0] = []float64{math.NaN(), 1}
	}
	if e.bad == 3 {
		result[0] = []float64{1}
	}
	return result, nil
}

func TestRetrievalRankingFallbackAndIndexLifecycle(t *testing.T) {
	docs := []Document{{ID: "access", Title: "Доступ", Body: "Владелец согласует заявку.", Version: "1"}, {ID: "review", Title: "Ревью", Body: "Перед ревью нужны проверки кода и тесты.", Version: "1"}}
	e := &embeddingStub{}
	path := filepath.Join(t.TempDir(), "index.json")
	r := &Retriever{Embedding: e, CacheFile: path}
	ctx := context.Background()
	for _, mode := range []string{"bm25", "embeddings", "hybrid"} {
		found, status := r.Search(ctx, docs, "проверки кода", 5, mode)
		if status != mode || len(found) == 0 || found[0].ID != "review" {
			t.Fatal(mode, status, found)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	e.inputs = nil
	r = &Retriever{Embedding: e, CacheFile: path}
	found, status := r.Search(ctx, docs, "ревью", 3, "hybrid")
	if status != "hybrid" || len(found) == 0 || len(e.inputs) != 1 {
		t.Fatal(status, found, e.inputs)
	}
	docs[1].Body = "Обновлённые критерии ревью"
	docs[1].Version = "2"
	e.inputs = nil
	r.Search(ctx, docs, "ревью", 3, "hybrid")
	if len(e.inputs) != 2 {
		t.Fatal("updated snapshot not reindexed", e.inputs)
	}
	e.inputs = nil
	r.Search(ctx, docs[:1], "доступ", 3, "hybrid")
	if len(r.vectors) != 1 || len(e.inputs) != 1 {
		t.Fatal("deleted vectors kept", r.vectors, e.inputs)
	}
	e.fail = true
	found, status = r.Search(ctx, docs, "доступ", 0, "hybrid")
	if status != "bm25_fallback" || len(found) != 1 || found[0].ID != "access" {
		t.Fatal(status, found)
	}
	for _, bad := range []int{1, 2, 3} {
		broken := &Retriever{Embedding: &embeddingStub{bad: bad}}
		_, status = broken.Search(ctx, docs, "ревью", 3, "hybrid")
		if status != "bm25_fallback" {
			t.Fatal(bad, status)
		}
	}
	_, status = (&Retriever{Embedding: &embeddingStub{}, CacheFile: path + "/impossible"}).Search(ctx, docs, "доступ", 3, "hybrid")
	if status != "bm25_fallback" {
		t.Fatal(status)
	}
}

func TestRetrievalLimitsChunksAndInvalidVectors(t *testing.T) {
	docs := []Document{{ID: "long", Title: "Ревью", Body: strings.Repeat("ревью проверки ", 120)}}
	chunks := fragments(docs)
	if len(chunks) < 2 {
		t.Fatal(chunks)
	}
	for _, chunk := range chunks {
		if len([]rune(chunk.doc.Body)) > 700 {
			t.Fatal("oversized chunk")
		}
	}
	r := &Retriever{}
	found, status := r.Search(context.Background(), docs, "ревью", 100, "bm25")
	if status != "bm25" || len(found) != 1 {
		t.Fatal(status, found)
	}
	found, status = r.Search(context.Background(), nil, "неизвестно", 3, "hybrid")
	if len(found) != 0 || status != "bm25" {
		t.Fatal(found, status)
	}
	found, _ = r.Search(context.Background(), docs, "zzxyzz", 3, "bm25")
	if len(found) != 0 {
		t.Fatal(found)
	}
	if validVector(nil) || validVector([]float64{0, 0}) || validVector([]float64{math.Inf(1)}) || cosine([]float64{1}, []float64{1, 0}) != -1 || cosine([]float64{1, 0}, []float64{1, 0}) != 1 {
		t.Fatal("vector validation")
	}
	if len(bm25([]fragment{{terms: []string{}}}, "x")) != 0 {
		t.Fatal("empty corpus")
	}
}
