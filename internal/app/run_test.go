package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/testsupport"
)

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	return address
}

func TestCorporateModeRequiresIdentityAuthorityAndNoDemoControls(t *testing.T) {
	t.Setenv("APP_MODE", "unknown")
	if err := RunContext(context.Background(), "server"); err == nil {
		t.Fatal("unknown mode accepted")
	}
	t.Setenv("APP_MODE", "corporate")
	if err := RunContext(context.Background(), "corporate"); err == nil {
		t.Fatal("simulator started as corporate backend")
	}
	db := testsupport.Database(t)
	t.Setenv("DATABASE_URL", db.Pool.Config().ConnString())
	t.Setenv("MODEL_PROVIDER", "ollama")
	t.Setenv("MODEL_NAME", "not-loaded-in-tests")
	t.Setenv("MODEL_BASE_URL", "http://127.0.0.1:11434")
	t.Setenv("MODEL_API_KEY", "")
	t.Setenv("MODEL_DATA_POLICY", "internal")
	t.Setenv("CORPORATE_TOKEN", "synthetic-corporate-service")
	t.Setenv("CORPORATE_BASE_URL", "http://127.0.0.1:8091")
	t.Setenv("SECURE_COOKIE", "true")
	t.Setenv("DEMO_OPERATOR_TOKEN", "must-be-disabled")
	if err := RunContext(context.Background(), "server"); err == nil {
		t.Fatal("corporate mode kept demo controls")
	}
	t.Setenv("DEMO_OPERATOR_TOKEN", "")
	t.Setenv("CATALOG_FILE", filepath.Join(t.TempDir(), "not-present"))
	if err := RunContext(context.Background(), "server"); err == nil {
		t.Fatal("embedded demo directory used in corporate mode")
	}
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"issuer": provider.URL, "authorization_endpoint": provider.URL + "/authorize", "token_endpoint": provider.URL + "/token", "jwks_uri": provider.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
	}))
	defer provider.Close()
	catalog := buddy.LoadCatalog()
	for i := range catalog.Profiles {
		catalog.Profiles[i].Issuer = provider.URL
		catalog.Profiles[i].Subject = "subject-" + catalog.Profiles[i].ID
	}
	file := filepath.Join(t.TempDir(), "trusted-catalog.json")
	raw, _ := json.Marshal(catalog)
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CATALOG_FILE", file)
	t.Setenv("AUTHORIZATION_BASE_URL", "https://authority.example")
	t.Setenv("AUTHORIZATION_TOKEN", "")
	if err := RunContext(context.Background(), "server"); err == nil {
		t.Fatal("missing resource authority accepted")
	}
	t.Setenv("AUTHORIZATION_TOKEN", "synthetic-authority-token")
	t.Setenv("OIDC_CLIENT_ID", "buddy-client")
	t.Setenv("OIDC_CLIENT_SECRET", "synthetic-client-secret")
	t.Setenv("OIDC_ISSUER", "http://unsafe-external.example")
	t.Setenv("OIDC_REDIRECT_URL", "https://buddy.example/api/login/callback")
	t.Setenv("OIDC_TRUSTED_ORIGINS", "")
	if err := RunContext(context.Background(), "server"); err == nil {
		t.Fatal("unsafe identity provider accepted")
	}
	t.Setenv("OIDC_ISSUER", provider.URL)
	address := freeAddress(t)
	t.Setenv("APP_ADDR", address)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunContext(ctx, "server") }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := http.Get("http://" + address + "/api/bootstrap")
		if err == nil {
			var result struct {
				Demo     bool
				Profiles []buddy.Profile
				LoginURL string `json:"login_url"`
			}
			err = json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			if err != nil || result.Demo || len(result.Profiles) != 0 || result.LoginURL != "/api/login" {
				t.Fatal(result, err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("corporate server did not start", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_MODE", "demo")
	t.Setenv("MODEL_DATA_POLICY", "invalid-policy")
	if err := RunContext(context.Background(), "server"); err == nil {
		t.Fatal("unknown processing policy accepted")
	}
	t.Setenv("MODEL_DATA_POLICY", "internal")
	t.Setenv("MODEL_BASE_URL", "https://approved-model.example/v1")
	t.Setenv("ALLOW_EXTERNAL_MODEL", "true")
	if err := RunContext(context.Background(), "server"); err == nil {
		t.Fatal("external model started without data policy")
	}
	t.Setenv("MODEL_BASE_URL", "http://127.0.0.1:11434")
	t.Setenv("CORPORATE_BASE_URL", "http://unsafe-corporate.example")
	if err := RunContext(context.Background(), "server"); err == nil {
		t.Fatal("unsafe corporate URL accepted")
	}
}

func TestConfiguredProcessesStartAndShutDown(t *testing.T) {
	db := testsupport.Database(t)
	t.Setenv("DATABASE_URL", db.Pool.Config().ConnString())
	t.Setenv("CORPORATE_TOKEN", "test-service")
	t.Setenv("DEMO_OPERATOR_TOKEN", "test-owner")
	t.Setenv("MODEL_PROVIDER", "fixture")
	corporateAddr, appAddr := freeAddress(t), freeAddress(t)
	t.Setenv("CORPORATE_ADDR", corporateAddr)
	t.Setenv("CORPORATE_BASE_URL", "http://"+corporateAddr)
	t.Setenv("APP_ADDR", appAddr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- RunContext(ctx, "corporate") }()
	go func() { results <- RunContext(ctx, "server") }()
	for _, url := range []string{"http://" + corporateAddr + "/health", "http://" + appAddr + "/api/health"} {
		deadline := time.Now().Add(3 * time.Second)
		for {
			resp, err := http.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode != 200 {
					t.Fatal(resp.StatusCode)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal(err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("shutdown stalled")
		}
	}
}

func TestConfigurationFailuresAndBindError(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := Run("server"); err == nil {
		t.Fatal("missing DB accepted")
	}
	t.Setenv("DATABASE_URL", "://invalid")
	if err := RunContext(context.Background(), "server"); err == nil {
		t.Fatal("bad DB accepted")
	}
	db := testsupport.Database(t)
	t.Setenv("DATABASE_URL", db.Pool.Config().ConnString())
	t.Setenv("CORPORATE_TOKEN", "")
	if err := RunContext(context.Background(), "server"); err == nil {
		t.Fatal("missing service token accepted")
	}
	t.Setenv("CORPORATE_TOKEN", "test")
	t.Setenv("MODEL_PROVIDER", "ollama")
	for _, value := range []string{"-1", "301", "invalid", "300"} {
		t.Setenv("MODEL_KEEP_ALIVE_SECONDS", value)
		if err := RunContext(context.Background(), "server"); err == nil {
			t.Fatal("invalid or non-CPU keep-alive accepted", value)
		}
	}
	t.Setenv("MODEL_KEEP_ALIVE_SECONDS", "0")
	t.Setenv("MODEL_PROVIDER", "unsupported")
	if err := RunContext(context.Background(), "server"); err == nil {
		t.Fatal("unsupported provider accepted")
	}
	server := &http.Server{Addr: "invalid address"}
	if err := Serve(context.Background(), server); err == nil {
		t.Fatal("bind failure ignored")
	}
}
