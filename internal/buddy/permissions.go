package buddy

import (
	"context"
	"errors"
)

func ValidActionTarget(p Profile, a Action) bool {
	return (a.Kind == "access" && a.Resource == p.Space && a.Level == "Чтение") || (a.Kind == "issue" && a.Resource == p.Project+"-101" && a.Level == "Назначить на себя")
}

func (s *Service) allowedDocuments(ctx context.Context, p Profile, ops []Operation) ([]Document, bool) {
	if s.Authorizer == nil {
		if !s.Demo {
			return []Document{}, false
		}
		return s.Catalog.Allowed(p, HasAccess(ops)), true
	}
	documents, fresh := []Document{}, true
	for _, d := range s.Catalog.Eligible(p) {
		err := s.Authorizer.Check(ctx, p, Resource{d.System, d.ResourceKind, d.Resource}, "read")
		if err == nil {
			documents = append(documents, d)
		} else if !errors.Is(err, ErrForbidden) {
			fresh = false
		}
	}
	return documents, fresh
}

func (s *Service) authorizeAction(ctx context.Context, p Profile, kind string) error {
	if s.Authorizer == nil {
		if s.Demo {
			return nil
		}
		return ErrForbidden
	}
	resource, permission := Resource{System: "confluence", Kind: "space", ID: p.Space}, "request_access"
	if kind == "issue" {
		resource, permission = Resource{System: "jira", Kind: "issue", ID: p.Project + "-101"}, "assign_self"
	} else if kind != "access" {
		return ErrInvalid
	}
	return s.Authorizer.Check(ctx, p, resource, permission)
}
