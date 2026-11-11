package e2e

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/app"
	"github.com/Astarino/ai-buddy/internal/testsupport"
)

func address(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	return addr
}

func TestBrowserJourney(t *testing.T) {
	if os.Getenv("RUN_BROWSER_E2E") != "true" {
		t.Skip("run make e2e for isolated browser tests")
	}
	db := testsupport.Database(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	appAddress, corpAddress := address(t), address(t)
	t.Setenv("DATABASE_URL", db.Pool.Config().ConnString())
	t.Setenv("APP_ADDR", appAddress)
	t.Setenv("CORPORATE_ADDR", corpAddress)
	t.Setenv("CORPORATE_BASE_URL", "http://"+corpAddress)
	t.Setenv("CORPORATE_TOKEN", "e2e-service")
	t.Setenv("DEMO_OPERATOR_TOKEN", "e2e-owner")
	t.Setenv("MODEL_PROVIDER", "fixture")
	t.Setenv("WEB_DIR", filepath.Join(root, "web/dist"))
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan error, 2)
	go func() { results <- app.RunContext(ctx, "corporate") }()
	go func() { results <- app.RunContext(ctx, "server") }()
	t.Cleanup(func() {
		cancel()
		for i := 0; i < 2; i++ {
			select {
			case err := <-results:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(6 * time.Second):
				t.Error("server shutdown timed out")
			}
		}
	})
	for _, path := range []string{"http://" + corpAddress + "/health", "http://" + appAddress + "/api/health"} {
		deadline := time.Now().Add(5 * time.Second)
		for {
			response, err := http.Get(path)
			if err == nil {
				response.Body.Close()
				if response.StatusCode != 200 {
					t.Fatal(response.StatusCode)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal(err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	command := exec.CommandContext(ctx, "pnpm", "--dir", filepath.Join(root, "web"), "test:e2e")
	command.Env = append(os.Environ(), "E2E_BASE_URL=http://"+appAddress, "E2E_OPERATOR_TOKEN=e2e-owner")
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err = command.Run(); err != nil {
		t.Fatal(err)
	}
}
