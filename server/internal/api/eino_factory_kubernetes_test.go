//go:build kubernetes_e2e

package api_test

import (
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
)

// Run in the fixture controller pod with a shared claim and the namespace Role.
func TestKubernetesEinoFactory(t *testing.T) {
	workspace := os.Getenv("WEFT_FIXTURE_WORKSPACES")
	if workspace == "" {
		t.Fatal("set WEFT_FIXTURE_WORKSPACES to the mounted fixture claim")
	}
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		t.Fatal(err)
	}
	srv, a := setupApp(t, func(cfg *config.Config) {
		cfg.Server.InternalURL = "http://factory-controller:8080"
		cfg.Queue = config.Queue{Type: "fs", Dir: t.TempDir()}
		cfg.Isolation.Kubernetes = &config.KubernetesIsolation{Image: "weft/sandbox-go:dev"}
		cfg.Workspaces.Root = workspace
		cfg.Runtimes.SharedSecretEnv = "WEFT_RUNTIME_SECRET"
	})
	cb := &http.Server{Handler: a.API.Handler()}
	go func() {
		if err := cb.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Errorf("callback: %v", err)
		}
	}()
	t.Cleanup(func() { cb.Close() })
	exerciseEinoFactory(t, srv, a, "http://weft-agents:8000", nil)
}
