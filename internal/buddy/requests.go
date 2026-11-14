package buddy

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type RequestReply struct {
	ID        string    `json:"id"`
	AuthorID  string    `json:"author_id"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

type RequestTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Service) requestTargets(ctx context.Context, p Profile) []RequestTarget {
	result := []RequestTarget{}
	if s.Authorizer == nil && !s.Demo {
		return result
	}
	if p.Audience == "responsible" {
		return result
	}
	for _, kind := range []string{"mentor", "owner"} {
		target, err := s.recipient(p, kind)
		if err != nil {
			continue
		}
		if s.Authorizer != nil {
			if err := s.Authorizer.Check(ctx, p, Resource{System: "ai-buddy", Kind: "person", ID: target.ID}, "request_help"); err != nil {
				continue
			}
		}
		result = append(result, RequestTarget{Kind: kind, ID: target.ID, Name: target.Name})
	}
	return result
}

type HelpRequest struct {
	ID              string         `json:"id"`
	RequesterID     string         `json:"requester_id"`
	Requester       string         `json:"requester"`
	AssigneeID      string         `json:"assignee_id"`
	Assignee        string         `json:"assignee"`
	Kind            string         `json:"kind"`
	Subject         string         `json:"subject"`
	Body            string         `json:"body"`
	Status          string         `json:"status"`
	Version         int            `json:"version"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	Replies         []RequestReply `json:"replies"`
	OperationKey    string         `json:"operation_key,omitempty"`
	OperationStatus string         `json:"operation_status,omitempty"`
	PendingDecision string         `json:"pending_decision,omitempty"`
}

type RequestStore interface {
	RequestsFor(context.Context, string) ([]HelpRequest, error)
	Request(context.Context, string) (HelpRequest, error)
	UpdateRequest(context.Context, string, func(*HelpRequest) error, Event) (HelpRequest, error)
}

type ResourceDecider interface {
	Decide(context.Context, string, string, string, string) error
}

func (s *Service) requestStore() RequestStore {
	if s.Requests != nil {
		return s.Requests
	}
	store, _ := s.Repo.(RequestStore)
	return store
}

func (s *Service) recipient(requester Profile, kind string) (Profile, error) {
	id := requester.MentorID
	if kind == "owner" || kind == "access" {
		id = requester.AccessOwnerID
	} else if kind != "mentor" {
		return Profile{}, ErrInvalid
	}
	if id == "" || id == requester.ID {
		return Profile{}, ErrForbidden
	}
	target, err := s.Catalog.Profile(id)
	if err != nil || target.Audience != "responsible" {
		return Profile{}, ErrForbidden
	}
	return target, nil
}

func (s *Service) requestAllowed(ctx context.Context, p Profile, r HelpRequest, write bool) error {
	requester, err := s.Catalog.Profile(r.RequesterID)
	if err != nil {
		return ErrNotFound
	}
	if p.ID != requester.ID {
		target, err := s.recipient(requester, r.Kind)
		if err != nil || p.ID != r.AssigneeID || p.ID != target.ID || r.Status == "draft" {
			return ErrNotFound
		}
	}
	if s.Authorizer != nil {
		permission := "read"
		if write {
			permission = "reply"
		}
		return s.Authorizer.Check(ctx, p, Resource{System: "ai-buddy", Kind: "request", ID: r.ID}, permission)
	}
	if !s.Demo {
		return ErrForbidden
	}
	return nil
}

func (s *Service) ListRequests(ctx context.Context, p Profile) ([]HelpRequest, error) {
	result := []HelpRequest{}
	store := s.requestStore()
	if store == nil {
		return result, nil
	}
	rows, err := store.RequestsFor(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if err := s.requestAllowed(ctx, p, r, false); err != nil {
			if !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
				return nil, err
			}
			continue
		}
		r, err = s.refreshAccessRequest(ctx, r)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

func (s *Service) GetRequest(ctx context.Context, p Profile, id string) (HelpRequest, error) {
	store := s.requestStore()
	if store == nil {
		return HelpRequest{}, ErrUnavailable
	}
	r, err := store.Request(ctx, id)
	if err != nil {
		return r, err
	}
	if err = s.requestAllowed(ctx, p, r, false); err != nil {
		return HelpRequest{}, err
	}
	return s.refreshAccessRequest(ctx, r)
}

func (s *Service) refreshAccessRequest(ctx context.Context, r HelpRequest) (HelpRequest, error) {
	if r.Kind != "access" {
		return r, nil
	}
	requester, err := s.Catalog.Profile(r.RequesterID)
	if err != nil {
		return HelpRequest{}, ErrNotFound
	}
	ops, fresh := s.operations(ctx, requester)
	if !fresh {
		return HelpRequest{}, ErrUnavailable
	}
	for _, op := range ops {
		if op.Key == r.OperationKey {
			if err = s.ensureAccessRequest(ctx, requester, op); err != nil {
				return HelpRequest{}, err
			}
			return s.requestStore().Request(ctx, r.ID)
		}
	}
	return HelpRequest{}, ErrUnavailable
}

func validSharedText(text string, min, max int) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(text))
	return n >= min && n <= max && !credentialRequest(text) && !policyRequest(text)
}

func (s *Service) PrepareRequest(ctx context.Context, p Profile, id, kind, subject, body string) (HelpRequest, error) {
	if s.Authorizer == nil && !s.Demo {
		return HelpRequest{}, ErrForbidden
	}
	if len(id) < 8 || len(id) > 80 || !validSharedText(subject, 3, 120) || !validSharedText(body, 3, 2000) {
		return HelpRequest{}, ErrInvalid
	}
	if p.Audience == "responsible" || kind == "access" {
		return HelpRequest{}, ErrForbidden
	}
	// Client retry keys belong to one requester; they cannot collide with another person's card.
	id = "help-" + fingerprint([]string{p.ID, id})[:32]
	target, err := s.recipient(p, kind)
	if err != nil {
		return HelpRequest{}, err
	}
	store := s.requestStore()
	if store == nil {
		return HelpRequest{}, ErrUnavailable
	}
	return store.UpdateRequest(ctx, id, func(r *HelpRequest) error {
		if r.ID != "" {
			if r.RequesterID != p.ID || r.Kind != kind || r.Subject != subject || r.Body != body {
				return ErrConflict
			}
			return nil
		}
		if s.Authorizer != nil {
			if err := s.Authorizer.Check(ctx, p, Resource{System: "ai-buddy", Kind: "person", ID: target.ID}, "request_help"); err != nil {
				return err
			}
		}
		*r = HelpRequest{ID: id, RequesterID: p.ID, Requester: p.Name, AssigneeID: target.ID, Assignee: target.Name, Kind: kind, Subject: subject, Body: body, Status: "draft", Version: 1, CreatedAt: s.Now().UTC(), UpdatedAt: s.Now().UTC(), Replies: []RequestReply{}}
		return nil
	}, Event{Type: "request_prepared", OwnerID: p.ID, ObjectID: id})
}

func (s *Service) ChangeRequest(ctx context.Context, p Profile, id, command string, version int, expectedRecipient ...string) (HelpRequest, error) {
	store := s.requestStore()
	if store == nil {
		return HelpRequest{}, ErrUnavailable
	}
	return store.UpdateRequest(ctx, id, func(r *HelpRequest) error {
		if err := s.requestAllowed(ctx, p, *r, true); err != nil {
			return err
		}
		if p.ID != r.RequesterID {
			return ErrForbidden
		}
		if command == "send" && r.Status != "draft" {
			return nil
		}
		if command == "resolve" && r.Status == "resolved" {
			return nil
		}
		if r.Version != version {
			return ErrConflict
		}
		switch command {
		case "send":
			target, err := s.recipient(p, r.Kind)
			if err != nil {
				return err
			}
			if target.ID != r.AssigneeID {
				return ErrConflict
			}
			r.Status = "waiting"
		case "resolve":
			if r.Status == "draft" || r.Kind == "access" {
				return ErrForbidden
			}
			r.Status = "resolved"
		case "reassign":
			if r.Status == "resolved" {
				return ErrConflict
			}
			target, err := s.recipient(p, r.Kind)
			if err != nil {
				return err
			}
			if len(expectedRecipient) != 1 || expectedRecipient[0] != target.ID {
				return ErrConflict
			}
			if target.ID == r.AssigneeID {
				return nil
			}
			if s.Authorizer != nil {
				if err := s.Authorizer.Check(ctx, p, Resource{System: "ai-buddy", Kind: "person", ID: target.ID}, "request_help"); err != nil {
					return err
				}
			}
			r.AssigneeID, r.Assignee = target.ID, target.Name
			if r.Status != "draft" {
				r.Status = "waiting"
			}
		default:
			return ErrInvalid
		}
		r.Version++
		r.UpdatedAt = s.Now().UTC()
		return nil
	}, Event{Type: "request_" + command, OwnerID: p.ID, ObjectID: id})
}

func (s *Service) ReplyRequest(ctx context.Context, p Profile, id, replyID, text string, version int) (HelpRequest, error) {
	if len(replyID) < 8 || len(replyID) > 80 || !validSharedText(text, 1, 2000) {
		return HelpRequest{}, ErrInvalid
	}
	store := s.requestStore()
	if store == nil {
		return HelpRequest{}, ErrUnavailable
	}
	return store.UpdateRequest(ctx, id, func(r *HelpRequest) error {
		if err := s.requestAllowed(ctx, p, *r, true); err != nil {
			return err
		}
		for _, reply := range r.Replies {
			if reply.ID == replyID {
				if reply.AuthorID != p.ID || reply.Text != text {
					return ErrConflict
				}
				return nil
			}
		}
		if r.Version != version {
			return ErrConflict
		}
		if r.Status == "draft" || r.Status == "resolved" || r.PendingDecision != "" || len(r.Replies) >= 100 {
			return ErrConflict
		}
		r.Replies = append(r.Replies, RequestReply{ID: replyID, AuthorID: p.ID, Author: p.Name, Text: text, CreatedAt: s.Now().UTC()})
		if r.Kind == "access" {
			r.Status = "waiting"
		} else if p.ID == r.AssigneeID {
			r.Status = "answered"
		} else {
			r.Status = "waiting"
		}
		r.Version++
		r.UpdatedAt = s.Now().UTC()
		return nil
	}, Event{Type: "request_replied", OwnerID: p.ID, ObjectID: id})
}

// The operation key also identifies its inbox card, so recovery cannot duplicate it.
func (s *Service) ensureAccessRequest(ctx context.Context, p Profile, op Operation) error {
	store := s.requestStore()
	if store == nil || op.Kind != "access" {
		return nil
	}
	target, err := s.recipient(p, "access")
	if err != nil {
		if s.Demo {
			return nil
		}
		return err
	}
	_, err = store.UpdateRequest(ctx, "access-"+op.Key, func(r *HelpRequest) error {
		if r.ID == "" {
			*r = HelpRequest{ID: "access-" + op.Key, RequesterID: p.ID, Requester: p.Name, AssigneeID: target.ID, Assignee: target.Name, Kind: "access", Subject: "Доступ к " + op.Resource, Body: op.Reason, Status: "waiting", Version: 1, CreatedAt: s.Now().UTC(), UpdatedAt: s.Now().UTC(), Replies: []RequestReply{}, OperationKey: op.Key}
		}
		cleared := r.PendingDecision != "" && (r.PendingDecision == op.Status || slices.Contains([]string{"granted", "rejected", "revoked"}, op.Status))
		if cleared {
			r.PendingDecision = ""
		}
		if r.OperationStatus == op.Status && !cleared {
			return nil
		}
		r.OperationStatus = op.Status
		if slices.Contains([]string{"granted", "rejected", "revoked"}, op.Status) {
			r.Status = "resolved"
		}
		r.Version++
		r.UpdatedAt = s.Now().UTC()
		return nil
	}, Event{Type: "access_request_synced", OwnerID: p.ID, ObjectID: op.Key})
	return err
}

func (s *Service) DecideRequest(ctx context.Context, p Profile, id, status string, version int) (HelpRequest, error) {
	r, err := s.GetRequest(ctx, p, id)
	if err != nil {
		return HelpRequest{}, err
	}
	if r.Kind != "access" || r.AssigneeID != p.ID || !slices.Contains([]string{"approved", "granted", "rejected", "revoked"}, status) {
		return HelpRequest{}, ErrForbidden
	}
	requester, err := s.Catalog.Profile(r.RequesterID)
	if err != nil {
		return HelpRequest{}, ErrForbidden
	}
	if err = s.requestAllowed(ctx, p, r, true); err != nil {
		return HelpRequest{}, err
	}
	if s.Authorizer != nil {
		if err = s.Authorizer.Check(ctx, p, Resource{System: "confluence", Kind: "space", ID: requester.Space}, "decide_access"); err != nil {
			return HelpRequest{}, err
		}
	}
	if r.Version != version && r.OperationStatus != status {
		return HelpRequest{}, ErrConflict
	}
	decider, ok := s.Corporate.(ResourceDecider)
	if !ok {
		return HelpRequest{}, ErrUnavailable
	}
	_, err = s.requestStore().UpdateRequest(ctx, id, func(current *HelpRequest) error {
		if err := s.requestAllowed(ctx, p, *current, true); err != nil {
			return err
		}
		if current.PendingDecision != "" && current.PendingDecision != status {
			return ErrConflict
		}
		if current.PendingDecision == status {
			return nil
		}
		if current.OperationStatus == status {
			return nil
		}
		if current.Version != version {
			return ErrConflict
		}
		allowed := map[string][]string{"pending": {"approved", "rejected"}, "approved": {"granted", "rejected"}, "granted": {"revoked"}}
		if !slices.Contains(allowed[current.OperationStatus], status) {
			return ErrConflict
		}
		current.PendingDecision = status
		return nil
	}, Event{Type: "access_decision_requested", OwnerID: requester.ID, ObjectID: id})
	if err != nil {
		return HelpRequest{}, err
	}
	if err = decider.Decide(ctx, p.ID, requester.ID, r.OperationKey, status); err != nil {
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) {
			_, clearErr := s.requestStore().UpdateRequest(ctx, id, func(current *HelpRequest) error {
				if current.PendingDecision == status {
					current.PendingDecision = ""
				}
				return nil
			}, Event{Type: "access_decision_rejected", OwnerID: requester.ID, ObjectID: id})
			if clearErr != nil {
				return HelpRequest{}, ErrUnavailable
			}
		}
		return HelpRequest{}, err
	}
	ops, fresh := s.operations(ctx, requester)
	if !fresh {
		return HelpRequest{}, ErrUnavailable
	}
	for _, op := range ops {
		if op.Key == r.OperationKey {
			if err = s.ensureAccessRequest(ctx, requester, op); err != nil {
				return HelpRequest{}, err
			}
			return s.GetRequest(ctx, p, id)
		}
	}
	return HelpRequest{}, fmt.Errorf("%w: missing operation result", ErrUnavailable)
}
