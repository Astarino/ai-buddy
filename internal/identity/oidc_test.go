package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
)

type attempts struct {
	mu     sync.Mutex
	values map[string]Attempt
	fail   bool
}

func (s *attempts) SaveLogin(_ context.Context, state string, a Attempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("test store unavailable")
	}
	s.values[state] = a
	return nil
}
func (s *attempts) ConsumeLogin(_ context.Context, state string) (Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.values[state]
	delete(s.values, state)
	if !ok {
		return Attempt{}, buddy.ErrForbidden
	}
	return a, nil
}

func encode(value any) string {
	raw, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func testProvider(t *testing.T) *httptest.Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration", "/unsafe/.well-known/openid-configuration":
			issuer, auth := server.URL, server.URL+"/authorize"
			if strings.HasPrefix(r.URL.Path, "/unsafe/") {
				issuer += "/unsafe"
				auth = "https://unapproved.example/authorize"
			}
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": auth, "token_endpoint": server.URL + "/token", "jwks_uri": server.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "test-key", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/token":
			r.ParseForm()
			parts := strings.Split(r.Form.Get("code"), "|")
			if len(parts) != 3 {
				w.WriteHeader(400)
				return
			}
			mode, nonce := parts[0], parts[1]
			digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(digest[:]) != parts[2] || mode == "exchange-error" {
				w.WriteHeader(400)
				return
			}
			claims := map[string]any{"iss": server.URL, "aud": "buddy-client", "sub": "employee-17", "nonce": nonce, "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix()}
			switch mode {
			case "nonce":
				claims["nonce"] = "wrong"
			case "issuer":
				claims["iss"] = "https://another.example"
			case "audience":
				claims["aud"] = "other-application"
			case "expiry":
				claims["exp"] = time.Now().Add(-10 * time.Minute).Unix()
			case "subject":
				claims["sub"] = ""
			}
			unsigned := encode(map[string]string{"alg": "RS256", "kid": "test-key", "typ": "JWT"}) + "." + encode(claims)
			hash := sha256.Sum256([]byte(unsigned))
			signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			if mode == "signature" {
				signature[0] ^= 0xff
			}
			response := map[string]any{"access_token": "opaque-api-token", "token_type": "Bearer", "expires_in": 60}
			if mode != "missing-id-token" {
				response["id_token"] = unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
			}
			json.NewEncoder(w).Encode(response)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestOIDCValidatesCodePKCEAndIdentity(t *testing.T) {
	provider := testProvider(t)
	store := &attempts{values: map[string]Attempt{}}
	client, err := New(context.Background(), Config{Issuer: provider.URL, ClientID: "buddy-client", ClientSecret: "server-secret", RedirectURL: "http://localhost/api/login/callback"}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "nonce", "issuer", "audience", "expiry", "subject", "signature", "missing-id-token", "exchange-error"} {
		t.Run(mode, func(t *testing.T) {
			location, state, err := client.Begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(location)
			q := u.Query()
			if q.Get("code_challenge_method") != "S256" || q.Get("scope") != "openid" || q.Get("code_verifier") != "" {
				t.Fatal("invalid authorization request", q)
			}
			code := mode + "|" + q.Get("nonce") + "|" + q.Get("code_challenge")
			principal, err := client.Complete(context.Background(), state, state, code)
			if mode == "valid" {
				if err != nil || principal.Issuer != provider.URL || principal.Subject != "employee-17" {
					t.Fatal(principal, err)
				}
			} else if !errors.Is(err, buddy.ErrForbidden) {
				t.Fatal("invalid identity accepted", mode, principal, err)
			}
			if _, err := client.Complete(context.Background(), state, state, code); !errors.Is(err, buddy.ErrForbidden) {
				t.Fatal("callback replay accepted", err)
			}
		})
	}
	location, state, _ := client.Begin(context.Background())
	u, _ := url.Parse(location)
	code := "valid|" + u.Query().Get("nonce") + "|" + u.Query().Get("code_challenge")
	for _, args := range [][3]string{{"short", state, code}, {state, "different-cookie", code}, {state, state, ""}} {
		if _, err := client.Complete(context.Background(), args[0], args[1], args[2]); err == nil {
			t.Fatal("invalid state/cookie/code accepted")
		}
	}
	a := store.values[state]
	a.ExpiresAt = time.Now().Add(-time.Minute)
	store.values[state] = a
	if _, err := client.Complete(context.Background(), state, state, code); err == nil {
		t.Fatal("expired login attempt accepted")
	}
	store.fail = true
	if _, _, err := client.Begin(context.Background()); err == nil {
		t.Fatal("login created without durable state")
	}
}

func TestOIDCRejectsUnsafeConfiguration(t *testing.T) {
	provider := testProvider(t)
	base := Config{Issuer: provider.URL, ClientID: "buddy-client", ClientSecret: "server-secret", RedirectURL: "http://localhost/api/login/callback"}
	for _, alter := range []func(*Config){func(c *Config) { c.ClientID = "" }, func(c *Config) { c.Issuer = "http://outside.example" }, func(c *Config) { c.RedirectURL = "javascript:bad" }, func(c *Config) { c.Issuer = provider.URL + "/unsafe" }, func(c *Config) { c.Issuer = provider.URL + "/missing" }, func(c *Config) { c.TrustedOrigins = []string{"https://idp.example/with-path"} }} {
		cfg := base
		alter(&cfg)
		if _, err := New(context.Background(), cfg, &attempts{values: map[string]Attempt{}}, nil); err == nil {
			t.Fatal("unsafe configuration accepted", cfg.Issuer)
		}
	}
	base.TrustedOrigins = []string{provider.URL}
	if _, err := New(context.Background(), base, &attempts{values: map[string]Attempt{}}, nil); err != nil {
		t.Fatal(err)
	}
}
