package buddy_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/model"
)

type agentScript struct {
	model.Fixture
	next func([]buddy.AgentTurn) (buddy.AgentTurn, error)
}

func (a agentScript) Next(_ context.Context, turns []buddy.AgentTurn) (buddy.AgentTurn, error) {
	return a.next(turns)
}
func tool(name, args string) buddy.AgentTurn {
	var c buddy.ToolCall
	c.Function.Name = name
	c.Function.Arguments = json.RawMessage(args)
	return buddy.AgentTurn{Calls: []buddy.ToolCall{c}}
}
func final(text string, sources ...string) buddy.AgentTurn {
	raw, _ := json.Marshal(buddy.Answer{Text: text, SourceIDs: sources})
	return buddy.AgentTurn{Content: string(raw)}
}

func TestAgentPreparesWithoutExecutingAndRestores(t *testing.T) {
	s, p, other := world(t)
	ctx := context.Background()
	calls := 0
	s.Model = agentScript{next: func(turns []buddy.AgentTurn) (buddy.AgentTurn, error) {
		calls++
		switch calls {
		case 1:
			return tool("get_context", "{}"), nil
		case 2:
			return tool("search_knowledge", `{"query":"доступ к пространству команды"}`), nil
		case 3:
			return tool("prepare_action", `{"kind":"access","reason":"Изучить руководство команды для первой задачи"}`), nil
		default:
			return final("Подготовил заявку", "source-1"), nil
		}
	}}
	s.Retrieval = &buddy.Retriever{}
	m, err := s.Chat(ctx, p, "agent-access-01", "Хочу начать работу, документация команды не открывается")
	if err != nil || m.ActionID == "" || m.SearchMode != "bm25_fallback" {
		t.Fatal(m, err)
	}
	v, err := s.View(ctx, p)
	if err != nil || len(v.Operations) != 0 || len(v.Actions) != 1 {
		t.Fatal(v, err)
	}
	if _, err = s.Confirm(ctx, other, m.ActionID); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal(err)
	}
	op, err := s.Confirm(ctx, p, m.ActionID)
	if err != nil || op.Status != "pending" {
		t.Fatal(op, err)
	}
	before := calls
	m2, err := s.Chat(ctx, p, "agent-access-01", "Хочу начать работу, документация команды не открывается")
	if err != nil || m2.ActionID != m.ActionID || calls != before {
		t.Fatal(m2, err, calls)
	}
	v, err = s.View(ctx, p)
	if err != nil || len(v.Operations) != 1 {
		t.Fatal(v, err)
	}
}

func TestAgentRejectsInvalidCallsAndLoops(t *testing.T) {
	for _, tc := range []struct {
		name, args string
		want       error
	}{
		{"execute_action", `{}`, buddy.ErrForbidden}, {"get_context", `{"user_id":"irina"}`, buddy.ErrInvalid},
		{"search_knowledge", `{"query":"x"}`, buddy.ErrInvalid}, {"search_knowledge", `{"query":"пароль"}`, buddy.ErrInvalid},
		{"prepare_action", `{"kind":"access","reason":"Начать работу"}`, buddy.ErrInvalid},
		{"prepare_request", `{"kind":"mentor","subject":"Вопрос","body":"Начать работу","recipient":"other"}`, buddy.ErrInvalid},
	} {
		t.Run(tc.name+tc.args, func(t *testing.T) {
			s, p, _ := world(t)
			s.Model = agentScript{next: func([]buddy.AgentTurn) (buddy.AgentTurn, error) { return tool(tc.name, tc.args), nil }}
			_, err := s.Chat(context.Background(), p, buddy.NewID(), "Помоги начать работу")
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
		})
	}
	t.Run("repeat", func(t *testing.T) {
		s, p, _ := world(t)
		s.Model = agentScript{next: func([]buddy.AgentTurn) (buddy.AgentTurn, error) { return tool("get_context", "{}"), nil }}
		_, err := s.Chat(context.Background(), p, buddy.NewID(), "Помоги начать работу")
		if !errors.Is(err, buddy.ErrModelUnavailable) {
			t.Fatal(err)
		}
	})
	t.Run("invalid_answer", func(t *testing.T) {
		s, p, _ := world(t)
		s.Model = agentScript{next: func([]buddy.AgentTurn) (buddy.AgentTurn, error) {
			return buddy.AgentTurn{Content: `{"answer":"ok","extra":1}`}, nil
		}}
		_, err := s.Chat(context.Background(), p, buddy.NewID(), "Помоги начать работу")
		if !errors.Is(err, buddy.ErrModelUnavailable) {
			t.Fatal(err)
		}
	})
	t.Run("unavailable", func(t *testing.T) {
		s, p, _ := world(t)
		s.Model = agentScript{next: func([]buddy.AgentTurn) (buddy.AgentTurn, error) { return buddy.AgentTurn{}, buddy.ErrUnavailable }}
		_, err := s.Chat(context.Background(), p, buddy.NewID(), "Помоги начать работу")
		if !errors.Is(err, buddy.ErrModelUnavailable) {
			t.Fatal(err)
		}
	})
}

func TestAgentRequestDraftAndRevokedSources(t *testing.T) {
	s, p, _ := world(t)
	ctx := context.Background()
	calls := 0
	s.Model = agentScript{next: func(turns []buddy.AgentTurn) (buddy.AgentTurn, error) {
		calls++
		switch calls {
		case 1:
			return tool("get_context", "{}"), nil
		case 2:
			return tool("prepare_request", `{"kind":"mentor","subject":"Первая задача","body":"Какие критерии готовности стоит проверить?"}`), nil
		default:
			return final("Вопрос готов"), nil
		}
	}}
	m, err := s.Chat(ctx, p, buddy.NewID(), "Подготовь вопрос наставнику о критериях задачи")
	if err != nil || m.RequestID == "" {
		t.Fatal(m, err)
	}
	requests, err := s.ListRequests(ctx, p)
	if err != nil || len(requests) != 1 || requests[0].Status != "draft" {
		t.Fatal(requests, err)
	}
	a, err := s.Prepare(ctx, p, "access", "Изучить руководство команды")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Confirm(ctx, p, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"approved", "granted"} {
		if err = s.Operator.Event(ctx, p.ID, a.ID, status); err != nil {
			t.Fatal(err)
		}
	}
	calls = 0
	s.Model = agentScript{next: func(turns []buddy.AgentTurn) (buddy.AgentTurn, error) {
		calls++
		if calls == 1 {
			return tool("search_knowledge", `{"query":"подготовка ревью"}`), nil
		}
		if err := s.Operator.Event(ctx, p.ID, a.ID, "revoked"); err != nil {
			t.Fatal(err)
		}
		return final("Проверь тесты", "source-1"), nil
	}}
	_, err = s.Chat(ctx, p, buddy.NewID(), "Как подготовиться к ревью?")
	if !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
}

type recordedAgent struct {
	buddy.Model
	next      buddy.AgentModel
	Calls     []string
	Outputs   []string
	Decisions []buddy.ToolCall
}

func (r *recordedAgent) Next(ctx context.Context, turns []buddy.AgentTurn) (buddy.AgentTurn, error) {
	turn, err := r.next.Next(ctx, turns)
	r.Outputs = append(r.Outputs, turn.Content)
	r.Decisions = append(r.Decisions, turn.Calls...)
	for _, call := range turn.Calls {
		r.Calls = append(r.Calls, call.Function.Name)
	}
	return turn, err
}

func TestLiveAgentSelection(t *testing.T) {
	mode := os.Getenv("RUN_AGENT_LIVE")
	if mode == "" {
		t.Skip("explicit real Qwen evaluation")
	}
	type result struct {
		Question          string           `json:"question"`
		State             string           `json:"state"`
		Tools             []string         `json:"model_selected_tools"`
		RawOutputs        []string         `json:"model_outputs"`
		Message           buddy.Message    `json:"message"`
		Error             string           `json:"error,omitempty"`
		DurationMS        int64            `json:"duration_ms"`
		Passed            bool             `json:"passed"`
		ModelChoicePassed bool             `json:"model_choice_passed"`
		Decisions         []buddy.ToolCall `json:"model_decisions"`
	}
	results := []result{}
	defer func() {
		if path := os.Getenv("AGENT_REPORT"); path != "" {
			raw, _ := json.MarshalIndent(map[string]any{"model": "qwen3.5:4b", "prompt": model.AgentPromptVersion, "mode": "real-local-ollama", "digest": agentEvalDigest(), "server_policy": "ACL, exact confirmation, idempotency, no model write tool", "cases": results}, "", "  ")
			if err := os.WriteFile(path, raw, 0644); err != nil {
				t.Error(err)
			}
		}
	}()
	for i, tc := range []struct{ q, state, want string }{
		{"Привет! Хочу приступить к первой задаче, но у меня не открывается документация команды. Проверь, что мне нужно сделать, и помоги начать работу.", "none", "prepare_action"},
		{"Материалы команды недоступны. Помоги подготовиться к работе.", "pending", "get_context"},
		{"Мне согласовали заявку. Можно уже читать руководство?", "approved", "get_context"},
		{"Доступ выдан. Хочу разобраться в руководстве и начать первую задачу.", "granted", "search_knowledge"},
		{"Руководство изучено, помоги приступить к первой задаче.", "read", "prepare_action"},
		{"Я получил первую задачу. Как понять, что она готова к ревью, и где посмотреть требования команды?", "assigned", "search_knowledge"},
		{"Подготовь наставнику вопрос: какие критерии готовности моей первой задачи?", "none", "prepare_request"},
	} {
		if mode == "smoke" && i > 0 {
			break
		}
		if mode != "full" && mode != "smoke" && mode != tc.state {
			continue
		}
		t.Run(tc.state+tc.want, func(t *testing.T) {
			s, p, _ := world(t)
			ctx := context.Background()
			if tc.state != "none" {
				a, err := s.Prepare(ctx, p, "access", "Изучить руководство команды")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.Confirm(ctx, p, a.ID); err != nil {
					t.Fatal(err)
				}
				if tc.state != "pending" {
					if err = s.Operator.Event(ctx, p.ID, a.ID, "approved"); err != nil {
						t.Fatal(err)
					}
				}
				if tc.state == "granted" || tc.state == "read" || tc.state == "assigned" {
					if err = s.Operator.Event(ctx, p.ID, a.ID, "granted"); err != nil {
						t.Fatal(err)
					}
				}
				if tc.state == "read" || tc.state == "assigned" {
					if err = s.Complete(ctx, p, "read-guide"); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tc.state == "assigned" {
				a, err := s.Prepare(ctx, p, "issue", "Первая задача после изучения руководства")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.Confirm(ctx, p, a.ID); err != nil {
					t.Fatal(err)
				}
			}
			llm, err := model.New("ollama", "http://127.0.0.1:11434", "qwen3.5:4b", "", false)
			if err != nil {
				t.Fatal(err)
			}
			if os.Getenv("AGENT_MODEL_CPU_ONLY") == "true" {
				llm.(*model.Client).CPUOnly = true
				llm.(*model.Client).KeepAliveSeconds = 300
			}
			recorder := &recordedAgent{Model: llm, next: llm.(buddy.AgentModel)}
			s.Model = recorder
			embedding, err := model.NewEmbedder("http://127.0.0.1:11434", "embeddinggemma:300m", "85462619ee721b466c5927d109d4cb765861907d5417b9109caebc4e614679f1", llm)
			if err != nil {
				t.Fatal(err)
			}
			s.Retrieval = &buddy.Retriever{Embedding: embedding}
			started := time.Now()
			m, err := s.Chat(ctx, p, buddy.NewID(), tc.q)
			res := result{Question: tc.q, State: tc.state, Tools: recorder.Calls, RawOutputs: recorder.Outputs, Decisions: recorder.Decisions, Message: m, DurationMS: time.Since(started).Milliseconds()}
			if err != nil {
				res.Error = err.Error()
			}
			res.Passed = err == nil
			view, viewErr := s.View(ctx, p)
			res.Passed = res.Passed && viewErr == nil
			if tc.want == "prepare_action" {
				kind := "access"
				if tc.state == "read" {
					kind = "issue"
				}
				res.Passed = res.Passed && m.ActionID != "" && slices.ContainsFunc(view.Actions, func(a buddy.Action) bool { return a.ID == m.ActionID && a.Kind == kind && a.Status == "ready" })
			}
			if tc.state == "pending" || tc.state == "approved" {
				res.Passed = res.Passed && m.ActionID == "" && len(view.Operations) == 1
				_, docErr := s.Document(ctx, p, p.Team+"-guide")
				res.Passed = res.Passed && errors.Is(docErr, buddy.ErrNotFound)
			}
			if tc.state == "granted" {
				res.Passed = res.Passed && m.ActionID == "" && slices.Contains(m.SourceIDs, p.Team+"-guide")
			}
			if tc.state == "assigned" {
				res.Passed = res.Passed && m.ActionID == "" && slices.Contains(m.SourceIDs, p.Team+"-review")
			}
			if tc.want == "prepare_request" {
				requests, listErr := s.ListRequests(ctx, p)
				res.Passed = res.Passed && listErr == nil && slices.ContainsFunc(requests, func(r buddy.HelpRequest) bool {
					return r.ID == m.RequestID && r.Status == "draft" && r.AssigneeID == p.MentorID
				})
			}
			res.ModelChoicePassed = res.Passed && slices.Contains(recorder.Calls, tc.want)
			if tc.state == "pending" || tc.state == "approved" || tc.state == "granted" {
				res.ModelChoicePassed = res.ModelChoicePassed && !slices.Contains(recorder.Calls, "prepare_action")
			}
			results = append(results, res)
			if !res.Passed {
				t.Errorf("%+v", res)
			}
		})
	}
}

func agentEvalDigest() string {
	if value := os.Getenv("AGENT_MODEL_DIGEST"); value != "" {
		return value
	}
	return "7f3251fa6878a78a606bcb3c074283306e5ad2ea411a7774a9dd98a4bc7f2d13"
}

func TestAgentRecoversUnstructuredFinalWithoutDisplayingIt(t *testing.T) {
	for _, status := range []string{"none", "pending", "approved"} {
		t.Run(status, func(t *testing.T) {
			s, p, _ := world(t)
			ctx := context.Background()
			operationID := ""
			if status != "none" {
				a, err := s.Prepare(ctx, p, "access", "Изучить руководство команды")
				if err != nil {
					t.Fatal(err)
				}
				op, err := s.Confirm(ctx, p, a.ID)
				if err != nil {
					t.Fatal(err)
				}
				operationID = op.ID
				if status == "approved" {
					if err = s.Operator.Event(ctx, p.ID, a.ID, "approved"); err != nil {
						t.Fatal(err)
					}
				}
			}
			calls := 0
			s.Model = agentScript{next: func([]buddy.AgentTurn) (buddy.AgentTurn, error) {
				calls++
				if calls == 1 {
					return tool("get_context", "{}"), nil
				}
				if status == "none" && calls == 2 {
					return tool("search_knowledge", `{"query":"доступ к пространству"}`), nil
				}
				return buddy.AgentTurn{Content: "UNVALIDATED_PROSE_MUST_NOT_BE_DISPLAYED"}, nil
			}}
			m, err := s.Chat(ctx, p, buddy.NewID(), "Как получить доступ к пространству?")
			if err != nil || strings.Contains(m.Text, "UNVALIDATED") {
				t.Fatal(m, err)
			}
			if status == "none" {
				if m.Kind != "agent_repaired" || len(m.SourceIDs) == 0 {
					t.Fatal(m)
				}
			} else if m.Kind != "agent_status" || !strings.Contains(m.Text, operationID) || !strings.Contains(m.Text, "не выдан") {
				t.Fatal(m)
			}
		})
	}
}

func TestAgentFinishRejectsUnretrievedCitation(t *testing.T) {
	for _, sources := range []string{`[]`, `["closed-source"]`} {
		t.Run(sources, func(t *testing.T) {
			s, p, _ := world(t)
			s.Model = agentScript{next: func([]buddy.AgentTurn) (buddy.AgentTurn, error) {
				return tool("finish", `{"answer":"Что нужно уточнить?","source_ids":`+sources+`,"needs_clarification":true}`), nil
			}}
			m, err := s.Chat(context.Background(), p, buddy.NewID(), "Помоги разобраться")
			if sources == `[]` {
				if err != nil || m.Kind != "clarification" {
					t.Fatal(m, err)
				}
			} else if !errors.Is(err, buddy.ErrModelUnavailable) {
				t.Fatal(err)
			}
		})
	}
}
