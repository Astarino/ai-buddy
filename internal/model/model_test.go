package model

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExternalPayloadRequiresConsentAndDropsInternalContext(t *testing.T) {
	m, err := New("compatible", "https://approved-model.example/v1", "qwen", "synthetic-server-key", true)
	if err != nil {
		t.Fatal(err)
	}
	client := m.(*Client)
	calls, source := 0, "source-1"
	client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		for _, private := range []string{"PRIVATE_TEAM", "PRIVATE_HISTORY", "native-doc-42", "native-space", "INTERNAL_ORIGIN", "internal-policy-v1"} {
			if strings.Contains(string(raw), private) {
				t.Fatal("internal metadata sent externally", private)
			}
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-server-key" {
			t.Fatal("provider authentication missing")
		}
		answer, _ := json.Marshal(buddy.Answer{Text: "Публичный ответ", SourceIDs: []string{source}})
		body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(answer)}}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})
	doc := buddy.Document{ID: "native-doc-42", Title: "Публичный материал", Body: "Публичные сведения", Scope: "public", DataClass: "public", System: "native-space", Resource: "native-space", Origin: "INTERNAL_ORIGIN", Version: "internal-policy-v1"}
	input := buddy.AnswerInput{Question: "Как работает этот процесс?", Team: "PRIVATE_TEAM", Role: "PRIVATE_TEAM", History: []buddy.Message{{Role: "user", Text: "PRIVATE_HISTORY"}}, Documents: []buddy.Document{doc}}
	if _, err := client.Answer(context.Background(), input); !errors.Is(err, buddy.ErrDataPolicy) || calls != 0 {
		t.Fatal("external call without consent", err, calls)
	}
	input.ApprovedExternal = true
	input.Documents[0].DataClass = "internal"
	if _, err := client.Answer(context.Background(), input); !errors.Is(err, buddy.ErrDataPolicy) || calls != 0 {
		t.Fatal("internal document sent", err, calls)
	}
	input.Documents[0].DataClass = "public"
	answer, err := client.Answer(context.Background(), input)
	if err != nil || len(answer.SourceIDs) != 1 || answer.SourceIDs[0] != doc.ID || calls != 1 {
		t.Fatal("public citation not mapped back", answer, err, calls)
	}
	source = "not-provided-to-model"
	if _, err := client.Answer(context.Background(), input); !errors.Is(err, buddy.ErrUnavailable) {
		t.Fatal("unknown external citation accepted", err)
	}
	if !client.Boundary().External || client.Boundary().Token == "" {
		t.Fatal("external boundary not described")
	}
}

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
					if body["keep_alive"] != float64(300) {
						t.Error("CPU model lifetime was not preserved")
					}
					if body["think"] != false || body["format"] == nil || body["options"].(map[string]any)["num_ctx"] != float64(4096) {
						t.Error(body)
					}
					if body["options"].(map[string]any)["num_gpu"] != float64(0) {
						t.Error("CPU option was not explicit")
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
			m.(*Client).CPUOnly = provider == "ollama"
			m.(*Client).KeepAliveSeconds = 300
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
