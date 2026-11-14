package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Astarino/ai-buddy/internal/authorization"
	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/corporate"
	"github.com/Astarino/ai-buddy/internal/httpapi"
	"github.com/Astarino/ai-buddy/internal/identity"
	"github.com/Astarino/ai-buddy/internal/model"
	"github.com/Astarino/ai-buddy/internal/outbound"
	"github.com/Astarino/ai-buddy/internal/publicdocs"
	"github.com/Astarino/ai-buddy/internal/storage"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func Run(kind string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return RunContext(ctx, kind)
}

func RunContext(ctx context.Context, kind string) error {
	mode := env("APP_MODE", "demo")
	if mode != "demo" && mode != "corporate" {
		return fmt.Errorf("APP_MODE must be demo or corporate")
	}
	if mode == "corporate" && kind == "corporate" {
		return fmt.Errorf("the simulator is available only in demo mode")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	open := storage.Open
	if mode == "corporate" {
		open = storage.OpenExisting
	}
	db, err := open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer db.Close()
	token, operator := os.Getenv("CORPORATE_TOKEN"), os.Getenv("DEMO_OPERATOR_TOKEN")
	if token == "" {
		return fmt.Errorf("CORPORATE_TOKEN is required")
	}
	var server *http.Server
	if kind == "corporate" {
		server = httpapi.HTTPServer(env("CORPORATE_ADDR", "127.0.0.1:8091"), corporate.NewServer(db, token, operator))
	} else {
		provider := env("MODEL_PROVIDER", "ollama")
		llm, err := model.New(provider, env("MODEL_BASE_URL", "http://127.0.0.1:11434"), env("MODEL_NAME", "qwen3.5:4b"), os.Getenv("MODEL_API_KEY"), os.Getenv("ALLOW_EXTERNAL_MODEL") == "true")
		if err != nil {
			return err
		}
		if client, ok := llm.(*model.Client); ok {
			client.CPUOnly = os.Getenv("MODEL_CPU_ONLY") == "true"
		}
		base := env("CORPORATE_BASE_URL", "http://127.0.0.1:8091")
		if _, err := outbound.ValidateBaseURL(base); err != nil {
			return err
		}
		corp := corporate.NewClient(base, token, operator)
		api := &httpapi.API{Service: buddy.NewService(db, corp, llm), Sessions: db, DemoToken: operator, StaticDir: env("WEB_DIR", "web/dist"), SecureCookie: os.Getenv("SECURE_COOKIE") == "true"}
		if env("PUBLIC_SEARCH_ENABLED", "true") == "true" {
			api.Service.Searcher = publicdocs.New()
		}
		policy := env("MODEL_DATA_POLICY", "internal")
		if policy != "internal" && policy != "public-only" {
			return fmt.Errorf("unknown MODEL_DATA_POLICY")
		}
		if llm.Boundary().External && policy != "public-only" {
			return fmt.Errorf("external model requires MODEL_DATA_POLICY=public-only")
		}
		api.Service.ModelDataPolicy = policy
		if mode == "corporate" {
			if operator != "" || token == "local-demo-service" || provider == "fixture" || !api.SecureCookie {
				return fmt.Errorf("corporate mode requires secure cookies, real service credentials and no demo controls")
			}
			catalog, err := buddy.ReadCatalog(os.Getenv("CATALOG_FILE"))
			if err != nil {
				return fmt.Errorf("corporate mode requires a trusted catalog: %w", err)
			}
			permissions, err := authorization.New(os.Getenv("AUTHORIZATION_BASE_URL"), os.Getenv("AUTHORIZATION_TOKEN"))
			if err != nil {
				return fmt.Errorf("authorization service configuration is required")
			}
			trustedOrigins := []string{}
			if raw := os.Getenv("OIDC_TRUSTED_ORIGINS"); raw != "" {
				trustedOrigins = strings.Split(raw, ",")
			}
			login, err := identity.New(ctx, identity.Config{Issuer: os.Getenv("OIDC_ISSUER"), ClientID: os.Getenv("OIDC_CLIENT_ID"), ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"), RedirectURL: os.Getenv("OIDC_REDIRECT_URL"), TrustedOrigins: trustedOrigins}, db, nil)
			if err != nil {
				return err
			}
			api.Production, api.Login = true, login
			api.Service.Catalog, api.Service.Authorizer, api.Service.Demo, api.Service.Operator = catalog, permissions, false, nil
		}
		server = httpapi.HTTPServer(env("APP_ADDR", "127.0.0.1:8090"), api.Handler())
		slog.Info("model configured", "provider", provider, "model", llm.Name(), "external_fallback", false)
	}
	return Serve(ctx, server)
}

func Serve(ctx context.Context, server *http.Server) error {
	finished := make(chan error, 1)
	go func() { slog.Info("listening", "address", server.Addr); finished <- server.ListenAndServe() }()
	select {
	case err := <-finished:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
