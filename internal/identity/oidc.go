package identity

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/outbound"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Attempt struct {
	Nonce     string
	Verifier  string
	ExpiresAt time.Time
}

type AttemptStore interface {
	SaveLogin(context.Context, string, Attempt) error
	ConsumeLogin(context.Context, string) (Attempt, error)
}

type Config struct {
	Issuer, ClientID, ClientSecret, RedirectURL string
	TrustedOrigins                              []string
}
type OIDC struct {
	config   oauth2.Config
	verifier *oidc.IDTokenVerifier
	store    AttemptStore
	http     *http.Client
}
type Identity struct {
	Issuer, Subject string
	ExpiresAt       time.Time
}

func New(ctx context.Context, cfg Config, store AttemptStore, client *http.Client) (*OIDC, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("OIDC client configuration is required")
	}
	issuer, err := outbound.ValidateBaseURL(cfg.Issuer)
	if err != nil {
		return nil, err
	}
	if _, err := outbound.ValidateBaseURL(cfg.RedirectURL); err != nil {
		return nil, err
	}
	if client == nil {
		client = outbound.Client(10 * time.Second)
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed")
	}
	endpoint := provider.Endpoint()
	var metadata struct {
		JWKS string `json:"jwks_uri"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return nil, fmt.Errorf("invalid OIDC metadata")
	}
	origins := map[string]bool{issuer.Scheme + "://" + issuer.Host: true}
	for _, raw := range cfg.TrustedOrigins {
		allowed, err := outbound.ValidateBaseURL(raw)
		if err != nil || (allowed.Path != "" && allowed.Path != "/") {
			return nil, fmt.Errorf("invalid OIDC trusted origin")
		}
		origins[allowed.Scheme+"://"+allowed.Host] = true
	}
	for _, address := range []string{endpoint.AuthURL, endpoint.TokenURL, metadata.JWKS} {
		u, err := outbound.ValidateBaseURL(address)
		if err != nil || !origins[u.Scheme+"://"+u.Host] {
			return nil, fmt.Errorf("unsafe OIDC endpoint")
		}
	}
	return &OIDC{config: oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL, Endpoint: endpoint, Scopes: []string{oidc.ScopeOpenID}}, verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID, SupportedSigningAlgs: []string{"RS256", "ES256"}}), store: store, http: client}, nil
}

func (o *OIDC) Begin(ctx context.Context) (string, string, error) {
	state, nonce, verifier := buddy.NewID()+buddy.NewID(), buddy.NewID()+buddy.NewID(), oauth2.GenerateVerifier()
	if err := o.store.SaveLogin(ctx, state, Attempt{Nonce: nonce, Verifier: verifier, ExpiresAt: time.Now().Add(10 * time.Minute)}); err != nil {
		return "", "", err
	}
	return o.config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), state, nil
}

func (o *OIDC) Complete(ctx context.Context, state, cookie, code string) (Identity, error) {
	if len(state) != 64 || subtle.ConstantTimeCompare([]byte(state), []byte(cookie)) != 1 || code == "" {
		return Identity{}, buddy.ErrForbidden
	}
	attempt, err := o.store.ConsumeLogin(ctx, state)
	if err != nil || !attempt.ExpiresAt.After(time.Now()) {
		return Identity{}, buddy.ErrForbidden
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, o.http)
	token, err := o.config.Exchange(ctx, code, oauth2.VerifierOption(attempt.Verifier))
	if err != nil {
		return Identity{}, buddy.ErrForbidden
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return Identity{}, buddy.ErrForbidden
	}
	id, err := o.verifier.Verify(ctx, raw)
	if err != nil || id.Subject == "" || !id.Expiry.After(time.Now()) || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(attempt.Nonce)) != 1 {
		return Identity{}, buddy.ErrForbidden
	}
	return Identity{Issuer: id.Issuer, Subject: id.Subject, ExpiresAt: id.Expiry}, nil
}
