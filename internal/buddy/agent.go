package buddy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type AgentTurn struct {
	Role     string     `json:"role"`
	Content  string     `json:"content"`
	Calls    []ToolCall `json:"tool_calls,omitempty"`
	ToolName string     `json:"tool_name,omitempty"`
	CallID   string     `json:"tool_call_id,omitempty"`
}

type AgentModel interface {
	Next(context.Context, []AgentTurn) (AgentTurn, error)
}

func (s *Service) canRunAgent(question string) bool {
	_, ok := s.Model.(AgentModel)
	return ok && !s.Model.Boundary().External && !credentialRequest(question) && !policyRequest(question)
}

func decodeArguments(raw []byte, value any) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return ErrInvalid
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func (s *Service) runAgent(ctx context.Context, p Profile, state State, requestID, question string, msg Message) (Message, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	model := s.Model.(AgentModel)
	ops, fresh := s.operations(ctx, p)
	if !fresh {
		return Message{}, ErrUnavailable
	}
	allowed, _ := s.allowedDocuments(ctx, p, ops)
	history := modelHistory(allowed, state.Messages, requestID)
	safe, err := (AnswerInput{Question: question, History: history}).Sanitized()
	if err != nil {
		return Message{}, err
	}
	turns := []AgentTurn{}
	for _, m := range safe.History {
		turns = append(turns, AgentTurn{Role: m.Role, Content: m.Text})
	}
	turns = append(turns, AgentTurn{Role: "user", Content: question})
	used := map[string]Document{}
	aliases := map[string]string{}
	seen := map[string]bool{}
	count, readContext, formatRetried := 0, false, false
	msg.Kind = "agent"
	msg.Dependencies = contextRefs(nil, history)
	for step := 0; step < 6; step++ {
		turn, err := model.Next(ctx, turns)
		if err != nil {
			return Message{}, fmt.Errorf("%w: %w", ErrModelUnavailable, err)
		}
		turn.Role = "assistant"
		if len(turn.Calls) == 1 && turn.Calls[0].Function.Name == "finish" {
			turn.Content = string(turn.Calls[0].Function.Arguments)
			turn.Calls = nil
		}
		turns = append(turns, turn)
		if len(turn.Calls) == 0 {
			var answer Answer
			if decodeArguments([]byte(turn.Content), &answer) != nil {
				if len(used) == 0 && !readContext {
					return Message{}, ErrModelUnavailable
				}
				if !formatRetried {
					turns[len(turns)-1] = AgentTurn{Role: "user", Content: "Use a tool call: prepare_action/prepare_request for the requested action, otherwise finish. Do not output plain text."}
					formatRetried = true
					continue
				}
				// Do not display unvalidated prose. Reuse the same model's grounded
				// structured adapter, or authoritative service status, to recover.
				statusReply := false
				if readContext {
					for _, op := range ops {
						if op.Kind == "access" && (op.Status == "pending" || op.Status == "approved") {
							status := "ожидает согласования"
							if op.Status == "approved" {
								status = "согласована, ожидает выдачи доступа"
							}
							answer = Answer{Text: fmt.Sprintf("Заявка %s %s. Доступ пока не выдан. Пока ждёшь, можно изучить принципы разработки.", op.ID, status), SourceIDs: []string{}}
							statusReply = true
							msg.Kind = "agent_status"
							break
						}
					}
				}
				if !statusReply {
					documents := []Document{}
					for _, doc := range used {
						body := []rune(doc.Body)
						if len(body) > 450 {
							doc.Body = string(body[:450])
						}
						documents = append(documents, doc)
					}
					slices.SortFunc(documents, func(a, b Document) int { return strings.Compare(a.ID, b.ID) })
					answer, err = s.Model.Answer(ctx, AnswerInput{Question: question, Team: p.TeamName, Day: p.Day, Direction: p.Direction, Role: p.Role, Documents: documents})
					if err != nil {
						return Message{}, fmt.Errorf("%w: %w", ErrModelUnavailable, err)
					}
					for i, id := range answer.SourceIDs {
						alias := ""
						for a, original := range aliases {
							if original == id {
								alias = a
							}
						}
						if alias == "" {
							return Message{}, ErrModelUnavailable
						}
						answer.SourceIDs[i] = alias
					}
					msg.Kind = "agent_repaired"
				}
			}
			if strings.TrimSpace(answer.Text) == "" || utf8.RuneCountInString(answer.Text) > 3000 {
				return Message{}, ErrModelUnavailable
			}
			msg.Text = answer.Text
			for _, alias := range answer.SourceIDs {
				id, ok := aliases[alias]
				if !ok {
					return Message{}, ErrModelUnavailable
				}
				if !slices.Contains(msg.SourceIDs, id) {
					msg.SourceIDs = append(msg.SourceIDs, id)
				}
			}
			if len(msg.SourceIDs) == 0 && !answer.NeedsClarification && !readContext {
				return Message{}, ErrModelUnavailable
			}
			if answer.NeedsClarification {
				msg.Kind = "clarification"
			}
			if msg.ActionID != "" {
				msg.Text = "Подготовил действие. Проверь параметры в карточке и подтверди отправку."
			}
			if msg.RequestID != "" {
				msg.Text = "Подготовил вопрос ответственному. Проверь текст и получателя перед отправкой."
			}
			for _, doc := range used {
				msg.Dependencies = append(msg.Dependencies, contextRefs([]Document{doc}, nil)...)
			}
			latest, fresh := s.operations(ctx, p)
			if !fresh {
				return Message{}, ErrUnavailable
			}
			current, _ := s.allowedDocuments(ctx, p, latest)
			if !permitsMessage(current, msg) {
				return Message{}, ErrForbidden
			}
			return msg, nil
		}
		if len(turn.Calls) > 2 || count+len(turn.Calls) > 8 {
			return Message{}, ErrModelUnavailable
		}
		for _, call := range turn.Calls {
			count++
			var normalized any
			if json.Unmarshal(call.Function.Arguments, &normalized) != nil {
				return Message{}, ErrInvalid
			}
			key := call.Function.Name + ":" + fingerprint(normalized)
			if seen[key] {
				return Message{}, ErrModelUnavailable
			}
			seen[key] = true
			var result any
			var toolErr error
			switch call.Function.Name {
			case "get_context":
				if decodeArguments(call.Function.Arguments, &struct{}{}) != nil {
					return Message{}, ErrInvalid
				}
				ops, fresh = s.operations(ctx, p)
				if !fresh {
					return Message{}, ErrUnavailable
				}
				state, err = s.Repo.Read(ctx, p.ID)
				if err != nil {
					return Message{}, err
				}
				_, next := s.Catalog.Route(p, state, ops)
				readContext = true
				operations := []map[string]string{}
				for _, op := range ops {
					operations = append(operations, map[string]string{"id": op.ID, "kind": op.Kind, "status": op.Status, "resource": op.Resource})
				}
				available := []string{}
				activeAccess, activeIssue := false, false
				for _, op := range ops {
					if op.Kind == "access" && (op.Status == "pending" || op.Status == "approved" || op.Status == "granted") {
						activeAccess = true
					}
					if op.Kind == "issue" {
						activeIssue = true
					}
				}
				if !activeAccess && s.authorizeAction(ctx, p, "access") == nil {
					available = append(available, "access")
				}
				if !activeIssue && s.authorizeAction(ctx, p, "issue") == nil && s.requireGuide(ctx, p, state, ops) == nil {
					available = append(available, "issue")
				}
				result = map[string]any{"team": p.TeamName, "day": p.Day, "direction": p.Direction, "next": next, "operations": operations, "space": p.Space, "first_issue": p.Project + "-101", "guide_read": state.Completed["read-guide"], "has_access": HasAccess(ops), "available_actions": available}
			case "search_knowledge":
				var args struct {
					Query string `json:"query"`
				}
				if decodeArguments(call.Function.Arguments, &args) != nil || !validSharedText(args.Query, 2, 500) {
					return Message{}, ErrInvalid
				}
				ops, fresh = s.operations(ctx, p)
				if !fresh {
					return Message{}, ErrUnavailable
				}
				allowed, _ = s.allowedDocuments(ctx, p, ops)
				docs := Search(allowed, args.Query)
				msg.SearchMode = "baseline"
				if s.Retrieval != nil {
					docs, msg.SearchMode = s.Retrieval.Search(ctx, allowed, args.Query, 3, "hybrid")
				}
				sources := []map[string]any{}
				for _, doc := range docs {
					if len(sources) == 2 {
						break
					}
					if _, known := used[doc.ID]; !known && len(used) >= 3 {
						continue
					}
					if containsCredentialValue(doc.Title + " " + doc.Body) {
						return Message{}, ErrForbidden
					}
					body := []rune(doc.Body)
					if len(body) > 300 {
						doc.Body = string(body[:300])
					}
					for _, original := range allowed {
						if original.ID == doc.ID {
							used[doc.ID] = original
						}
					}
					alias := ""
					for a, id := range aliases {
						if id == doc.ID {
							alias = a
						}
					}
					if alias == "" {
						alias = fmt.Sprintf("source-%d", len(aliases)+1)
						aliases[alias] = doc.ID
					}
					sources = append(sources, map[string]any{"id": alias, "title": doc.Title, "body": doc.Body, "version": doc.Version, "origin": doc.Origin, "synthetic": doc.Synthetic})
				}
				result = map[string]any{"sources": sources, "search_mode": msg.SearchMode}
			case "prepare_action":
				var args struct {
					Kind   string `json:"kind"`
					Reason string `json:"reason"`
				}
				if decodeArguments(call.Function.Arguments, &args) != nil || !readContext || msg.ActionID != "" || msg.RequestID != "" {
					return Message{}, ErrInvalid
				}
				var action Action
				action, toolErr = s.Prepare(ctx, p, args.Kind, args.Reason)
				if toolErr == nil {
					msg.ActionID = action.ID
					result = map[string]any{"action": action, "requires_confirmation": true, "executed": false}
				}
			case "prepare_request":
				var args struct {
					Kind    string `json:"kind"`
					Subject string `json:"subject"`
					Body    string `json:"body"`
				}
				if decodeArguments(call.Function.Arguments, &args) != nil || msg.ActionID != "" || msg.RequestID != "" {
					return Message{}, ErrInvalid
				}
				var request HelpRequest
				request, toolErr = s.PrepareRequest(ctx, p, requestID, args.Kind, args.Subject, args.Body)
				if toolErr == nil {
					msg.RequestID = request.ID
					result = map[string]any{"request": request, "requires_confirmation": true, "sent": false}
				}
			default:
				return Message{}, ErrForbidden
			}
			if toolErr != nil {
				code := "unavailable"
				switch {
				case errors.Is(toolErr, ErrConflict):
					code = "already_exists"
				case errors.Is(toolErr, ErrForbidden):
					code = "requirements_not_met"
				case errors.Is(toolErr, ErrInvalid):
					code = "invalid_arguments"
				default:
					return Message{}, toolErr
				}
				result = map[string]any{"error": code, "executed": false}
			}
			raw, _ := json.Marshal(result)
			turns = append(turns, AgentTurn{Role: "tool", ToolName: call.Function.Name, CallID: call.ID, Content: string(raw)})
			_, err = s.Repo.Update(ctx, p.ID, func(st *State) error {
				st.Events = append(st.Events, Event{Type: "agent_tool", ObjectID: call.Function.Name, CorrelationID: requestID, Outcome: "returned", At: s.Now()})
				return nil
			})
			if err != nil {
				return Message{}, err
			}
			if msg.ActionID != "" || msg.RequestID != "" {
				msg.Text = "Подготовил действие. Проверь параметры в карточке и подтверди отправку."
				if msg.RequestID != "" {
					msg.Text = "Подготовил вопрос ответственному. Проверь текст и получателя перед отправкой."
				}
				for _, doc := range used {
					msg.SourceIDs = append(msg.SourceIDs, doc.ID)
					msg.Dependencies = append(msg.Dependencies, contextRefs([]Document{doc}, nil)...)
				}
				latest, fresh := s.operations(ctx, p)
				if !fresh {
					return Message{}, ErrUnavailable
				}
				current, _ := s.allowedDocuments(ctx, p, latest)
				if !permitsMessage(current, msg) {
					return Message{}, ErrForbidden
				}
				return msg, nil
			}
		}
	}
	return Message{}, ErrModelUnavailable
}
