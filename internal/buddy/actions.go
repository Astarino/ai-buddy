package buddy

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

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
		if err := s.requireGuide(ctx, p, state, ops); err != nil {
			return Action{}, err
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
	index := slices.IndexFunc(state.Actions, func(a Action) bool { return a.ID == id })
	if index < 0 {
		return Operation{}, ErrNotFound
	}
	action := state.Actions[index]
	if !ValidActionTarget(p, action) {
		return Operation{}, ErrForbidden
	}
	ops, fresh := s.operations(ctx, p)
	if !fresh {
		return Operation{}, ErrUnavailable
	}
	// Reconcile before expiry checks: an earlier request may have succeeded without a response.
	for _, op := range ops {
		if op.Key == action.ID {
			if !matchesOperation(p.ID, action, op) {
				return Operation{}, ErrUnavailable
			}
			if action.Status == "completed" {
				if audit, ok := s.Repo.(AuditStore); ok {
					if err := audit.AppendAudit(ctx, Event{OwnerID: p.ID, Type: "action_replayed", ObjectID: action.ID, Outcome: op.Status}); err != nil {
						return Operation{}, ErrUnavailable
					}
				}
			}
			return op, s.recordResult(ctx, p, action, op)
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
	if action.Kind == "issue" {
		if err := s.requireGuide(ctx, p, state, ops); err != nil {
			return Operation{}, err
		}
	}
	if action.Kind == "access" {
		for _, op := range ops {
			if op.Kind == "access" && op.Status != "rejected" && op.Status != "revoked" {
				return Operation{}, ErrConflict
			}
		}
	}
	op, err := s.Corporate.Execute(ctx, p.ID, action)
	if err != nil {
		return Operation{}, err
	}
	if !matchesOperation(p.ID, action, op) {
		return Operation{}, ErrUnavailable
	}
	return op, s.recordResult(ctx, p, action, op)
}

func (s *Service) requireGuide(ctx context.Context, p Profile, state State, ops []Operation) error {
	if !HasAccess(ops) || !state.Completed["read-guide"] {
		return ErrForbidden
	}
	allowed, _ := s.allowedDocuments(ctx, p, ops)
	if !slices.ContainsFunc(allowed, func(d Document) bool { return d.ID == p.Team+"-guide" }) {
		return ErrForbidden
	}
	return nil
}

func matchesOperation(owner string, action Action, op Operation) bool {
	if op.ID == "" || op.UserID != owner || op.Key != action.ID || op.Kind != action.Kind ||
		op.Resource != action.Resource || op.Level != action.Level || op.Reason != action.Reason {
		return false
	}
	switch action.Kind {
	case "access":
		return slices.Contains([]string{"pending", "approved", "granted", "rejected", "revoked"}, op.Status)
	case "issue":
		return slices.Contains([]string{"assigned", "done"}, op.Status)
	default:
		return false
	}
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
