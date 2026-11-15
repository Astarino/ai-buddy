package buddy_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Astarino/ai-buddy/internal/buddy"
)

func TestShortRouteQuestionsWorkWithoutTheModel(t *testing.T) {
	s, p, _ := world(t)
	s.Model = answerFunc(func(context.Context, buddy.AnswerInput) (buddy.Answer, error) {
		t.Fatal("route question reached the unavailable model")
		return buddy.Answer{}, buddy.ErrUnavailable
	})
	for i, question := range []string{"что делать", "Что мне делать?!", "что мне делать дальше", "с чего мне начать"} {
		msg, err := s.Chat(context.Background(), p, "route-question-"+string(rune('a'+i)), question)
		if err != nil || msg.Kind != "workflow" || !strings.Contains(msg.Text, "Ближайший доступный шаг") {
			t.Fatal(msg, err)
		}
	}
}

func TestToolQuestionIsNotReplacedByTheRoute(t *testing.T) {
	s, p, _ := world(t)
	s.Catalog.Documents = append(s.Catalog.Documents, buddy.Document{ID: "go-help", Title: "Запуск тестов Go", Body: "Go test запускают из корня модуля с go.mod.", Scope: "public", DataClass: "public", Keywords: "go test module"})
	calls := 0
	s.Model = answerFunc(func(_ context.Context, in buddy.AnswerInput) (buddy.Answer, error) {
		calls++
		return buddy.Answer{Text: "Перейди в папку с go.mod и повтори go test.", SourceIDs: []string{"go-help"}}, nil
	})
	for i, question := range []string{"В Go команда go test не видит go.mod. Что делать дальше?", "С чего начать проверку go test с ошибкой go.mod?"} {
		msg, err := s.Chat(context.Background(), p, "tool-route-question-"+string(rune('a'+i)), question)
		if err != nil || msg.Kind != "answer" || !strings.Contains(msg.Text, "go.mod") {
			t.Fatal(msg, err)
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestVagueProblemsAndConflictingSourcesAreClarifiedBeforeInference(t *testing.T) {
	s, p, _ := world(t)
	s.Model = answerFunc(func(context.Context, buddy.AnswerInput) (buddy.Answer, error) {
		t.Fatal("incomplete question reached the model")
		return buddy.Answer{}, nil
	})
	for i, q := range []string{"Что мне сделать с ошибкой?", "Не понимаю эту настройку. Помоги разобраться.", "Как исправить эту проблему?", "В документах разные сроки выдачи. Какой верный?"} {
		msg, err := s.Chat(context.Background(), p, "vague-question-"+string(rune('a'+i)), q)
		if err != nil || msg.Kind != "clarification" || !strings.Contains(msg.Text, "?") {
			t.Fatal(msg, err)
		}
	}
}

func TestConcreteToolDetailsArePreservedForTheModel(t *testing.T) {
	s, p, _ := world(t)
	s.Catalog.Documents = append(s.Catalog.Documents, buddy.Document{ID: "pandas-doc", Title: "pandas merge", Body: "pandas merge can report ValueError. Check the column types.", Scope: "public", DataClass: "public"})
	question := "В pandas при merge ошибка ValueError: типы колонок разные. Как проверить?"
	calls := 0
	s.Model = answerFunc(func(_ context.Context, in buddy.AnswerInput) (buddy.Answer, error) {
		calls++
		if in.Question != question {
			t.Fatal(in.Question)
		}
		return buddy.Answer{Text: "Проверь типы колонок перед merge.", SourceIDs: []string{"pandas-doc"}}, nil
	})
	msg, err := s.Chat(context.Background(), p, "concrete-pandas-question", question)
	if err != nil || msg.Kind != "answer" || calls != 1 {
		t.Fatal(msg, err, calls)
	}
}

func TestProjectCodeComesFromAnAuthorizedSource(t *testing.T) {
	s, p, _ := world(t)
	for i := range s.Catalog.Documents {
		if s.Catalog.Documents[i].ID == "catalog-welcome" {
			s.Catalog.Documents[i].Body = "Тестовая команда. Проект Jira: DEMO2."
		}
	}
	calls := 0
	s.Model = answerFunc(func(context.Context, buddy.AnswerInput) (buddy.Answer, error) {
		calls++
		return buddy.Answer{Text: "Какой проект?", NeedsClarification: true}, nil
	})
	msg, err := s.Chat(context.Background(), p, "project-source-question", "Какой ключ проекта Jira у моей команды?")
	if err != nil || msg.Kind != "source" || !strings.Contains(msg.Text, "DEMO2") || len(msg.SourceIDs) != 1 || msg.SourceIDs[0] != "catalog-welcome" || calls != 0 {
		t.Fatal(msg, err, calls)
	}
	s.Authorizer = permissionFunc(func(_ context.Context, _ buddy.Profile, r buddy.Resource, action string) error {
		if action == "read" && r.ID == "catalog-welcome" {
			return buddy.ErrForbidden
		}
		return nil
	})
	denied, err := s.Chat(context.Background(), p, "denied-project-question", "Какой ключ проекта Jira у моей команды?")
	if err != nil || strings.Contains(denied.Text, "DEMO2") {
		t.Fatal("source lookup bypassed resource permissions", denied, err)
	}
}

func TestNamedPublicSourceExcerptKeepsItsProvenance(t *testing.T) {
	s, p, _ := world(t)
	s.Model = answerFunc(func(context.Context, buddy.AnswerInput) (buddy.Answer, error) {
		t.Fatal("source excerpt pretended to need inference")
		return buddy.Answer{}, nil
	})
	msg, err := s.Chat(context.Background(), p, "playbook-source-question", "Чем полезен публичный Playbook при разработке?")
	if err != nil || msg.Kind != "source" || !strings.Contains(msg.Text, "проверять изменения автоматически") || !strings.Contains(msg.Text, "исторический") || len(msg.SourceIDs) != 1 || msg.SourceIDs[0] != "engineering" {
		t.Fatal(msg, err)
	}
	for i := range s.Catalog.Documents {
		if s.Catalog.Documents[i].ID == "engineering" {
			s.Catalog.Documents[i].Body = "Новый снимок с другими сведениями"
		}
	}
	replay, err := s.Chat(context.Background(), p, "playbook-source-question", "Чем полезен публичный Playbook при разработке?")
	if err != nil || replay.Kind != "restricted" || strings.Contains(replay.Text, "проверять изменения автоматически") {
		t.Fatal("changed source left a stale quote visible", replay, err)
	}
}

func TestMissingInformationCannotBecomeAnUnsupportedResourceClaim(t *testing.T) {
	s, p, _ := world(t)
	s.Model = answerFunc(func(_ context.Context, in buddy.AnswerInput) (buddy.Answer, error) {
		return buddy.Answer{Text: "В документах нет информации, потому что такого сервиса у команды нет.", SourceIDs: []string{in.Documents[0].ID}}, nil
	})
	msg, err := s.Chat(context.Background(), p, "unknown-version-question", "Какая версия Kubernetes у команды?")
	if err != nil || msg.Kind != "clarification" || strings.Contains(msg.Text, "такого сервиса") || !strings.Contains(msg.Text, "?") {
		t.Fatal(msg, err)
	}
	s.Model = answerFunc(func(context.Context, buddy.AnswerInput) (buddy.Answer, error) {
		return buddy.Answer{Text: "Нет информации", SourceIDs: []string{"catalog-mentor-private"}}, nil
	})
	if _, err := s.Chat(context.Background(), p, "bad-source-guard-question", "Какая версия Kubernetes у команды?"); !errors.Is(err, buddy.ErrUnavailable) {
		t.Fatal("guard accepted a forbidden citation", err)
	}
}
