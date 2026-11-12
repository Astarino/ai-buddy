package corporate

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/outbound"
)

type Client struct {
	BaseURL       string
	Token         string
	OperatorToken string
	HTTP          *http.Client
}

func NewClient(base, token, operator string) *Client {
	return &Client{strings.TrimRight(base, "/"), token, operator, outbound.Client(3 * time.Second)}
}

func (c *Client) call(ctx context.Context, method, path, token string, body, target any) error {
	if _, err := outbound.ValidateBaseURL(c.BaseURL); err != nil {
		return buddy.ErrUnavailable
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return buddy.ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	_, correlation := buddy.AuditIdentity(ctx)
	if correlation != "" {
		req.Header.Set("X-Buddy-Request-ID", correlation)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return buddy.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return buddy.ErrNotFound
	}
	if resp.StatusCode == http.StatusConflict {
		return buddy.ErrConflict
	}
	if resp.StatusCode == http.StatusForbidden {
		return buddy.ErrForbidden
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return buddy.ErrUnavailable
	}
	if target == nil {
		return nil
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(target); err != nil {
		return buddy.ErrUnavailable
	}
	return nil
}

func (c *Client) List(ctx context.Context, owner string) ([]buddy.Operation, error) {
	result := []buddy.Operation{}
	err := c.call(ctx, http.MethodGet, "/operations?user="+url.QueryEscape(owner), c.Token, nil, &result)
	return result, err
}

func (c *Client) Execute(ctx context.Context, owner string, a buddy.Action) (buddy.Operation, error) {
	var result buddy.Operation
	err := c.call(ctx, http.MethodPost, "/operations", c.Token, map[string]any{"user_id": owner, "action": a}, &result)
	return result, err
}

func (c *Client) Event(ctx context.Context, owner, key, status string) error {
	return c.call(ctx, http.MethodPost, "/events", c.OperatorToken, map[string]string{"user_id": owner, "key": key, "status": status}, nil)
}
