package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/tools"
)

// Run the compiled test inside the dedicated daemon (the same convention as
// TestDefaultSandboxHardeningRealDaemon). No provider or remote Git is used.
func TestTaskCheckoutGitRealDaemon(t *testing.T) {
	if os.Getenv("WEFT_TEST_TASK_CHECKOUT") != "1" {
		t.Skip("requires compiled fixture inside the dedicated sandbox daemon")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture Git: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(source, "source.txt"), []byte("reviewed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "base")
	store, err := fsstore.Open(filepath.Join(root, "store"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	m := projectconfig.Manager{Store: store}
	p, err := m.Create(ctx, projectconfig.Input{Name: "Fixture", Path: source}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := m.CreateTaskCheckout(ctx, p, p.Head, root, "task-run-sandbox", "admin")
	if err != nil {
		t.Fatal(err)
	}
	// weftd and the default sandbox both run as 10001. The compiled test
	// runs as daemon root solely to access its Docker socket; reproduce the
	// production workspace owner without making private directories public.
	if err = filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		return os.Chown(path, 10001, 10001)
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Isolation.Docker.Host = "unix:///var/run/docker.sock"
	cfg.Isolation.Docker.Bin = filepath.Join(root, "docker-no-pull")
	if err = os.WriteFile(cfg.Isolation.Docker.Bin, []byte("#!/bin/sh\nif [ \"$1\" = run ]; then shift; exec docker run --pull=never \"$@\"; fi\nexec docker \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("weft-task-checkout-test-%d", time.Now().UnixNano())
	sb, err := sandboxFactory(cfg, nil, store, events.NewBus(store, slog.Default()))(ctx, engine.SandboxSpec{Name: name, Graph: &domain.Graph{Metadata: domain.GraphMetadata{ID: name}}, Node: &domain.Node{ID: "worker", Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}}, HostWorkdir: dir, Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sb.Close(); err != nil {
			t.Error(err)
		}
	})
	// Native Git tools use this very sandbox. Run actual branch/stage/commit
	// operations after the mount path changed from the daemon to /workspace.
	result, err := sb.Exec(ctx, tools.ExecRequest{Shell: "set -eu; git rev-parse HEAD; git log -1 --format=%s; git branch weft/fixture; printf 'task change\\n' > source.txt; git add source.txt; git -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm 'task change'; git status --porcelain"})
	if err != nil || result.ExitCode != 0 || !strings.Contains(result.Stdout, p.Head) {
		t.Fatalf("sandbox Git failed: %+v %v", result, err)
	}
	if run("rev-parse", "HEAD") != p.Head || run("status", "--porcelain") != "" {
		t.Fatal("sandbox changed source")
	}
	probe, err := sb.Exec(ctx, tools.ExecRequest{Argv: []string{"test", "!", "-e", source}})
	if err != nil || probe.ExitCode != 0 {
		t.Fatal("source mounted in sandbox", probe, err)
	}
	out, err := tools.DockerCommand(ctx, cfg.Isolation.Docker.Bin, cfg.Isolation.Docker.Host, "inspect", "--format", "{{json .Mounts}}", name).Output()
	if err != nil {
		t.Fatal(err)
	}
	var mounts []struct{ Source, Destination string }
	if err = json.Unmarshal(out, &mounts); err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 || mounts[0].Source != dir || mounts[0].Destination != "/workspace" {
		t.Fatal("unexpected task mounts", mounts)
	}
}
