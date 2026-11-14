package authorization

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/outbound"
)

type Client struct {
	BaseURL, Token string
	HTTP           *http.Client
}
type Request struct {
	ActorID    string         `json:"actor_id"`
	Issuer     string         `json:"issuer"`
	Subject    string         `json:"subject"`
	Resource   buddy.Resource `json:"resource"`
	Permission string         `json:"permission"`
}
type Decision struct {
	ActorID       string         `json:"actor_id"`
	Resource      buddy.Resource `json:"resource"`
	Permission    string         `json:"permission"`
	Allowed       bool           `json:"allowed"`
	ExpiresAt     time.Time      `json:"expires_at"`
	PolicyVersion string         `json:"policy_version"`
}

func New(base, token string) (*Client, error) {
	if _, err := outbound.ValidateBaseURL(base); err != nil {
		return nil, err
	}
	if token == "" {
		return nil, buddy.ErrInvalid
	}
	return &Client{strings.TrimRight(base, "/"), token, outbound.Client(3 * time.Second)}, nil
}

func (c *Client) Check(ctx context.Context, p buddy.Profile, resource buddy.Resource, permission string) error {
	if p.ID == "" || p.Issuer == "" || p.Subject == "" || resource.System == "" || resource.Kind == "" || resource.ID == "" {
		return buddy.ErrForbidden
	}
	allowed := permission == "read"
	switch permission {
	case "request_access", "decide_access":
		allowed = resource.System == "confluence" && resource.Kind == "space"
	case "assign_self":
		allowed = resource.System == "jira" && resource.Kind == "issue"
	case "request_help":
		allowed = resource.System == "ai-buddy" && resource.Kind == "person"
	case "reply":
		allowed = resource.System == "ai-buddy" && resource.Kind == "request"
	case "use":
		allowed = resource.System == "ai-buddy" && resource.Kind == "tool" && resource.ID == "public-search"
	}
	if !allowed {
		return buddy.ErrForbidden
	}
	if _, err := outbound.ValidateBaseURL(c.BaseURL); err != nil {
		return buddy.ErrUnavailable
	}
	raw, err := json.Marshal(Request{p.ID, p.Issuer, p.Subject, resource, permission})
	if err != nil {
		return buddy.ErrInvalid
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/authorize", bytes.NewReader(raw))
	if err != nil {
		return buddy.ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return buddy.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return buddy.ErrForbidden
	}
	if resp.StatusCode != http.StatusOK {
		return buddy.ErrUnavailable
	}
	var decision Decision
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decision) != nil || decoder.Decode(new(any)) != io.EOF {
		return buddy.ErrUnavailable
	}
	if decision.ActorID != p.ID || decision.Resource != resource || decision.Permission != permission || !decision.ExpiresAt.After(time.Now()) || decision.PolicyVersion == "" {
		return buddy.ErrForbidden
	}
	if !decision.Allowed {
		return buddy.ErrForbidden
	}
	return nil
}
