package app

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

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
	t.Setenv("MODEL_PROVIDER", "unsupported")
	if err := RunContext(context.Background(), "server"); err == nil {
		t.Fatal("unsupported provider accepted")
	}
	server := &http.Server{Addr: "invalid address"}
	if err := Serve(context.Background(), server); err == nil {
		t.Fatal("bind failure ignored")
	}
}
