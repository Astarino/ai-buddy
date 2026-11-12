package buddy_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/model"
)

type permissionFunc func(context.Context, buddy.Profile, buddy.Resource, string) error

func (f permissionFunc) Check(ctx context.Context, p buddy.Profile, r buddy.Resource, action string) error {
	return f(ctx, p, r, action)
}

func TestCorporateDocumentsUseSpecificResourceDecisions(t *testing.T) {
	s, p, other := world(t)
	ctx := context.Background()
	grant(t, s, p)
	p.Issuer, p.Subject = "https://idp.example", "employee-17"
	first := buddy.Document{ID: "native-page-one", Title: "Проверка отдельного документа", Body: "Проверка документа: разрешена только первая страница.", Keywords: "проверка отдельного документа", Team: p.Team, Scope: "space", Resource: "page-one", System: "confluence", ResourceKind: "document", Version: "1", DataClass: "internal"}
	second := first
	second.ID, second.Resource, second.Body = "native-page-two", "page-two", "SYNTHETIC_UNAUTHORIZED_PAGE"
	s.Catalog.Documents = []buddy.Document{first, second}
	s.Demo = false
	allowed := true
	s.Authorizer = permissionFunc(func(_ context.Context, actor buddy.Profile, r buddy.Resource, action string) error {
		if actor.ID == p.ID && actor.Subject == p.Subject && allowed && r.ID == "page-one" && action == "read" {
			return nil
		}
		return buddy.ErrForbidden
	})
	if _, err := s.Document(ctx, p, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Document(ctx, p, second.ID); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal("space grant opened another resource", err)
	}
	if _, err := s.Document(ctx, other, first.ID); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal("another user inherited permission", err)
	}
	if err := s.Complete(ctx, p, "read-guide"); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal("unreadable guide marked completed", err)
	}
	reply, err := s.Chat(ctx, p, "specific-resource-question", "Проверка отдельного документа")
	if err != nil || strings.Contains(reply.Text, "UNAUTHORIZED") {
		t.Fatal(reply, err)
	}
	allowed = false
	replay, err := s.Chat(ctx, p, "specific-resource-question", "Проверка отдельного документа")
	if err != nil || replay.Kind != "restricted" {
		t.Fatal("resource revocation did not hide replay", replay, err)
	}
	s.Authorizer = permissionFunc(func(context.Context, buddy.Profile, buddy.Resource, string) error { return buddy.ErrUnavailable })
	view, err := s.View(ctx, p)
	if err != nil || view.ServiceFresh || len(view.Documents) != 0 {
		t.Fatal("authority outage leaked sources", view, err)
	}
	s.Authorizer = nil
	if _, err := s.Prepare(ctx, p, "access", "Получить доступ для задачи"); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal("corporate workflow used demo authority", err)
	}
}

func TestPermissionIsRecheckedAtConfirmationAndTargetChange(t *testing.T) {
	s, p, _ := world(t)
	ctx := context.Background()
	s.Demo = false
	allowed := true
	s.Authorizer = permissionFunc(func(_ context.Context, _ buddy.Profile, r buddy.Resource, action string) error {
		if allowed && r.ID == p.Space && action == "request_access" {
			return nil
		}
		return buddy.ErrForbidden
	})
	a, err := s.Prepare(ctx, p, "access", "Изучить руководство команды")
	if err != nil {
		t.Fatal(err)
	}
	allowed = false
	if _, err := s.Confirm(ctx, p, a.ID); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal("revoked permission executed action", err)
	}
	allowed = true
	p.Space = "NEW-TEAM-SPACE"
	if _, err := s.Confirm(ctx, p, a.ID); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal("old card followed changed profile target", err)
	}
}

type externalFixture struct {
	calls  int
	target string
}

func (*externalFixture) Name() string { return "fixture-external" }
func (m *externalFixture) Boundary() buddy.ModelBoundary {
	return buddy.ModelBoundary{External: true, Endpoint: "https://approved-model.example/v1", Token: m.target}
}
func (m *externalFixture) Answer(ctx context.Context, input buddy.AnswerInput) (buddy.Answer, error) {
	m.calls++
	if !input.ApprovedExternal || len(input.History) != 0 || input.Team != "" || input.Role != "" || input.Direction != "" {
		return buddy.Answer{}, errors.New("private context crossed boundary")
	}
	return (model.Fixture{}).Answer(ctx, input)
}

func TestExternalProcessingRequiresPolicyPublicDataAndBoundConsent(t *testing.T) {
	s, p, _ := world(t)
	ctx := context.Background()
	m := &externalFixture{target: "provider-one"}
	s.Model = m
	const question = "Покажи общий чек-лист"
	if _, err := s.ChatWithConsent(ctx, p, "external-question", question, m.target); !errors.Is(err, buddy.ErrDataPolicy) {
		t.Fatal(err)
	}
	s.ModelDataPolicy = "public-only"
	if _, err := s.Chat(ctx, p, "external-no-consent", question); !errors.Is(err, buddy.ErrConsentRequired) {
		t.Fatal(err)
	}
	if _, err := s.ChatWithConsent(ctx, p, "external-wrong-consent", question, "different-provider"); !errors.Is(err, buddy.ErrConsentRequired) {
		t.Fatal(err)
	}
	if m.calls != 0 {
		t.Fatal("provider called before policy/consent", m.calls)
	}
	if _, err := s.ChatWithConsent(ctx, p, "external-no-consent", question, m.target); err != nil || m.calls != 1 {
		t.Fatal(err, m.calls)
	}
	m.target = "provider-two"
	if _, err := s.ChatWithConsent(ctx, p, "external-changed-provider", question, "provider-one"); !errors.Is(err, buddy.ErrConsentRequired) {
		t.Fatal("old approval followed model change", err)
	}
	for i := range s.Catalog.Documents {
		if s.Catalog.Documents[i].ID == "checklist" {
			s.Catalog.Documents[i].DataClass = "unknown"
		}
	}
	if _, err := s.ChatWithConsent(ctx, p, "external-unknown-data", question, m.target); !errors.Is(err, buddy.ErrDataPolicy) {
		t.Fatal("unclassified data sent", err)
	}
	grant(t, s, p)
	if _, err := s.ChatWithConsent(ctx, p, "external-private-data", "Как подготовиться к первому ревью?", m.target); !errors.Is(err, buddy.ErrDataPolicy) {
		t.Fatal("closed source sent outside", err)
	}
	if m.calls != 1 {
		t.Fatal("rejected context still reached provider", m.calls)
	}
}

func TestOtherDirectionsAndQuestionsOutsideChecklist(t *testing.T) {
	s, p, _ := world(t)
	p.Direction, p.Role = "analytics", "Стажёр-аналитик"
	s.Catalog.Documents = append(s.Catalog.Documents, buddy.Document{ID: "analytics-guide", Title: "Как проверить SQL-витрину", Body: "В тестовом примере сначала сравни число строк и уникальность ключа витрины.", Keywords: "SQL витрина уникальность", Direction: "analytics", Scope: "team", Version: "1"})
	called := false
	s.Model = answerFunc(func(ctx context.Context, input buddy.AnswerInput) (buddy.Answer, error) {
		called = true
		if input.Direction != "analytics" || input.Role != p.Role {
			t.Fatal("direction lost", input.Direction, input.Role)
		}
		for _, doc := range input.Documents {
			if doc.Direction == "backend" {
				t.Fatal("Backend-only source used for analyst")
			}
		}
		return (model.Fixture{}).Answer(ctx, input)
	})
	reply, err := s.Chat(context.Background(), p, "analytics-outside-checklist", "Как проверить SQL-витрину и уникальность ключа?")
	if err != nil || !called || len(reply.SourceIDs) == 0 || reply.SourceIDs[0] != "analytics-guide" {
		t.Fatal(reply, err)
	}
	view, err := s.View(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range view.Tasks {
		if task.Direction == "backend" {
			t.Fatal("Backend-only route used for analyst")
		}
	}
	for _, question := range []string{"Мой пароль synthetic-password", "API-токен synthetic-token"} {
		if _, err := s.Draft(context.Background(), p, question); !errors.Is(err, buddy.ErrForbidden) {
			t.Fatal("secret entered mentor draft", err)
		}
		if _, err := s.Prepare(context.Background(), p, "access", question); !errors.Is(err, buddy.ErrForbidden) {
			t.Fatal("secret entered action reason", err)
		}
	}
}
