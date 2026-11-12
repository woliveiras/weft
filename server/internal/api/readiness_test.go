package api_test

import (
	"context"
	"errors"
	"github.com/woliveiras/weft/server/internal/storage"
	"os"
	"path/filepath"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
)

// writeDocker installs a docker CLI stand-in that answers `version` with
// the given exit code.
func writeDocker(t *testing.T, exit string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "docker")
	script := "#!/bin/sh\nif [ \"" + exit + "\" != 0 ]; then echo \"Cannot connect to the Docker daemon at $DOCKER_HOST\" >&2; exit 1; fi\necho 28.5.1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestReadinessReportsSandboxDaemon(t *testing.T) {
	var checks map[string]string

	srv := setup(t)
	if c := call(t, srv, "GET", "/readyz", "", "", "", &checks); c != 200 || checks["sandbox"] != "not configured" {
		t.Fatalf("unconfigured: %d %v", c, checks)
	}

	up, _ := setupApp(t, func(c *config.Config) {
		c.Isolation.Docker.Host = "tcp://sandbox-daemon:2376"
		c.Isolation.Docker.Bin = writeDocker(t, "0")
	})
	if c := call(t, up, "GET", "/readyz", "", "", "", &checks); c != 200 || checks["sandbox"] != "ok" {
		t.Fatalf("daemon up: %d %v", c, checks)
	}

	// A down sandbox daemon is reported, but the platform stays ready:
	// only docker-isolated agents depend on it.
	down, _ := setupApp(t, func(c *config.Config) {
		c.Isolation.Docker.Host = "tcp://sandbox-daemon:2376"
		c.Isolation.Docker.Bin = writeDocker(t, "1")
	})
	want := "unavailable"
	if c := call(t, down, "GET", "/readyz", "", "", "", &checks); c != 200 || checks["sandbox"] != want || checks["storage"] != "ok" {
		t.Fatalf("daemon down: %d %v", c, checks)
	}
}

type unavailablePing struct{ storage.Store }

func (unavailablePing) Ping(context.Context) error {
	return errors.New("postgres://user:synthetic-private-password@internal-db/private")
}

func TestReadinessHidesDependencyDetailsAndRecovers(t *testing.T) {
	srv, a := setupApp(t, nil)
	original := a.API.Store
	a.API.Store = unavailablePing{Store: original}
	var checks map[string]string
	if code := call(t, srv, "GET", "/readyz", "", "", "", &checks); code != 503 || checks["storage"] != "unavailable" || checks["queue"] != "ok" {
		t.Fatalf("%d %v", code, checks)
	}
	// Liveness and readiness have intentionally different contracts.
	if code := call(t, srv, "GET", "/healthz", "", "", "", nil); code != 200 {
		t.Fatal(code)
	}
	a.API.Store = original
	if code := call(t, srv, "GET", "/readyz", "", "", "", &checks); code != 200 || checks["storage"] != "ok" {
		t.Fatalf("%d %v", code, checks)
	}
}
