package model

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Astarino/ai-buddy/internal/buddy"
)

func TestNativeToolsAndEmbeddingContract(t *testing.T) {
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	status, reply := 200, `{"message":{"role":"assistant","tool_calls":[{"function":{"name":"get_context","arguments":{}}}]}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(status)
			io.WriteString(w, `{"models":[{"name":"embedding","digest":"`+digest+`"}]}`)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("bad request")
		}
		if r.URL.Path == "/api/chat" {
			if len(body["tools"].([]any)) != 5 || body["keep_alive"].(float64) != 0 || body["think"] != false {
				t.Error(body)
			}
		} else if r.URL.Path == "/api/embed" {
			if body["keep_alive"].(float64) != 0 || body["truncate"] != false {
				t.Error(body)
			}
		}
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer server.Close()
	llm, _ := New("ollama", server.URL, "qwen", "", false)
	client := llm.(*Client)
	client.CPUOnly = true
	turn, err := client.Next(context.Background(), []buddy.AgentTurn{{Role: "user", Content: "Помоги"}})
	if err != nil || len(turn.Calls) != 1 || turn.Calls[0].Function.Name != "get_context" {
		t.Fatal(turn, err)
	}
	reply = `{"message":{"content":"{\"answer\":\"Готово\",\"source_ids\":[],\"needs_clarification\":false}"}}`
	if _, err = client.Next(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	client.gate <- struct{}{}
	if _, err = client.Next(context.Background(), nil); !errors.Is(err, buddy.ErrBusy) {
		t.Fatal(err)
	}
	<-client.gate
	for _, payload := range []string{`no-json`, `{"message":{}}`} {
		reply = payload
		if _, err = client.Next(context.Background(), nil); err == nil {
			t.Fatal(payload)
		}
	}
	status = 503
	if _, err = client.Next(context.Background(), nil); err == nil {
		t.Fatal("HTTP failure accepted")
	}
	status = 200
	embedding, err := NewEmbedder(server.URL, "embedding", digest, llm)
	if err != nil || embedding.Name() != "embedding@"+digest {
		t.Fatal(embedding, err)
	}
	reply = `{"embeddings":[[1,0],[0,1]]}`
	vectors, err := embedding.Embed(context.Background(), []string{"query", "doc"})
	if err != nil || len(vectors) != 2 {
		t.Fatal(vectors, err)
	}
	for _, payload := range []string{`invalid`, `{"embeddings":[]}`} {
		reply = payload
		if _, err = embedding.Embed(context.Background(), []string{"query"}); err == nil {
			t.Fatal(payload)
		}
	}
	status = 500
	if _, err = embedding.Embed(context.Background(), []string{"query"}); err == nil {
		t.Fatal("HTTP failure accepted")
	}
	status = 200
	client.gate <- struct{}{}
	if _, err = embedding.Embed(context.Background(), []string{"query"}); !errors.Is(err, buddy.ErrBusy) {
		t.Fatal(err)
	}
	<-client.gate
	if _, err = embedding.Embed(context.Background(), nil); !errors.Is(err, buddy.ErrInvalid) {
		t.Fatal(err)
	}
	for _, base := range []string{"https://example.com", "bad-url"} {
		if _, err = NewEmbedder(base, "embedding", digest, llm); err == nil {
			t.Fatal("external embedding allowed")
		}
	}
	if _, err = NewEmbedder(server.URL, "embedding", "", llm); err == nil {
		t.Fatal("unversioned embedding allowed")
	}
	client.BaseURL = "https://example.com"
	if _, err = client.Next(context.Background(), nil); !errors.Is(err, buddy.ErrDataPolicy) {
		t.Fatal(err)
	}
}

func TestCompatibleLocalTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer local-key" {
			t.Error(r.URL.Path)
		}
		io.WriteString(w, `{"choices":[{"message":{"tool_calls":[{"id":"call-1","type":"function","function":{"name":"get_context","arguments":"{}"}}]}}]}`)
	}))
	defer server.Close()
	llm, _ := New("compatible", server.URL, "qwen", "local-key", false)
	turn, err := llm.(*Client).Next(context.Background(), nil)
	if err != nil || len(turn.Calls) != 1 || string(turn.Calls[0].Function.Arguments) != "{}" {
		t.Fatal(turn, err)
	}
}

func TestToolsAreRestrictedByTrustedWorkflowState(t *testing.T) {
	var want []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct {
				Function struct {
					Name       string `json:"name"`
					Parameters struct {
						Properties map[string]struct {
							Enum []string `json:"enum"`
						} `json:"properties"`
					} `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("invalid body")
		}
		found := false
		for _, tool := range body.Tools {
			if tool.Function.Name == "get_context" {
				t.Error("repeated context offered")
			}
			if tool.Function.Name == "prepare_action" {
				found = true
				if len(want) != 1 || len(tool.Function.Parameters.Properties["kind"].Enum) != 1 || tool.Function.Parameters.Properties["kind"].Enum[0] != want[0] {
					t.Error("invalid allowed action")
				}
			}
		}
		if found != (len(want) > 0) {
			t.Error("invalid tool availability")
		}
		io.WriteString(w, `{"message":{"content":"{}"}}`)
	}))
	defer server.Close()
	llm, _ := New("ollama", server.URL, "qwen", "", false)
	for _, allowed := range [][]string{{}, {"access"}, {"issue"}} {
		want = allowed
		raw, _ := json.Marshal(map[string]any{"available_actions": allowed})
		_, err := llm.(*Client).Next(context.Background(), []buddy.AgentTurn{{Role: "tool", ToolName: "get_context", Content: string(raw)}})
		if err != nil {
			t.Fatal(err)
		}
	}
}
