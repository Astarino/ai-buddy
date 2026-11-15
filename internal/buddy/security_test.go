package buddy_test

import (
	"context"
	"errors"
	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/model"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReplayRechecksRevokedAccess(t *testing.T) {
	s, p, _ := world(t)
	ctx := context.Background()
	a := grant(t, s, p)
	const id = "audit-replay-question"
	const question = "Как подготовиться к первому ревью?"
	original, err := s.Chat(ctx, p, id, question)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	for _, source := range original.SourceIDs {
		if _, e := s.Catalog.Document(p, false, source); e != nil {
			closed = true
		}
	}
	if !closed {
		t.Fatal("audit setup: original answer must use a closed source")
	}
	if err := s.Operator.Event(ctx, p.ID, a.ID, "revoked"); err != nil {
		t.Fatal(err)
	}
	view, err := s.View(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	masked := false
	for _, m := range view.Messages {
		if m.ID == original.ID && m.Kind == "restricted" {
			masked = true
		}
	}
	if !masked {
		t.Fatal("audit setup: normal View did not hide the answer")
	}
	replay, err := s.Chat(ctx, p, id, question)
	if err == nil && replay.Text == original.Text && replay.Kind != "restricted" {
		t.Error("revoked answer returned intact through Chat replay despite View masking it")
	}
}

func TestRevocationIgnoresModelCitationClaims(t *testing.T) {
	for _, usePublicSource := range []bool{false, true} {
		name := "empty_sources"
		if usePublicSource {
			name = "public_source"
		}
		t.Run(name, func(t *testing.T) {
			s, p, _ := world(t)
			ctx := context.Background()
			a := grant(t, s, p)
			s.Model = answerFunc(func(ctx context.Context, in buddy.AnswerInput) (buddy.Answer, error) {
				closedBody := ""
				for _, d := range in.Documents {
					if d.Scope == "space" {
						closedBody = d.Body
						break
					}
				}
				if closedBody == "" {
					t.Fatal("audit setup: model did not receive a closed document")
				}
				runes := []rune(closedBody)
				if len(runes) > 150 {
					closedBody = string(runes[:150])
				}
				sources := []string{}
				if usePublicSource {
					for _, d := range in.Documents {
						if _, e := s.Catalog.Document(p, false, d.ID); e == nil {
							sources = append(sources, d.ID)
							break
						}
					}
					if len(sources) == 0 {
						t.Fatal("audit setup: missing public source")
					}
				}
				if err := s.Operator.Event(ctx, p.ID, a.ID, "revoked"); err != nil {
					t.Fatal(err)
				}
				return buddy.Answer{Text: closedBody, SourceIDs: sources, NeedsClarification: !usePublicSource}, nil
			})
			_, err := s.Chat(ctx, p, "audit-during-generation", "Как подготовиться к первому ревью?")
			if !errors.Is(err, buddy.ErrForbidden) {
				t.Errorf("closed context survives revocation when citations are %s: err=%v", name, err)
			}
		})
	}
}

func TestBlockedSecretStaysOutOfNextPrompt(t *testing.T) {
	s, p, _ := world(t)
	ctx := context.Background()
	const marker = "AUDIT_SYNTHETIC_SECRET_ONLY"
	calls, leaked := 0, false
	s.Model = answerFunc(func(ctx context.Context, in buddy.AnswerInput) (buddy.Answer, error) {
		calls++
		for _, m := range in.History {
			if strings.Contains(m.Text, marker) {
				leaked = true
			}
		}
		return (model.Fixture{}).Answer(ctx, in)
	})
	blocked, err := s.Chat(ctx, p, "audit-secret-question", "Мой пароль "+marker)
	if err != nil || blocked.Kind != "policy" || calls != 0 {
		t.Fatal("audit setup: initial secret was not blocked")
	}
	if _, err := s.Chat(ctx, p, "audit-normal-question", "Как применить принципы разработки перед ревью?"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("audit setup: expected one fixture model call, got %d", calls)
	}
	if leaked {
		t.Error("blocked secret included in the next model input History")
	}
	state, err := s.Repo.Read(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range state.Messages {
		if strings.Contains(message.Text, marker) {
			t.Fatal("secret persisted in conversation text")
		}
	}
	replay, err := s.Chat(ctx, p, "audit-secret-question", "Мой пароль "+marker)
	if err != nil || replay.Kind != "policy" || calls != 1 {
		t.Fatal("redacted input did not preserve replay semantics", replay, err, calls)
	}
	if _, err := s.Chat(ctx, p, "audit-secret-question", "Другой вопрос"); !errors.Is(err, buddy.ErrConflict) {
		t.Fatal("reused request ID accepted a different question", err)
	}
}

func TestUnknownDocumentScopeFailsClosed(t *testing.T) {
	catalog := buddy.LoadCatalog()
	p, err := catalog.Profile("anton")
	if err != nil {
		t.Fatal(err)
	}
	catalog.Documents = append(catalog.Documents, buddy.Document{ID: "audit-unknown-scope", Team: p.Team, Scope: "new-restricted-scope", Body: "synthetic closed text"})
	if _, err := catalog.Document(p, false, "audit-unknown-scope"); err == nil {
		t.Error("unknown ACL scope allowed instead of failing closed")
	}
}

func TestUnrelated403NeedsResourceClarification(t *testing.T) {
	s, p, _ := world(t)
	msg, err := s.Chat(context.Background(), p, "audit-github-question", "GitHub возвращает 403, что проверить?")
	if err != nil {
		t.Fatal(err)
	}
	if msg.ActionID != "" {
		t.Error("GitHub 403 prepared a Confluence access action without resource clarification")
	}
	if !strings.Contains(msg.Text, "странице GitHub") || strings.Contains(msg.Text, "какой сервис") {
		t.Fatal("asked for an already known service", msg.Text)
	}
}

func TestChangedReasonNeedsNewConfirmation(t *testing.T) {
	s, p, _ := world(t)
	first, err := s.Prepare(context.Background(), p, "access", "Прочитать руководство команды")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Prepare(context.Background(), p, "access", "Разобрать другую рабочую проблему")
	if err == nil && first.ID == second.ID {
		t.Error("changed reason silently returns the old confirmation card")
	}
	if _, err := s.Confirm(context.Background(), p, first.ID); !errors.Is(err, buddy.ErrConflict) {
		t.Fatal("superseded card could be submitted", err)
	}
	if _, err := s.Confirm(context.Background(), p, second.ID); err != nil {
		t.Fatal("new confirmation card failed", err)
	}
}

func TestHistoryDependenciesSurvivePublicOnlyCitations(t *testing.T) {
	s, p, _ := world(t)
	ctx := context.Background()
	a := grant(t, s, p)
	first, err := s.Chat(ctx, p, "private-history-question", "Как подготовиться к первому ревью?")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Chat(ctx, p, "public-history-question", "Покажи общий чек-лист")
	if err != nil || len(second.SourceIDs) != 1 || second.SourceIDs[0] != "checklist" {
		t.Fatal("setup: expected public-only citation", second, err)
	}
	if err = s.Operator.Event(ctx, p.ID, a.ID, "revoked"); err != nil {
		t.Fatal(err)
	}
	view, err := s.View(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range view.Messages {
		if (message.ID == first.ID || message.ID == second.ID) && message.Kind != "restricted" {
			t.Fatal("transitive private context remained visible", message.ID)
		}
		if len(message.Dependencies) != 0 || message.RequestHash != "" || message.ContextVersion != 0 {
			t.Fatal("internal context metadata exposed in View")
		}
	}
	s.Model = answerFunc(func(_ context.Context, input buddy.AnswerInput) (buddy.Answer, error) {
		for _, message := range input.History {
			if message.Text == first.Text || message.Text == second.Text {
				t.Fatal("revoked answer reused as model history")
			}
		}
		return (model.Fixture{}).Answer(ctx, input)
	})
	if _, err := s.Chat(ctx, p, "after-history-revocation", "Покажи общий чек-лист"); err != nil {
		t.Fatal(err)
	}
}

type boundaryRoundTrip func(*http.Request) (*http.Response, error)

func (f boundaryRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestModelRedirectCannotEscapeLocalBoundary(t *testing.T) {
	const marker = "AUDIT_SYNTHETIC_CONTEXT_ONLY"
	received := make(chan bool, 1)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- strings.Contains(string(body), marker)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"answer\":\"ok\",\"source_ids\":[],\"needs_clarification\":true}"}}]}`)
	}))
	defer sink.Close()
	entry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://audit-outside.invalid/collect", http.StatusTemporaryRedirect)
	}))
	defer entry.Close()
	m, err := model.New("compatible", entry.URL, "audit-fixture-model", "", false)
	if err != nil {
		t.Fatal(err)
	}
	client := m.(*model.Client)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	sinkURL, _ := url.Parse(sink.URL)
	client.HTTP.Transport = boundaryRoundTrip(func(r *http.Request) (*http.Response, error) {
		// Keep all traffic on loopback while observing the destination chosen by the real client.
		if r.URL.Hostname() == "audit-outside.invalid" {
			clone := r.Clone(r.Context())
			changed := *r.URL
			changed.Host, changed.Scheme = sinkURL.Host, sinkURL.Scheme
			clone.URL = &changed
			return transport.RoundTrip(clone)
		}
		return transport.RoundTrip(r)
	})
	_, _ = client.Answer(context.Background(), buddy.AnswerInput{Question: marker})
	select {
	case bodyWasSent := <-received:
		if bodyWasSent {
			t.Error("ALLOW_EXTERNAL_MODEL=false bypassed by 307 redirect; synthetic prompt body resent to an outside URL")
		}
	default:
	}
}
