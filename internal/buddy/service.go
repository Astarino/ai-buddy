package buddy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type Service struct {
	Repo            Repository
	Corporate       Corporate
	Model           Model
	Catalog         Catalog
	Now             func() time.Time
	Authorizer      Authorizer
	Operator        DemoOperator
	Demo            bool
	ModelDataPolicy string
	Searcher        PublicSearcher
	Requests        RequestStore
}

func NewService(repo Repository, corporate Corporate, model Model) *Service {
	operator, _ := corporate.(DemoOperator)
	return &Service{Repo: repo, Corporate: corporate, Model: model, Catalog: LoadCatalog(), Now: time.Now, Operator: operator, Demo: true}
}

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (s *Service) operations(ctx context.Context, p Profile) ([]Operation, bool) {
	ops, err := s.Corporate.List(ctx, p.ID)
	if err != nil {
		return []Operation{}, false
	}
	filtered := []Operation{}
	for _, op := range ops {
		if op.UserID == p.ID && ((op.Kind == "access" && op.Resource == p.Space) || (op.Kind == "issue" && op.Resource == p.Project+"-101")) {
			filtered = append(filtered, op)
		}
	}
	return filtered, true
}

func (s *Service) View(ctx context.Context, p Profile) (View, error) {
	state, err := s.Repo.Read(ctx, p.ID)
	if err != nil {
		return View{}, err
	}
	ops, fresh := s.operations(ctx, p)
	reconciled := false
	for _, action := range state.Actions {
		if action.Status == "completed" {
			continue
		}
		for _, op := range ops {
			if op.Key != action.ID {
				continue
			}
			if !matchesOperation(p.ID, action, op) {
				return View{}, ErrUnavailable
			}
			if err = s.recordResult(ctx, p, action, op); err != nil {
				return View{}, err
			}
			reconciled = true
		}
	}
	if reconciled {
		state, err = s.Repo.Read(ctx, p.ID)
		if err != nil {
			return View{}, err
		}
	}
	for _, op := range ops {
		if err := s.ensureAccessRequest(ctx, p, op); err != nil {
			return View{}, err
		}
	}
	allowed, permissionsFresh := s.allowedDocuments(ctx, p, ops)
	docs := slices.Clone(allowed)
	for i := range docs {
		docs[i].Body = ""
		docs[i].Keywords = ""
	}
	messages := slices.Clone(state.Messages)
	for i := range messages {
		messages[i] = visibleMessage(allowed, messages[i])
	}
	tasks, next := s.Catalog.Route(p, state, ops)
	if audit, ok := s.Repo.(AuditStore); ok {
		if err := audit.AppendAudit(ctx, Event{OwnerID: p.ID, Type: "state_read", ObjectID: p.ID}); err != nil {
			return View{}, ErrUnavailable
		}
	}
	requests, err := s.ListRequests(ctx, p)
	if err != nil {
		requests = []HelpRequest{}
		permissionsFresh = false
	}
	if p.Audience == "responsible" {
		tasks, next = []Task{}, ""
	}
	return View{Profile: p, Messages: messages, Actions: state.Actions, Tasks: tasks, Operations: ops, Documents: docs, NextTaskID: next, Draft: state.Draft, ServiceFresh: fresh && permissionsFresh, Model: s.Model.Name(), Demo: s.Demo, Requests: requests, RequestTargets: s.requestTargets(ctx, p)}, nil
}

func (s *Service) Document(ctx context.Context, p Profile, id string) (Document, error) {
	ops, _ := s.operations(ctx, p)
	allowed, _ := s.allowedDocuments(ctx, p, ops)
	index := slices.IndexFunc(allowed, func(d Document) bool { return d.ID == id })
	if index < 0 {
		return Document{}, ErrNotFound
	}
	doc := allowed[index]
	_, err := s.Repo.Update(ctx, p.ID, func(st *State) error {
		st.Events = append(st.Events, Event{Type: "source_opened", ObjectID: id, At: s.Now()})
		return nil
	})
	return doc, err
}

func (s *Service) Prepare(ctx context.Context, p Profile, kind, reason string) (Action, error) {
	if err := s.authorizeAction(ctx, p, kind); err != nil {
		return Action{}, err
	}
	reason = strings.TrimSpace(reason)
	if credentialRequest(reason) {
		return Action{}, ErrForbidden
	}
	if utf8.RuneCountInString(reason) < 8 || utf8.RuneCountInString(reason) > 300 {
		return Action{}, ErrInvalid
	}
	ops, fresh := s.operations(ctx, p)
	if !fresh {
		return Action{}, ErrUnavailable
	}
	state, err := s.Repo.Read(ctx, p.ID)
	if err != nil {
		return Action{}, err
	}
	a := Action{ID: NewID(), Kind: kind, Reason: reason, Status: "ready", ExpiresAt: s.Now().Add(15 * time.Minute)}
	switch kind {
	case "access":
		if HasAccess(ops) {
			return Action{}, ErrConflict
		}
		for _, op := range ops {
			if op.Kind == "access" && (op.Status == "pending" || op.Status == "approved") {
				return Action{}, ErrConflict
			}
		}
		a.Resource, a.Level = p.Space, "Чтение"
	case "issue":
		if !HasAccess(ops) || !state.Completed["read-guide"] {
			return Action{}, ErrForbidden
		}
		allowed, _ := s.allowedDocuments(ctx, p, ops)
		if !slices.ContainsFunc(allowed, func(d Document) bool { return d.ID == p.Team+"-guide" }) {
			return Action{}, ErrForbidden
		}
		for _, op := range ops {
			if op.Kind == "issue" {
				return Action{}, ErrConflict
			}
		}
		a.Resource, a.Level = p.Project+"-101", "Назначить на себя"
	default:
		return Action{}, ErrInvalid
	}
	_, err = s.Repo.Update(ctx, p.ID, func(st *State) error {
		for i, old := range st.Actions {
			if old.Kind == kind && old.Status == "ready" && old.ExpiresAt.After(s.Now()) {
				if old.Resource == a.Resource && old.Level == a.Level && old.Reason == a.Reason {
					a = old
					return nil
				}
				st.Actions[i].Status = "superseded"
			}
		}
		st.Actions = append(st.Actions, a)
		st.Events = append(st.Events, Event{Type: "action_prepared", ObjectID: a.ID, At: s.Now()})
		return nil
	})
	return a, err
}

func (s *Service) Confirm(ctx context.Context, p Profile, id string) (Operation, error) {
	state, err := s.Repo.Read(ctx, p.ID)
	if err != nil {
		return Operation{}, err
	}
	var action *Action
	for i := range state.Actions {
		if state.Actions[i].ID == id {
			a := state.Actions[i]
			action = &a
			break
		}
	}
	if action == nil {
		return Operation{}, ErrNotFound
	}
	if !ValidActionTarget(p, *action) {
		return Operation{}, ErrForbidden
	}
	ops, fresh := s.operations(ctx, p)
	if !fresh {
		return Operation{}, ErrUnavailable
	}
	// Reconcile before expiry checks: an earlier request may have succeeded without a response.
	for _, op := range ops {
		if op.Key == action.ID {
			if !matchesOperation(p.ID, *action, op) {
				return Operation{}, ErrUnavailable
			}
			if action.Status == "completed" {
				if audit, ok := s.Repo.(AuditStore); ok {
					if err := audit.AppendAudit(ctx, Event{OwnerID: p.ID, Type: "action_replayed", ObjectID: action.ID, Outcome: op.Status}); err != nil {
						return Operation{}, ErrUnavailable
					}
				}
			}
			return op, s.recordResult(ctx, p, *action, op)
		}
	}
	if action.Status != "ready" {
		return Operation{}, ErrConflict
	}
	if err := s.authorizeAction(ctx, p, action.Kind); err != nil {
		return Operation{}, err
	}
	if !action.ExpiresAt.After(s.Now()) {
		return Operation{}, ErrExpired
	}
	if action.Kind == "issue" && (!HasAccess(ops) || !state.Completed["read-guide"]) {
		return Operation{}, ErrForbidden
	}
	if action.Kind == "issue" {
		allowed, _ := s.allowedDocuments(ctx, p, ops)
		if !slices.ContainsFunc(allowed, func(d Document) bool { return d.ID == p.Team+"-guide" }) {
			return Operation{}, ErrForbidden
		}
	}
	if action.Kind == "access" {
		for _, op := range ops {
			if op.Kind == "access" && op.Status != "rejected" && op.Status != "revoked" {
				return Operation{}, ErrConflict
			}
		}
	}
	op, err := s.Corporate.Execute(ctx, p.ID, *action)
	if err != nil {
		return Operation{}, err
	}
	if !matchesOperation(p.ID, *action, op) {
		return Operation{}, ErrUnavailable
	}
	return op, s.recordResult(ctx, p, *action, op)
}

func matchesOperation(owner string, action Action, op Operation) bool {
	validStatus := action.Kind == "access" && slices.Contains([]string{"pending", "approved", "granted", "rejected", "revoked"}, op.Status) || action.Kind == "issue" && slices.Contains([]string{"assigned", "done"}, op.Status)
	return validStatus && op.ID != "" && op.UserID == owner && op.Key == action.ID && op.Kind == action.Kind && op.Resource == action.Resource && op.Level == action.Level && op.Reason == action.Reason
}

func (s *Service) recordResult(ctx context.Context, p Profile, a Action, op Operation) error {
	_, err := s.Repo.Update(ctx, p.ID, func(st *State) error {
		for i := range st.Actions {
			if st.Actions[i].ID == a.ID {
				if st.Actions[i].Status == "completed" {
					return nil
				}
				st.Actions[i].Status, st.Actions[i].ResultID = "completed", op.ID
			}
		}
		text := fmt.Sprintf("Заявка %s создана. Пространство %s, права: чтение. Теперь её должен согласовать владелец ресурса. Доступ пока не выдан. Пока ждёшь, можно изучить принципы разработки.", op.ID, op.Resource)
		if op.Kind == "access" && op.Status != "pending" {
			status := map[string]string{"approved": "согласовано, ожидает выдачи", "granted": "доступ выдан", "rejected": "отклонено", "revoked": "доступ отозван"}[op.Status]
			text = fmt.Sprintf("Сервис подтвердил результат %s для пространства %s: %s. %s", op.ID, op.Resource, status, op.Note)
		}
		if op.Kind == "issue" {
			text = fmt.Sprintf("Задача %s назначена на тебя в тестовой Jira. Начни с критериев готовности в руководстве; результат и ревью подтвердит наставник.", op.ID)
			if op.Status == "done" {
				text = fmt.Sprintf("Тестовая Jira подтвердила завершение задачи %s и ревью.", op.ID)
			}
		}
		st.Messages = append(st.Messages, Message{ID: NewID(), Role: "assistant", Text: text, CreatedAt: s.Now(), SourceIDs: []string{}, Kind: "operation"})
		st.Events = append(st.Events, Event{Type: "action_completed", ObjectID: op.ID, At: s.Now()})
		return nil
	})
	if err != nil {
		return err
	}
	return s.ensureAccessRequest(ctx, p, op)
}

func (s *Service) Complete(ctx context.Context, p Profile, taskID string) error {
	ops, _ := s.operations(ctx, p)
	allowed, _ := s.allowedDocuments(ctx, p, ops)
	_, err := s.Repo.Update(ctx, p.ID, func(st *State) error {
		tasks, _ := s.Catalog.Route(p, *st, ops)
		for _, task := range tasks {
			if task.ID != taskID {
				continue
			}
			if task.Completion != "self" || (task.Status != "available" && task.Status != "completed") {
				return ErrForbidden
			}
			if task.DocumentID != "" && !slices.ContainsFunc(allowed, func(d Document) bool { return d.ID == task.DocumentID }) {
				return ErrForbidden
			}
			if !st.Completed[taskID] {
				st.Completed[taskID] = true
				st.Events = append(st.Events, Event{Type: "task_completed", ObjectID: taskID, At: s.Now()})
			}
			return nil
		}
		return ErrNotFound
	})
	return err
}

func (s *Service) Draft(ctx context.Context, p Profile, question string) (string, error) {
	if utf8.RuneCountInString(question) > 2000 {
		return "", ErrInvalid
	}
	state, err := s.Repo.Read(ctx, p.ID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(question) == "" {
		for i := len(state.Messages) - 1; i >= 0; i-- {
			if state.Messages[i].Role == "user" {
				question = state.Messages[i].Text
				break
			}
		}
	}
	if strings.TrimSpace(question) == "" {
		question = "Хочу уточнить следующий шаг адаптации и критерии первой задачи."
	}
	if credentialRequest(question) {
		return "", ErrForbidden
	}
	draft := fmt.Sprintf("%s, привет! Я прохожу адаптацию в команде «%s».\n\nВопрос: %s\n\nПосмотрел доступные материалы в AI Buddy, но полного ответа не нашёл. Поможешь уточнить, что проверить дальше?", p.Mentor, p.TeamName, question)
	_, err = s.Repo.Update(ctx, p.ID, func(st *State) error {
		st.Draft = draft
		st.Events = append(st.Events, Event{Type: "mentor_draft", ObjectID: p.ID, At: s.Now()})
		return nil
	})
	return draft, err
}

func (s *Service) Chat(ctx context.Context, p Profile, id, question string) (Message, error) {
	return s.ChatWithConsent(ctx, p, id, question, "")
}

func (s *Service) ChatWithConsent(ctx context.Context, p Profile, id, question, consentTo string) (Message, error) {
	question = strings.TrimSpace(question)
	if len(id) < 8 || len(id) > 80 || question == "" || utf8.RuneCountInString(question) > 2000 {
		return Message{}, ErrInvalid
	}
	state, err := s.Repo.Update(ctx, p.ID, func(st *State) error {
		for i, old := range st.Messages {
			if old.Role == "user" && old.RequestHash == "" && credentialRequest(old.Text) {
				st.Messages[i].RequestHash = fingerprint(old.Text)
				st.Messages[i].Text, st.Messages[i].Blocked = secretPlaceholder, true
			}
		}
		for i, m := range st.Messages {
			if m.ID == id {
				if (m.RequestHash != "" && m.RequestHash != fingerprint(question)) || (m.RequestHash == "" && m.Text != question) {
					return ErrConflict
				}
				if consentTo != "" && consentTo == s.Model.Boundary().Token {
					st.Messages[i].ConsentTo = consentTo
				}
				return nil
			}
		}
		if len(st.Messages) >= 200 {
			return ErrConflict
		}
		text := question
		if credentialRequest(question) {
			text = secretPlaceholder
		}
		approved := ""
		if consentTo != "" && consentTo == s.Model.Boundary().Token {
			approved = consentTo
		}
		st.Messages = append(st.Messages, Message{ID: id, Role: "user", Text: text, CreatedAt: s.Now(), SourceIDs: []string{}, RequestHash: fingerprint(question), Blocked: credentialRequest(question) || policyRequest(question), ConsentTo: approved})
		return nil
	})
	if err != nil {
		return Message{}, err
	}
	for _, m := range state.Messages {
		if m.ReplyTo == id {
			ops, _ := s.operations(ctx, p)
			allowed, _ := s.allowedDocuments(ctx, p, ops)
			return visibleMessage(allowed, m), nil
		}
	}
	start := s.Now()
	guarded := false
	msg := Message{ID: NewID(), Role: "assistant", CreatedAt: s.Now(), ReplyTo: id, SourceIDs: []string{}, Kind: "workflow", ContextVersion: contextVersion}
	lower := strings.ToLower(question)
	switch {
	case p.Audience == "responsible" && containsAny(lower, "кому нужна", "мои стаж", "кто ждёт", "ждет ответа", "ждёт ответа"):
		requests, err := s.ListRequests(ctx, p)
		if err != nil {
			return Message{}, err
		}
		count := 0
		for _, r := range requests {
			if r.AssigneeID == p.ID && r.Status == "waiting" {
				count++
			}
		}
		msg.Text = fmt.Sprintf("Твоего ответа ждут запросы: %d. Открой «Нужен мой ответ» — там они разделены по стажёрам.", count)
	case containsAny(lower, "не запускаются тесты", "не могу запустить тесты") && !containsAny(lower, "go.mod", "pattern", "go test", "ошибка:", "пишет"):
		msg.Text = "Какую команду запускаешь и что пишет в ответ?"
		msg.Kind = "clarification"
	case credentialRequest(question):
		msg.Text = "Не присылай пароли и токены в чат. Как получить доступ, подскажет владелец сервиса; могу подготовить ему вопрос."
		msg.Kind = "policy"
	case policyRequest(question):
		msg.Text = "На это у меня нет прав. Могу помочь подготовить запрос владельцу ресурса."
		msg.Kind = "policy"
	case conflictingSources(lower):
		msg.Text = "Какие два документа сравниваешь?"
		msg.Kind = "clarification"
	case vagueProblem(lower):
		msg.Text = "В каком инструменте появилась ошибка?"
		if strings.Contains(lower, "настро") {
			msg.Text = "Какую настройку имеешь в виду?"
		}
		msg.Kind = "clarification"
	case containsAny(lower, "не открывается", "нет доступа", "запросить доступ", "403", "нужен доступ", "доступ к вики"):
		if !containsAny(lower, "вики", "confluence", "пространств", "руководств") || containsAny(lower, "github", "gitlab", "jira", "sentry", "grafana") {
			msg.Text = "Что именно не открывается: какой сервис или страница?"
			for _, service := range []string{"GitHub", "GitLab", "Jira", "Sentry", "Grafana"} {
				if strings.Contains(lower, strings.ToLower(service)) {
					msg.Text = "Что пишет " + service + " при открытии страницы?"
					if strings.Contains(lower, "403") {
						msg.Text = "На какой странице " + service + " появляется 403?"
					}
					break
				}
			}
			msg.Kind = "clarification"
			break
		}
		a, prepareErr := s.Prepare(ctx, p, "access", "Изучить руководство команды и подготовиться к первой задаче")
		if prepareErr == nil {
			msg.ActionID = a.ID
			msg.Text = "К вики нужен доступ. Подготовил запрос — проверь и отправь."
			msg.SourceIDs = []string{"access-policy"}
		} else if prepareErr == ErrConflict {
			msg.Text = "Запрос уже есть или доступ выдан. Проверь состояние в «Моих заявках»."
		} else {
			return Message{}, prepareErr
		}
	case containsAny(lower, "взять первую", "дай первую задачу", "назначь задачу"):
		a, prepareErr := s.Prepare(ctx, p, "issue", "Первая учебная задача после изучения руководства команды")
		if prepareErr == ErrForbidden {
			msg.Text = "Сначала получи доступ к вики и изучи руководство. Затем можно взять задачу."
		} else if prepareErr == ErrConflict {
			msg.Text = "Первая задача уже назначена. Её ключ и состояние есть в «Моих заявках»."
		} else if prepareErr != nil {
			return Message{}, prepareErr
		} else {
			msg.ActionID = a.ID
			msg.Text = "Подготовил первую задачу. Проверь карточку и подтверди назначение."
		}
	case containsAny(lower, "мой план", "мой маршрут", "что дальше", "что делать дальше", "с чего начать", "план адаптации"):
		ops, _ := s.operations(ctx, p)
		tasks, next := s.Catalog.Route(p, state, ops)
		msg.Text = "Маршрут первых двух недель открыт справа."
		for _, task := range tasks {
			if task.ID == next {
				msg.Text += " Ближайший доступный шаг: «" + task.Title + "». " + task.Description
			}
		}
		msg.SourceIDs = []string{"checklist"}
	default:
		ops, _ := s.operations(ctx, p)
		allowed, _ := s.allowedDocuments(ctx, p, ops)
		docs := conversationDocuments(allowed, state.Messages, id, question)
		for i := len(state.Searches) - 1; i >= 0; i-- {
			if state.Searches[i].Query != question {
				continue
			}
			docs = []Document{}
			for _, snapshot := range state.Searches[i].Documents {
				for _, doc := range allowed {
					if doc.ID == snapshot.ID {
						docs = append(docs, doc)
					}
				}
			}
			break
		}
		if len(docs) == 0 {
			msg.Text = "В доступных материалах нет надёжного ответа. Уточни, с какой задачей или инструментом связан вопрос и что уже получилось проверить. Можно подготовить вопрос наставнику."
			msg.Kind = "clarification"
		} else {
			if reply, ok := sourceReply(p, lower, docs); ok {
				msg.Text, msg.SourceIDs, msg.Kind = reply.Text, reply.SourceIDs, "source"
				used := []Document{}
				for _, doc := range docs {
					if slices.Contains(reply.SourceIDs, doc.ID) {
						used = append(used, doc)
					}
				}
				msg.Dependencies = contextRefs(used, nil)
				break
			}
			history := modelHistory(allowed, state.Messages, id)
			boundary := s.Model.Boundary()
			if boundary.External {
				if s.ModelDataPolicy != "public-only" {
					return Message{}, ErrDataPolicy
				}
				if !(AnswerInput{Documents: docs}).PublicOnly() {
					return Message{}, ErrDataPolicy
				}
				approved := false
				for _, old := range state.Messages {
					if old.ID == id && old.ConsentTo == boundary.Token {
						approved = true
					}
				}
				if !approved {
					return Message{}, ErrConsentRequired
				}
				history = nil
			}
			msg.Kind = "answer"
			msg.Dependencies = contextRefs(docs, history)
			input, inputErr := (AnswerInput{Question: question, Team: p.TeamName, Day: p.Day, Documents: docs, History: history, Direction: p.Direction, Role: p.Role}).Sanitized()
			if inputErr != nil {
				return Message{}, inputErr
			}
			input.ApprovedExternal = boundary.External
			if boundary.External {
				input.Team, input.Role, input.Direction = "", "", ""
			}
			if audit, ok := s.Repo.(AuditStore); ok {
				kind := "model_requested"
				if boundary.External {
					kind = "external_model_requested"
				}
				if err := audit.AppendAudit(ctx, Event{OwnerID: p.ID, Type: kind, ObjectID: msg.ID, Outcome: "started"}); err != nil {
					return Message{}, ErrUnavailable
				}
			}
			answer, modelErr := s.Model.Answer(ctx, input)
			if modelErr != nil {
				return Message{}, modelErr
			}
			if strings.TrimSpace(answer.Text) == "" || utf8.RuneCountInString(answer.Text) > 3000 {
				return Message{}, ErrUnavailable
			}
			for _, source := range answer.SourceIDs {
				if !slices.ContainsFunc(docs, func(d Document) bool { return d.ID == source }) {
					return Message{}, ErrUnavailable
				}
			}
			if s.Model.Name() != "fixture" && !answer.NeedsClarification && missingInformation.MatchString(answer.Text) {
				answer.Text = "Для какой задачи это нужно? По этим источникам не могу надёжно ответить."
				answer.SourceIDs = []string{}
				answer.NeedsClarification = true
				guarded = true
			}
			if !answer.NeedsClarification && len(answer.SourceIDs) == 0 {
				return Message{}, ErrUnavailable
			}
			msg.Text, msg.SourceIDs = answer.Text, answer.SourceIDs
			if answer.NeedsClarification {
				msg.Kind = "clarification"
			}
			if s.Model.Name() == "fixture" {
				msg.Kind = "fixture"
			}
			// Recheck after inference so an access revocation during generation fails closed.
			latest, fresh := s.operations(ctx, p)
			if !fresh {
				latest = nil
			}
			latestAllowed, _ := s.allowedDocuments(ctx, p, latest)
			if !permitsMessage(latestAllowed, msg) {
				return Message{}, ErrForbidden
			}
		}
	}
	msg.DurationMS = s.Now().Sub(start).Milliseconds()
	_, err = s.Repo.Update(ctx, p.ID, func(st *State) error {
		for _, old := range st.Messages {
			if old.ReplyTo == id {
				msg = old
				return nil
			}
		}
		st.Messages = append(st.Messages, msg)
		if guarded {
			st.Events = append(st.Events, Event{Type: "answer_guarded", ObjectID: msg.ID, At: s.Now(), Outcome: "clarification"})
		}
		st.Events = append(st.Events, Event{Type: "answer_" + msg.Kind, ObjectID: msg.ID, At: s.Now(), DurationMS: msg.DurationMS})
		return nil
	})
	if err != nil {
		return Message{}, err
	}
	ops, _ := s.operations(ctx, p)
	allowed, _ := s.allowedDocuments(ctx, p, ops)
	return visibleMessage(allowed, msg), nil
}

func containsAny(s string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(s, value) {
			return true
		}
	}
	return false
}
