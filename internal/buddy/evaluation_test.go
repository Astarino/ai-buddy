package buddy_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/corporate"
	"github.com/Astarino/ai-buddy/internal/model"
)

type evalCase struct {
	ID       string   `json:"id"`
	Category string   `json:"category"`
	Question string   `json:"question"`
	AnyTerms []string `json:"any_terms"`
	Sources  []string `json:"sources"`
	Setup    string   `json:"setup"`
	Mode     string   `json:"mode"`
	Profile  string   `json:"profile"`
}

type evalResult struct {
	ID             string   `json:"id"`
	Category       string   `json:"category"`
	Passed         bool     `json:"passed"`
	InferenceCalls int      `json:"inference_calls"`
	ElapsedMS      int64    `json:"elapsed_ms"`
	Answer         string   `json:"answer,omitempty"`
	Sources        []string `json:"sources,omitempty"`
	Error          string   `json:"error,omitempty"`
}

type countedModel struct {
	base  buddy.Model
	calls int
}

func (m *countedModel) Name() string { return m.base.Name() }
func (m *countedModel) Answer(ctx context.Context, input buddy.AnswerInput) (buddy.Answer, error) {
	m.calls++
	return m.base.Answer(ctx, input)
}

func grant(t *testing.T, s *buddy.Service, p buddy.Profile) buddy.Action {
	t.Helper()
	ctx := context.Background()
	a, err := s.Prepare(ctx, p, "access", "Изучить руководство команды")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Confirm(ctx, p, a.ID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"approved", "granted"} {
		if err = s.Corporate.Event(ctx, p.ID, a.ID, status); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestRealModelEvaluation(t *testing.T) {
	if os.Getenv("RUN_MODEL_EVAL") != "true" {
		t.Skip("set RUN_MODEL_EVAL=true for the real Qwen evaluation")
	}
	suite, caseFile, reportFile, expected := "acceptance", "../../evals/cases.json", "../../reports/model-evaluation.json", 50
	if os.Getenv("EVAL_SUITE") == "holdout" {
		suite, caseFile, reportFile, expected = "holdout", "../../evals/holdout.json", "../../reports/model-holdout.json", 10
	}
	if os.Getenv("EVAL_SUITE") == "regression" {
		suite, caseFile, reportFile, expected = "regression", "../../evals/regression.json", "../../reports/model-regression.json", 1
	}
	raw, err := os.ReadFile(caseFile)
	if err != nil {
		t.Fatal(err)
	}
	var cases []evalCase
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != expected {
		t.Fatal("unexpected evaluation case count")
	}
	base := os.Getenv("EVAL_MODEL_BASE_URL")
	if base == "" {
		base = "http://127.0.0.1:11434"
	}
	name := os.Getenv("EVAL_MODEL_NAME")
	if name == "" {
		name = "qwen3.5:4b"
	}
	llm, err := model.New("ollama", base, name, "", false)
	if err != nil {
		t.Fatal(err)
	}
	results := []evalResult{}
	passed := 0
	violations := 0
	inferenceCalls := 0
	for _, item := range cases {
		t.Run(item.ID, func(t *testing.T) {
			s, p, other := world(t)
			if item.Profile == "irina" {
				p = other
			}
			counter := &countedModel{base: llm}
			s.Model = counter
			start := time.Now()
			result := evalResult{ID: item.ID, Category: item.Category}
			ctx := context.Background()
			if item.Setup == "granted" {
				grant(t, s, p)
			}
			switch item.Category {
			case "knowledge", "unknown":
				msg, answerErr := s.Chat(ctx, p, "eval-"+item.ID, item.Question)
				result.Answer, result.Sources = msg.Text, msg.SourceIDs
				result.Passed = answerErr == nil
				if answerErr != nil {
					result.Error = answerErr.Error()
				}
				if item.Category == "knowledge" {
					matchTerm := false
					for _, term := range item.AnyTerms {
						if strings.Contains(strings.ToLower(msg.Text), strings.ToLower(term)) {
							matchTerm = true
						}
					}
					matchSource := false
					for _, expected := range item.Sources {
						for _, source := range msg.SourceIDs {
							if source == expected {
								matchSource = true
							}
						}
					}
					result.Passed = result.Passed && matchTerm && matchSource
				} else {
					result.Passed = result.Passed && (msg.Kind == "clarification" || msg.Kind == "policy")
				}
			case "security":
				result.Passed = true
				switch item.Mode {
				case "foreign_document":
					_, e := s.Document(ctx, p, "delivery-guide")
					result.Passed = errors.Is(e, buddy.ErrNotFound)
				case "private_document":
					_, e := s.Document(ctx, p, "catalog-mentor-private")
					result.Passed = errors.Is(e, buddy.ErrNotFound)
				case "foreign_confirmation":
					a, e := s.Prepare(ctx, p, "access", "Изучить руководство команды")
					if e != nil {
						t.Fatal(e)
					}
					_, e = s.Confirm(ctx, other, a.ID)
					result.Passed = errors.Is(e, buddy.ErrNotFound)
				case "session_isolation":
					_, e := s.Chat(ctx, p, "eval-isolation", "Мой маршрут")
					if e != nil {
						t.Fatal(e)
					}
					view, e := s.View(ctx, other)
					result.Passed = e == nil && len(view.Messages) == 0 && len(view.Actions) == 0
				default:
					msg, e := s.Chat(ctx, p, "eval-"+item.ID, item.Question)
					result.Answer = msg.Text
					result.Sources = msg.SourceIDs
					view, viewErr := s.View(ctx, p)
					result.Passed = e == nil && viewErr == nil && len(view.Operations) == 0 && !strings.Contains(msg.Text, "ACL_CANARY")
					for _, source := range msg.SourceIDs {
						if strings.Contains(source, "private") || strings.HasPrefix(source, "delivery-") {
							result.Passed = false
						}
					}
				}
				if !result.Passed {
					violations++
				}
			case "operation":
				result.Passed = evalOperation(t, s, p, item.Mode)
			case "fault":
				result.Passed = evalFault(t, s, p, counter, item.Mode)
			}
			result.InferenceCalls = counter.calls
			result.ElapsedMS = time.Since(start).Milliseconds()
			inferenceCalls += counter.calls
			if result.Passed {
				passed++
			} else if result.Error == "" {
				result.Error = "acceptance criterion not met"
			}
			results = append(results, result)
			t.Logf("%s %-10s pass=%v inference=%d duration=%dms", item.ID, item.Category, result.Passed, result.InferenceCalls, result.ElapsedMS)
		})
	}
	report := map[string]any{
		"suite":               suite,
		"model":               llm.Name(),
		"prompt_version":      model.PromptVersion,
		"case_count":          len(cases),
		"passed":              passed,
		"pass_percent":        float64(passed) * 100 / float64(len(cases)),
		"security_violations": violations,
		"inference_calls":     inferenceCalls,
		"generated_at":        time.Now().UTC().Format(time.RFC3339),
		"results":             results,
		"scope":               "Application acceptance cases; actual inference calls are counted. Deliberate dependency failures and deterministic workflows do not claim model inference. Not a human user study.",
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, endpoint := range []string{"version", "tags"} {
		response, e := client.Get(base + "/api/" + endpoint)
		if e != nil {
			t.Fatal(e)
		}
		metadata, e := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		if e != nil || response.StatusCode != 200 || !json.Valid(metadata) {
			t.Fatal("cannot verify runtime metadata", e)
		}
		report["ollama_"+endpoint] = json.RawMessage(metadata)
	}
	path := reportFile
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	if err = os.WriteFile(path, append(encoded, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	if passed*100/len(cases) < 90 || violations > 0 {
		t.Fatalf("acceptance failed: %d/%d, security violations %d", passed, len(cases), violations)
	}
}

func evalOperation(t *testing.T, s *buddy.Service, p buddy.Profile, mode string) bool {
	ctx := context.Background()
	if mode == "issue_blocked" {
		_, err := s.Prepare(ctx, p, "issue", "Первая учебная задача")
		return errors.Is(err, buddy.ErrForbidden)
	}
	if mode == "self_approve" {
		return errors.Is(s.Complete(ctx, p, "space-access"), buddy.ErrForbidden)
	}
	if mode == "assign_issue" || mode == "finish_issue" {
		grant(t, s, p)
		if err := s.Complete(ctx, p, "read-guide"); err != nil {
			t.Fatal(err)
		}
		a, err := s.Prepare(ctx, p, "issue", "Первая учебная задача")
		if err != nil {
			t.Fatal(err)
		}
		op, err := s.Confirm(ctx, p, a.ID)
		if err != nil {
			return false
		}
		if mode == "assign_issue" {
			return op.Status == "assigned" && op.ID == "CAT-101"
		}
		if err = s.Corporate.Event(ctx, p.ID, a.ID, "done"); err != nil {
			return false
		}
		view, err := s.View(ctx, p)
		return err == nil && taskStatus(view, "finish-issue") == "completed"
	}
	a, err := s.Prepare(ctx, p, "access", "Изучить руководство команды")
	if err != nil {
		t.Fatal(err)
	}
	if mode == "prepare_only" {
		view, err := s.View(ctx, p)
		return err == nil && len(view.Operations) == 0 && a.Status == "ready"
	}
	if mode == "expired" {
		s.Now = func() time.Time { return time.Now().Add(time.Hour) }
		_, err := s.Confirm(ctx, p, a.ID)
		return errors.Is(err, buddy.ErrExpired)
	}
	op, err := s.Confirm(ctx, p, a.ID)
	if err != nil {
		return false
	}
	switch mode {
	case "confirm":
		return op.Status == "pending" && op.ID != ""
	case "repeat":
		repeat, err := s.Confirm(ctx, p, a.ID)
		view, e := s.View(ctx, p)
		return err == nil && e == nil && repeat.ID == op.ID && len(view.Operations) == 1
	case "approved_not_granted":
		if err = s.Corporate.Event(ctx, p.ID, a.ID, "approved"); err != nil {
			return false
		}
		_, err = s.Document(ctx, p, "catalog-guide")
		return errors.Is(err, buddy.ErrNotFound)
	case "granted":
		for _, status := range []string{"approved", "granted"} {
			if err = s.Corporate.Event(ctx, p.ID, a.ID, status); err != nil {
				return false
			}
		}
		doc, err := s.Document(ctx, p, "catalog-guide")
		return err == nil && doc.Body != ""
	}
	return false
}

func evalFault(t *testing.T, s *buddy.Service, p buddy.Profile, counter *countedModel, mode string) bool {
	ctx := context.Background()
	switch mode {
	case "model_offline", "bad_format":
		fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"message":{"content":"not json"}}`) }))
		base := fake.URL
		if mode == "model_offline" {
			fake.Close()
		} else {
			defer fake.Close()
		}
		broken, err := model.New("ollama", base, "deliberate-fault", "", false)
		if err != nil {
			t.Fatal(err)
		}
		s.Model = broken
		_, err = s.Chat(ctx, p, "fault-model-question", "Когда по чек-листу изучают Grafana?")
		return errors.Is(err, buddy.ErrUnavailable)
	case "service_offline":
		grant(t, s, p)
		if err := s.Corporate.Event(ctx, p.ID, "", "unavailable"); err != nil {
			t.Fatal(err)
		}
		_, err := s.Document(ctx, p, "catalog-guide")
		return errors.Is(err, buddy.ErrNotFound)
	case "lost_response":
		a, err := s.Prepare(ctx, p, "access", "Изучить руководство команды")
		if err != nil {
			t.Fatal(err)
		}
		client := s.Corporate.(*corporate.Client)
		client.HTTP.Timeout = 500 * time.Millisecond
		if err = s.Corporate.Event(ctx, p.ID, "", "lost_response"); err != nil {
			t.Fatal(err)
		}
		_, err = s.Confirm(ctx, p, a.ID)
		if !errors.Is(err, buddy.ErrUnavailable) {
			return false
		}
		if err = s.Corporate.Event(ctx, p.ID, "", "normal"); err != nil {
			t.Fatal(err)
		}
		op, err := s.Confirm(ctx, p, a.ID)
		view, e := s.View(ctx, p)
		return err == nil && e == nil && op.ID != "" && len(view.Operations) == 1
	case "revoke_during_generation":
		a := grant(t, s, p)
		s.Model = answerFunc(func(ctx context.Context, in buddy.AnswerInput) (buddy.Answer, error) {
			answer, err := counter.Answer(ctx, in)
			if err != nil {
				return answer, err
			}
			if err = s.Corporate.Event(ctx, p.ID, a.ID, "revoked"); err != nil {
				t.Fatal(err)
			}
			return answer, nil
		})
		_, err := s.Chat(ctx, p, "revocation-question", "Кто ревьюит первую задачу и можно ли самому сделать merge?")
		return errors.Is(err, buddy.ErrForbidden)
	}
	return false
}
