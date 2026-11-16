package model

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/outbound"
)

type EmbeddingClient struct {
	BaseURL, Model, Version string
	HTTP                    *http.Client
	gate                    chan struct{}
}

func NewEmbedder(base, name, version string, llm buddy.Model) (*EmbeddingClient, error) {
	u, err := outbound.ValidateBaseURL(base)
	_, digestErr := hex.DecodeString(version)
	if err != nil || !outbound.Local(u) || name == "" || len(version) != 64 || digestErr != nil {
		return nil, buddy.ErrInvalid
	}
	gate := make(chan struct{}, 1)
	if client, ok := llm.(*Client); ok {
		gate = client.gate
	}
	return &EmbeddingClient{strings.TrimRight(base, "/"), name, version, outbound.Client(20 * time.Second), gate}, nil
}

func (c *EmbeddingClient) Name() string { return c.Model + "@" + c.Version }

func (c *EmbeddingClient) Embed(ctx context.Context, text []string) ([][]float64, error) {
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	default:
		return nil, buddy.ErrBusy
	}
	if len(text) == 0 || len(text) > 128 {
		return nil, buddy.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	identity, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	tagsResponse, err := c.HTTP.Do(identity)
	if err != nil {
		return nil, buddy.ErrUnavailable
	}
	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(tagsResponse.Body, 1<<20)).Decode(&tags)
	tagsResponse.Body.Close()
	matched := false
	for _, tag := range tags.Models {
		if tag.Name == c.Model && tag.Digest == c.Version {
			matched = true
		}
	}
	if tagsResponse.StatusCode != 200 || decodeErr != nil || !matched {
		return nil, buddy.ErrUnavailable
	}
	raw, err := json.Marshal(map[string]any{"model": c.Model, "input": text, "keep_alive": 0, "truncate": false})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/embed", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, buddy.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, buddy.ErrUnavailable
	}
	var body struct {
		Vectors [][]float64 `json:"embeddings"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body) != nil || len(body.Vectors) != len(text) {
		return nil, buddy.ErrUnavailable
	}
	return body.Vectors, nil
}
