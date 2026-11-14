package buddy_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Astarino/ai-buddy/internal/buddy"
)

func TestFiveInternsHaveSeparateRequests(t *testing.T) {
	s, anton, irina := world(t)
	ctx := context.Background()
	dasha, _ := s.Catalog.Profile("dasha")
	ids := []string{"anton", "lera", "nikita", "sofia", "timur"}
	for i, id := range ids {
		p, _ := s.Catalog.Profile(id)
		r, err := s.PrepareRequest(ctx, p, fmt.Sprintf("question-%d", i), "mentor", "Не запускаются тесты", "Проверил команду и папку. Подскажешь следующий шаг?")
		if err != nil || r.Status != "draft" || r.AssigneeID != dasha.ID {
			t.Fatal(r, err)
		}
		inbox, err := s.ListRequests(ctx, dasha)
		if err != nil || len(inbox) != i {
			t.Fatal(inbox, err)
		}
		r, err = s.ChangeRequest(ctx, p, r.ID, "send", r.Version)
		if err != nil || r.Status != "waiting" {
			t.Fatal(r, err)
		}
		again, err := s.ChangeRequest(ctx, p, r.ID, "send", 1)
		if err != nil || again.Version != r.Version {
			t.Fatal(again, err)
		}
	}
	inbox, err := s.ListRequests(ctx, dasha)
	if err != nil || len(inbox) != 5 {
		t.Fatal(inbox, err)
	}
	if len(inbox[0].Replies) != 0 {
		t.Fatal(inbox)
	}
	for _, r := range inbox {
		if _, err = s.GetRequest(ctx, irina, r.ID); !errors.Is(err, buddy.ErrNotFound) {
			t.Fatal(err)
		}
	}
	r := inbox[0]
	r, err = s.ReplyRequest(ctx, dasha, r.ID, "reply-one", "Открой папку с go.mod и повтори go test ./...", r.Version)
	if err != nil || r.Status != "answered" || r.Replies[0].Author != "Даша Волкова" {
		t.Fatal(r, err)
	}
	if _, err = s.ReplyRequest(ctx, dasha, r.ID, "reply-one", "Другое сообщение", r.Version); !errors.Is(err, buddy.ErrConflict) {
		t.Fatal(err)
	}
	repeat, err := s.ReplyRequest(ctx, dasha, r.ID, "reply-one", r.Replies[0].Text, 1)
	if err != nil || len(repeat.Replies) != 1 {
		t.Fatal(repeat, err)
	}
	p, _ := s.Catalog.Profile(r.RequesterID)
	if _, err = s.ReplyRequest(ctx, p, r.ID, "stale-reply", "Применил совет", 1); !errors.Is(err, buddy.ErrConflict) {
		t.Fatal(err)
	}
	r, err = s.ReplyRequest(ctx, p, r.ID, "follow-up", "Тесты прошли, спасибо", r.Version)
	if err != nil || r.Status != "waiting" {
		t.Fatal(r, err)
	}
	r, err = s.ChangeRequest(ctx, p, r.ID, "resolve", r.Version)
	if err != nil || r.Status != "resolved" {
		t.Fatal(r, err)
	}
	if _, err = s.ReplyRequest(ctx, dasha, r.ID, "late-reply", "Ещё ответ", r.Version); !errors.Is(err, buddy.ErrConflict) {
		t.Fatal(err)
	}
	if _, err = s.ChangeRequest(ctx, dasha, inbox[1].ID, "resolve", inbox[1].Version); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err = s.PrepareRequest(ctx, anton, "another-problem", "mentor", "Другая проблема", "Не понимаю критерии первой задачи"); err != nil {
		t.Fatal(err)
	}
	view, err := s.View(ctx, dasha)
	if err != nil || len(view.Tasks) != 0 || len(view.Requests) != 5 {
		t.Fatal(view, err)
	}
	msg, err := s.Chat(ctx, dasha, "help-overview", "Кому нужна помощь из моих стажёров?")
	if err != nil || !strings.Contains(msg.Text, "4") {
		t.Fatal(msg, err)
	}
}

func TestRequestIsolationReassignmentAndRetries(t *testing.T) {
	s, p, other := world(t)
	ctx := context.Background()
	mentor, _ := s.Catalog.Profile(p.MentorID)
	r, err := s.PrepareRequest(ctx, p, "stable-question", "mentor", "Проверка прав", "Достаточный контекст вопроса")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Repo.(buddy.AuditStore).AuditEvents(ctx, p.ID, 200)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.PrepareRequest(ctx, p, "stable-question", "mentor", r.Subject, r.Body); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	after, _ := s.Repo.(buddy.AuditStore).AuditEvents(ctx, p.ID, 200)
	if len(after) != len(before) {
		t.Fatal("duplicate audit on retry", len(before), len(after))
	}
	if foreign, createErr := s.PrepareRequest(ctx, other, "stable-question", "mentor", r.Subject, r.Body); createErr != nil || foreign.ID == r.ID || foreign.RequesterID != other.ID {
		t.Fatal("retry key was not scoped to its owner", foreign, createErr)
	}
	if _, err = s.PrepareRequest(ctx, p, "stable-question", "mentor", r.Subject, "Изменённые параметры"); !errors.Is(err, buddy.ErrConflict) {
		t.Fatal(err)
	}
	if _, err = s.GetRequest(ctx, mentor, r.ID); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal("draft leaked", err)
	}
	r, err = s.ChangeRequest(ctx, p, r.ID, "send", r.Version)
	if err != nil {
		t.Fatal(err)
	}
	s.Catalog.Profiles[0].MentorID = "mikhail"
	p = s.Catalog.Profiles[0]
	if _, err = s.GetRequest(ctx, mentor, r.ID); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal("old mentor retained access", err)
	}
	rows, err := s.ListRequests(ctx, mentor)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	r, err = s.ChangeRequest(ctx, p, r.ID, "reassign", r.Version, "mikhail")
	if err != nil || r.AssigneeID != "mikhail" {
		t.Fatal(r, err)
	}
	newMentor, _ := s.Catalog.Profile("mikhail")
	if _, err = s.GetRequest(ctx, newMentor, r.ID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ kind, subject, body string }{
		{"mentor", "ok", "password=long-secret-value"}, {"admin", "Неверный тип", "Текст вопроса"}, {"access", "Обход подтверждения", "Текст вопроса"}, {"mentor", "x", "text"},
	} {
		if _, err = s.PrepareRequest(ctx, p, buddy.NewID(), tc.kind, tc.subject, tc.body); err == nil {
			t.Fatal(tc)
		}
	}
	if _, err = s.ReplyRequest(ctx, p, r.ID, "secret-reply", "token=long-secret-value", r.Version); !errors.Is(err, buddy.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err = s.ChangeRequest(ctx, p, r.ID, "unknown", r.Version); !errors.Is(err, buddy.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err = s.GetRequest(ctx, p, "unknown"); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestResourceOwnerInboxUsesCorporateResult(t *testing.T) {
	s, p, _ := world(t)
	ctx := context.Background()
	owner, _ := s.Catalog.Profile("wiki-owner")
	mentor, _ := s.Catalog.Profile(p.MentorID)
	a, err := s.Prepare(ctx, p, "access", "Изучить руководство команды")
	if err != nil {
		t.Fatal(err)
	}
	op, err := s.Confirm(ctx, p, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := "access-" + op.Key
	r, err := s.GetRequest(ctx, owner, id)
	if err != nil || r.OperationStatus != "pending" {
		t.Fatal(r, err)
	}
	if _, err = s.DecideRequest(ctx, mentor, id, "approved", r.Version); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = s.DecideRequest(ctx, p, id, "approved", r.Version); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err = s.DecideRequest(ctx, owner, id, "granted", r.Version); !errors.Is(err, buddy.ErrConflict) {
		t.Fatal(err)
	}
	r, err = s.DecideRequest(ctx, owner, id, "approved", r.Version)
	if err != nil || r.OperationStatus != "approved" {
		t.Fatal(r, err)
	}
	if _, err = s.Document(ctx, p, "catalog-guide"); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal("approval granted read", err)
	}
	r, err = s.DecideRequest(ctx, owner, id, "granted", r.Version)
	if err != nil || r.OperationStatus != "granted" || r.Status != "resolved" {
		t.Fatal(r, err)
	}
	if _, err = s.Document(ctx, p, "catalog-guide"); err != nil {
		t.Fatal(err)
	}
	r, err = s.DecideRequest(ctx, owner, id, "granted", r.Version-1)
	if err != nil {
		t.Fatal("repeated service result", r, err)
	}
	r, err = s.DecideRequest(ctx, owner, id, "revoked", r.Version)
	if err != nil || r.OperationStatus != "revoked" {
		t.Fatal(r, err)
	}
	if _, err = s.Document(ctx, p, "catalog-guide"); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRequestAndPublicSearchResourcePermissions(t *testing.T) {
	s, p, _ := world(t)
	ctx := context.Background()
	mentor, _ := s.Catalog.Profile(p.MentorID)
	s.Demo = false
	denied := ""
	s.Authorizer = permissionFunc(func(_ context.Context, actor buddy.Profile, r buddy.Resource, action string) error {
		if actor.ID == "" || r.ID == "" {
			t.Fatal("unbound authorization", actor, r)
		}
		if r.Kind == denied {
			return buddy.ErrForbidden
		}
		return nil
	})
	denied = "person"
	if _, err := s.PrepareRequest(ctx, p, "denied-request", "mentor", "Нужна помощь", "Контекст задачи"); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	denied = ""
	r, err := s.PrepareRequest(ctx, p, "allowed-request", "mentor", "Нужна помощь", "Контекст задачи")
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.ChangeRequest(ctx, p, r.ID, "send", r.Version)
	if err != nil {
		t.Fatal(err)
	}
	denied = "request"
	if _, err = s.GetRequest(ctx, mentor, r.ID); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	rows, err := s.ListRequests(ctx, mentor)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	if _, err = s.ReplyRequest(ctx, mentor, r.ID, "denied-reply", "Текст ответа", r.Version); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	denied = "tool"
	s.Searcher = publicSearchFunc(func(context.Context, string, string) ([]buddy.Document, error) {
		return []buddy.Document{{Title: "Public", Body: "module root go.mod", URL: "https://go.dev/ref/mod", Scope: "public", DataClass: "public", Version: "v1"}}, nil
	})
	if _, err = s.SearchPublic(ctx, p, "denied-search", "go", "go.mod module"); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	denied = ""
	run, err := s.SearchPublic(ctx, p, "allowed-search", "go", "go.mod module")
	if err != nil {
		t.Fatal(err)
	}
	denied = "search"
	if _, err = s.Document(ctx, p, run.Documents[0].ID); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal("revoked snapshot visible", err)
	}
	view, err := s.View(ctx, p)
	if err != nil || !view.ServiceFresh {
		t.Fatal("denial was reported as an outage", view, err)
	}
	s.Authorizer = nil
	if _, err = s.Document(ctx, p, run.Documents[0].ID); !errors.Is(err, buddy.ErrNotFound) {
		t.Fatal("snapshot bypassed missing authority", err)
	}
	if _, err = s.SearchPublic(ctx, p, "missing-authority", "go", "module root"); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err = s.PrepareRequest(ctx, p, "missing-authority", "mentor", "Нужна помощь", "Контекст задачи"); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
	s.Demo = true
	s.Catalog.Profiles[0].MentorID = ""
	if _, err = s.PrepareRequest(ctx, s.Catalog.Profiles[0], "missing-recipient", "mentor", "Нужна помощь", "Контекст задачи"); !errors.Is(err, buddy.ErrForbidden) {
		t.Fatal(err)
	}
}

type gatedDecision struct {
	buddy.Corporate
	started, release chan struct{}
}

func (g *gatedDecision) Decide(ctx context.Context, actor, owner, key, status string) error {
	close(g.started)
	<-g.release
	return g.Corporate.(buddy.ResourceDecider).Decide(ctx, actor, owner, key, status)
}

func TestAccessDecisionClaimsOneIntentBeforeNetworkCall(t *testing.T) {
	s, p, _ := world(t)
	ctx := context.Background()
	owner, _ := s.Catalog.Profile("wiki-owner")
	a, err := s.Prepare(ctx, p, "access", "Изучить руководство команды")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Confirm(ctx, p, a.ID); err != nil {
		t.Fatal(err)
	}
	r, err := s.GetRequest(ctx, owner, "access-"+a.ID)
	if err != nil {
		t.Fatal(err)
	}
	g := &gatedDecision{s.Corporate, make(chan struct{}), make(chan struct{})}
	s.Corporate = g
	finished := make(chan error, 1)
	go func() { _, err := s.DecideRequest(ctx, owner, r.ID, "approved", r.Version); finished <- err }()
	<-g.started
	if _, err = s.DecideRequest(ctx, owner, r.ID, "rejected", r.Version); !errors.Is(err, buddy.ErrConflict) {
		t.Error("different decision used same confirmation", err)
	}
	if _, err = s.ReplyRequest(ctx, owner, r.ID, "concurrent-reply", "Текст ответа", r.Version); !errors.Is(err, buddy.ErrConflict) {
		t.Error(err)
	}
	close(g.release)
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
	r, err = s.GetRequest(ctx, owner, r.ID)
	if err != nil || r.OperationStatus != "approved" || r.PendingDecision != "" {
		t.Fatal(r, err)
	}
}
