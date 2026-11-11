package model

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
)

func TestProviderConfiguration(t *testing.T) {
	for _, tc := range []struct {
		provider, base, name string
		external, valid      bool
	}{
		{"fixture", "", "", false, true}, {"other", "http://localhost", "qwen", false, false},
		{"ollama", "http://127.0.0.1:11434", "qwen", false, true}, {"ollama", "http://localhost:11434", "qwen", false, true},
		{"ollama", "http://host.docker.internal:11434", "qwen", false, true},
		{"compatible", "https://example.com/v1", "qwen", false, false}, {"compatible", "https://example.com/v1", "qwen", true, true},
		{"compatible", "http://example.com/v1", "qwen", true, false}, {"ollama", "not-a-url", "qwen", false, false},
		{"ollama", "http://u:p@localhost", "qwen", false, false}, {"ollama", "http://localhost?key=x", "qwen", false, false},
		{"ollama", "http://localhost", "", false, false},
	} {
		t.Run(tc.provider+tc.base+tc.name, func(t *testing.T) {
			m, err := New(tc.provider, tc.base, tc.name, "", tc.external)
			if (err == nil) != tc.valid {
				t.Fatal(m, err)
			}
			if err == nil && m.Name() == "" {
				t.Fatal("missing name")
			}
		})
	}
	answer, err := (Fixture{}).Answer(context.Background(), buddy.AnswerInput{})
	if err != nil || !answer.NeedsClarification {
		t.Fatal(answer, err)
	}
	answer, err = (Fixture{}).Answer(context.Background(), buddy.AnswerInput{Documents: []buddy.Document{{ID: "doc", Body: "test"}}})
	if err != nil || answer.Text != "test" {
		t.Fatal(answer, err)
	}
}

func TestStructuredAnswerAndProviderBoundaries(t *testing.T) {
	for _, provider := range []string{"ollama", "compatible"} {
		t.Run(provider, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["stream"] != false || body["model"] != "test-qwen" {
					t.Error(body)
				}
				if provider == "ollama" {
					if body["think"] != false || body["format"] == nil || body["options"].(map[string]any)["num_ctx"] != float64(4096) {
						t.Error(body)
					}
					if r.URL.Path != "/api/chat" {
						t.Error(r.URL)
					}
				} else {
					if r.Header.Get("Authorization") != "Bearer test-key" || r.URL.Path != "/chat/completions" {
						t.Error(r)
					}
				}
				content := `{"answer":"Тестовый ответ","source_ids":["access-policy"],"needs_clarification":false}`
				if provider == "ollama" {
					json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": content}})
				} else {
					json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
				}
			}))
			defer server.Close()
			m, err := New(provider, server.URL, "test-qwen", "test-key", false)
			if err != nil {
				t.Fatal(err)
			}
			answer, err := m.Answer(context.Background(), buddy.AnswerInput{Question: "Как получить доступ?", History: []buddy.Message{{Text: strings.Repeat("x", 1000)}}})
			if err != nil || answer.Text != "Тестовый ответ" {
				t.Fatal(answer, err)
			}
		})
	}
}

func TestBadModelResponsesAndSingleConcurrency(t *testing.T) {
	for _, body := range []string{`invalid`, `{"message":{"content":"not json"}}`, `{"message":{"content":"{\"unknown\":true}"}}`, `{"message":{"content":"{} {}"}}`, `{"choices":[]}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer server.Close()
			m, _ := New("ollama", server.URL, "qwen", "", false)
			if _, err := m.Answer(context.Background(), buddy.AnswerInput{}); err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	m, _ := New("ollama", server.URL, "qwen", "", false)
	if _, err := m.Answer(context.Background(), buddy.AnswerInput{}); !errors.Is(err, buddy.ErrUnavailable) {
		t.Fatal(err)
	}
	server.Close()
	if _, err := m.Answer(context.Background(), buddy.AnswerInput{}); !errors.Is(err, buddy.ErrUnavailable) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Answer(ctx, buddy.AnswerInput{}); err == nil {
		t.Fatal("cancel ignored")
	}
	client := m.(*Client)
	client.gate <- struct{}{}
	if _, err := client.Answer(context.Background(), buddy.AnswerInput{}); !errors.Is(err, buddy.ErrBusy) {
		t.Fatal(err)
	}
	<-client.gate
	client.BaseURL = ":bad-url"
	if _, err := client.Answer(context.Background(), buddy.AnswerInput{}); err == nil {
		t.Fatal("bad URL accepted")
	}
	client.HTTP.Timeout = time.Millisecond
}
