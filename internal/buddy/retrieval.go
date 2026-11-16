package buddy

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Embedder interface {
	Name() string
	Embed(context.Context, []string) ([][]float64, error)
}

type Retriever struct {
	Embedding Embedder
	CacheFile string
	mu        sync.Mutex
	vectors   map[string][]float64
}

type fragment struct {
	doc   Document
	key   string
	terms []string
}
type scoredFragment struct {
	index int
	score float64
}

func fragments(docs []Document) []fragment {
	result := []fragment{}
	for _, doc := range docs {
		body := []rune(doc.Body)
		for start := 0; start < len(body); start += 600 {
			end := min(start+700, len(body))
			chunk := doc
			chunk.Body = string(body[start:end])
			text := doc.Title + " " + doc.Title + " " + doc.Keywords + " " + chunk.Body
			result = append(result, fragment{chunk, fingerprint([]string{fingerprint(doc), chunk.Body}), retrievalWords(text)})
			if end == len(body) {
				break
			}
		}
	}
	return result
}

func retrievalWords(text string) []string {
	tokens := words(text)
	result := []string{}
	for _, token := range tokens {
		r := []rune(token)
		if len(r) < 2 || slicesWord(token) {
			continue
		}
		if len(r) > 5 {
			token = string(r[:len(r)-2])
		}
		result = append(result, token)
	}
	return result
}

func slicesWord(s string) bool {
	return strings.Contains("|как|что|это|для|или|мне|меня|где|при|про|его|она|они|уже|хочу|пожалуйста|", "|"+s+"|")
}

func bm25(chunks []fragment, query string) []scoredFragment {
	avg := 0.0
	df := map[string]int{}
	for _, chunk := range chunks {
		avg += float64(len(chunk.terms))
		seen := map[string]bool{}
		for _, term := range chunk.terms {
			if !seen[term] {
				df[term]++
				seen[term] = true
			}
		}
	}
	if len(chunks) == 0 {
		return nil
	}
	avg /= float64(len(chunks))
	if avg == 0 {
		return nil
	}
	ranked := []scoredFragment{}
	for i, chunk := range chunks {
		freq := map[string]int{}
		for _, term := range chunk.terms {
			freq[term]++
		}
		score := 0.0
		for _, term := range retrievalWords(query) {
			tf := float64(freq[term])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(chunks)-df[term])+0.5)/(float64(df[term])+0.5))
			score += idf * tf * 2.2 / (tf + 1.2*(0.25+0.75*float64(len(chunk.terms))/avg))
		}
		if score > 0 {
			ranked = append(ranked, scoredFragment{i, score})
		}
	}
	sortScores(ranked)
	return ranked
}

func sortScores(scores []scoredFragment) {
	sort.SliceStable(scores, func(i, j int) bool { return scores[i].score > scores[j].score })
}

func validVector(v []float64) bool {
	if len(v) == 0 || len(v) > 4096 {
		return false
	}
	norm := 0.0
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
		norm += x * x
	}
	return norm > 0
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) || !validVector(a) || !validVector(b) {
		return -1
	}
	dot, aa, bb := 0.0, 0.0, 0.0
	for i, x := range a {
		dot += x * b[i]
		aa += x * x
		bb += b[i] * b[i]
	}
	return dot / math.Sqrt(aa*bb)
}

func (r *Retriever) vectorRanks(ctx context.Context, chunks []fragment, query string) ([]scoredFragment, error) {
	if r.Embedding == nil {
		return nil, ErrUnavailable
	}
	name := r.Embedding.Name()
	if r.vectors == nil {
		r.vectors = map[string][]float64{}
		if r.CacheFile != "" {
			if raw, err := os.ReadFile(r.CacheFile); err == nil {
				_ = json.Unmarshal(raw, &r.vectors)
				if r.vectors == nil {
					r.vectors = map[string][]float64{}
				}
			}
		}
	}
	active := map[string]bool{}
	inputs := []string{"task: search result | query: " + query}
	missing := []string{}
	for _, chunk := range chunks {
		key := name + ":" + chunk.key
		active[key] = true
		if !validVector(r.vectors[key]) {
			missing = append(missing, key)
			inputs = append(inputs, "title: "+chunk.doc.Title+" | text: "+chunk.doc.Body)
		}
	}
	// Remove stale snapshots, deleted documents and documents outside this allowed set.
	for key := range r.vectors {
		if !active[key] {
			delete(r.vectors, key)
		}
	}
	vectors, err := r.Embedding.Embed(ctx, inputs)
	if err != nil || len(vectors) != len(inputs) {
		return nil, ErrUnavailable
	}
	for _, v := range vectors {
		if !validVector(v) || len(v) != len(vectors[0]) {
			return nil, ErrUnavailable
		}
	}
	for i, key := range missing {
		r.vectors[key] = vectors[i+1]
	}
	if r.CacheFile != "" {
		if err = r.save(); err != nil {
			return nil, err
		}
	}
	ranked := []scoredFragment{}
	for i, chunk := range chunks {
		v := r.vectors[name+":"+chunk.key]
		if len(v) != len(vectors[0]) {
			return nil, ErrUnavailable
		}
		score := cosine(vectors[0], v)
		if score > 0 {
			ranked = append(ranked, scoredFragment{i, score})
		}
	}
	sortScores(ranked)
	return ranked, nil
}

func (r *Retriever) save() error {
	if err := os.MkdirAll(filepath.Dir(r.CacheFile), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(r.vectors)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(r.CacheFile), ".vectors-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), r.CacheFile)
}

// Search accepts only an already-authorized snapshot; it never discovers documents.
func (r *Retriever) Search(ctx context.Context, docs []Document, query string, limit int, mode string) ([]Document, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit < 1 {
		limit = 3
	}
	if limit > 10 {
		limit = 10
	}
	chunks := fragments(docs)
	lexical := bm25(chunks, query)
	ranked := lexical
	status := "bm25"
	if mode != "bm25" && len(chunks) > 0 {
		semantic, err := r.vectorRanks(ctx, chunks, query)
		if err != nil {
			status = "bm25_fallback"
		} else if mode == "embeddings" {
			ranked, status = semantic, "embeddings"
		} else {
			scores := map[int]float64{}
			for _, list := range [][]scoredFragment{lexical, semantic} {
				for rank, item := range list {
					scores[item.index] += 1 / (60 + float64(rank+1))
				}
			}
			ranked = []scoredFragment{}
			for i := range chunks {
				if scores[i] > 0 {
					ranked = append(ranked, scoredFragment{i, scores[i]})
				}
			}
			sortScores(ranked)
			status = "hybrid"
		}
	}
	result := []Document{}
	seen := map[string]bool{}
	for _, item := range ranked {
		doc := chunks[item.index].doc
		if seen[doc.ID] {
			continue
		}
		seen[doc.ID] = true
		result = append(result, doc)
		if len(result) == limit {
			break
		}
	}
	return result, status
}
