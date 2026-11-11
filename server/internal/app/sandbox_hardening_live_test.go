//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestDefaultSandboxHardeningRealDaemon(t *testing.T) {
	if os.Getenv("WEFT_TEST_SANDBOX_HARDENING") != "1" {
		t.Skip("run make test-sandbox")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()

	// The test binary runs in sandbox-daemon, so its workspace bind source
	// is visible to Docker. Refuse pulls even if the cached image disappears.
	cfg := config.Default()
	cfg.Isolation.Docker.Host = "unix:///var/run/docker.sock"
	cfg.Isolation.Docker.Bin = filepath.Join(t.TempDir(), "docker-no-pull")
	if err := os.WriteFile(cfg.Isolation.Docker.Bin, []byte(`#!/bin/sh
if [ "$1" = run ]; then
	shift
	exec docker run --pull=never "$@"
fi
exec docker "$@"
`), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	workspace := t.TempDir()
	name := fmt.Sprintf("weft-sbx-hardening-test-%d", time.Now().UnixNano())
	factory := sandboxFactory(cfg, nil, store, events.NewBus(store, slog.Default()))
	sb, err := factory(ctx, engine.SandboxSpec{
		Name:        name,
		Graph:       &domain.Graph{Metadata: domain.GraphMetadata{ID: name}},
		Node:        &domain.Node{ID: "worker", Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}},
		HostWorkdir: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sb.Close(); err != nil {
			t.Error(err)
		}
	})

	const format = `{"running":{{.State.Running}},"capDrop":{{json .HostConfig.CapDrop}},"securityOpt":{{json .HostConfig.SecurityOpt}},"network":{{json .HostConfig.NetworkMode}},"mounts":{{json .Mounts}}}`
	out, err := tools.DockerCommand(ctx, cfg.Isolation.Docker.Bin, cfg.Isolation.Docker.Host,
		"inspect", "--format", format, name).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect sandbox: %v: %s", err, out)
	}
	var inspected struct {
		Running     bool
		CapDrop     []string
		SecurityOpt []string
		Network     string
		Mounts      []struct{ Type, Source, Destination string }
	}
	if err := json.Unmarshal(out, &inspected); err != nil {
		t.Fatalf("decode sandbox inspection: %v: %s", err, out)
	}
	if !inspected.Running || !slices.Contains(inspected.CapDrop, "ALL") ||
		!slices.Contains(inspected.SecurityOpt, "no-new-privileges") || inspected.Network != "none" ||
		len(inspected.Mounts) != 1 || inspected.Mounts[0].Type != "bind" ||
		inspected.Mounts[0].Source != workspace || inspected.Mounts[0].Destination != "/workspace" {
		t.Fatalf("unexpected default sandbox configuration: %s", out)
	}
	t.Logf("Docker inspect: %s", strings.TrimSpace(strings.ReplaceAll(string(out), workspace, "<fixture-workspace>")))

	result, err := sb.Exec(ctx, tools.ExecRequest{Shell: "awk '/^(CapEff|CapBnd|NoNewPrivs):/ {print $1, $2}' /proc/self/status"})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("read sandbox process status: %+v %v", result, err)
	}
	status := strings.Join(strings.Fields(result.Stdout), " ")
	if status != "CapEff: 0000000000000000 CapBnd: 0000000000000000 NoNewPrivs: 1" {
		t.Fatalf("unexpected sandbox process status: %s", status)
	}
	t.Logf("Sandbox process: %s", status)
}
