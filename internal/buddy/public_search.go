package buddy

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

type PublicSearch struct {
	ID        string     `json:"id"`
	Topic     string     `json:"topic"`
	Query     string     `json:"query"`
	FetchedAt time.Time  `json:"fetched_at"`
	Documents []Document `json:"documents"`
}

type PublicSearcher interface {
	Search(context.Context, string, string) ([]Document, error)
}

func (s *Service) SearchPublic(ctx context.Context, p Profile, id, topic, query string) (PublicSearch, error) {
	if s.Authorizer != nil {
		if err := s.Authorizer.Check(ctx, p, Resource{System: "ai-buddy", Kind: "tool", ID: "public-search"}, "use"); err != nil {
			return PublicSearch{}, err
		}
	} else if !s.Demo {
		return PublicSearch{}, ErrForbidden
	}
	query = strings.TrimSpace(query)
	if len(id) < 8 || len(id) > 80 || utf8.RuneCountInString(query) < 3 || utf8.RuneCountInString(query) > 2000 || (topic != "go" && topic != "git") {
		return PublicSearch{}, ErrInvalid
	}
	if credentialRequest(query) || policyRequest(query) {
		return PublicSearch{}, ErrForbidden
	}
	state, err := s.Repo.Read(ctx, p.ID)
	if err != nil {
		return PublicSearch{}, err
	}
	for _, run := range state.Searches {
		if run.ID == id {
			if run.Query != query || run.Topic != topic {
				return PublicSearch{}, ErrConflict
			}
			return run, nil
		}
	}
	if s.Searcher == nil {
		return PublicSearch{}, ErrUnavailable
	}
	docs, err := s.Searcher.Search(ctx, topic, query)
	if err != nil {
		return PublicSearch{}, err
	}
	run := PublicSearch{ID: id, Topic: topic, Query: query, FetchedAt: s.Now().UTC(), Documents: []Document{}}
	for i, d := range docs {
		if i >= 3 {
			break
		}
		if d.Scope != "public" || d.DataClass != "public" || containsCredentialValue(d.Body) {
			return PublicSearch{}, ErrForbidden
		}
		d.ID = "web-" + fingerprint(id)[:16] + "-" + fingerprint(d.URL)[:12]
		run.Documents = append(run.Documents, d)
	}
	_, err = s.Repo.Update(ctx, p.ID, func(st *State) error {
		for _, old := range st.Searches {
			if old.ID == id {
				if old.Query != query || old.Topic != topic {
					return ErrConflict
				}
				run = old
				return nil
			}
		}
		if len(st.Searches) >= 30 {
			return ErrConflict
		}
		st.Searches = append(st.Searches, run)
		st.Events = append(st.Events, Event{Type: "public_search", ObjectID: id, At: s.Now(), Outcome: "completed"})
		return nil
	})
	return run, err
}
