//go:build !windows

package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

type reuseWorkspaces struct{ root, prefix string }

func (w reuseWorkspaces) Ensure(_ context.Context, g *domain.Graph) (string, error) {
	dir := filepath.Join(w.root, g.Metadata.ID)
	return dir, os.MkdirAll(dir, 0755)
}
func (w reuseWorkspaces) HostPath(dir string) string { return w.prefix + filepath.Base(dir) }

func TestReuseRealSandbox(t *testing.T) {
	bin := os.Getenv("WEFT_TEST_ENV_DOCKER_BIN")
	if bin == "" {
		t.Skip("run make test-environment-context")
	}
	ctx := context.Background()
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("reuse-test-%d-", time.Now().UnixNano())
	ws := reuseWorkspaces{root: t.TempDir(), prefix: prefix}
	m, err := New(Options{Store: store, Workspaces: ws, DockerBin: bin, Capacity: &sandboxcapacity.Manager{Store: store, Scope: prefix, Policy: sandboxcapacity.Policy{MaxActive: 1, TotalMemory: "256m", DefaultMemory: "256m"}}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	graphs := []*domain.Graph{}
	shared := map[string]sharedEnvironment{}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, g := range graphs {
			if err := m.Reset(cleanup, g.Metadata.ID, "test"); err != nil {
				t.Error(err)
			}
			if err := m.docker(cleanup, nil, io.Discard, "volume", "rm", ws.prefix+g.Metadata.ID); err != nil {
				t.Error(err)
			}
		}
		// Only this test's uniquely named resources; production Reset retains seeds.
		for _, s := range shared {
			for _, c := range s.Caches {
				if err := m.docker(cleanup, nil, io.Discard, "volume", "rm", c.Volume); err != nil {
					t.Error(err)
				}
			}
			if err := m.docker(cleanup, nil, io.Discard, "rmi", s.Image); err != nil {
				t.Error(err)
			}
		}
	})
	build := func(id string) (*domain.Graph, *View) {
		t.Helper()
		g := &domain.Graph{Metadata: domain.GraphMetadata{ID: prefix + id}, Spec: domain.GraphSpec{Workspace: domain.WorkspaceSpec{Repo: "https://example.test/" + prefix}, Environment: &domain.EnvironmentSpec{Image: "alpine:3.22", Caches: []string{"/home/sandbox/.cache/pip"}, Network: domain.EnvironmentNetwork{Setup: "none"}, Setup: []string{"test \"$(cat /sys/fs/cgroup/memory.max)\" = 268435456", "test ! -e /home/sandbox/private-marker", "touch /home/sandbox/private-marker", "if test -f /home/sandbox/.cache/pip/dependency; then echo cache-seed-hit; else echo fixture-dependency > /home/sandbox/.cache/pip/dependency; fi", "echo installed > /workspace/installed"}}}}
		graphs = append(graphs, g)
		dir, e := ws.Ensure(ctx, g)
		if e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("fixture==1"), 0644); e != nil {
			t.Fatal(e)
		}
		if _, e := m.Build(ctx, g, false, "test"); e != nil {
			t.Fatal(e)
		}
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			v, e := m.Get(ctx, g)
			if e != nil {
				t.Fatal(e)
			}
			if v.State == StateFailed {
				t.Fatalf("%s: %s\n%s", id, v.Error, v.Log)
			}
			if v.State == StateReady {
				data, e := store.KVGet(ctx, reuseNamespace, v.ReuseKey)
				if e != nil {
					t.Fatal(e)
				}
				var s sharedEnvironment
				if e := json.Unmarshal(data, &s); e != nil {
					t.Fatal(e)
				}
				shared[v.ReuseKey] = s
				return g, v
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("build timeout")
		return nil, nil
	}
	run := func(g *domain.Graph, script string) {
		t.Helper()
		sb, e := m.SandboxFor(ctx, g)
		if e != nil {
			t.Fatal(e)
		}
		dir, e := ws.Ensure(ctx, g)
		if e != nil {
			t.Fatal(e)
		}
		args := []string{"run", "--rm", "--network", "none", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "-v", ws.HostPath(dir) + ":/workspace"}
		for _, v := range sb.Volumes {
			args = append(args, "-v", v)
		}
		args = append(args, "--entrypoint", "sh", sb.Image, "-ec", script)
		var out bytes.Buffer
		if e := m.docker(ctx, nil, &out, args...); e != nil {
			t.Fatalf("sandbox: %v %s", e, out.String())
		}
	}
	a, av := build("a")
	run(a, `test "$(id -u)" = 10001; test "$(cat /workspace/installed)" = installed; echo task-a > /workspace/private; echo mutated-by-a > /home/sandbox/.cache/pip/dependency`)
	b, bv := build("b")
	if av.ReuseDecision != "no_compatible_generation" || bv.ReuseDecision != "compatible" || av.Phase != "" || bv.Phase != "" {
		t.Fatalf("reuse presentation a=%+v b=%+v", av, bv)
	}
	if av.Reused || !bv.Reused || av.ReuseKey != bv.ReuseKey || av.Caches[0].Volume == bv.Caches[0].Volume {
		t.Fatalf("unexpected reuse a=%+v b=%+v", av, bv)
	}
	if !strings.Contains(bv.Log, "cache-seed-hit") {
		t.Fatal("second setup did not consume seeded dependencies")
	}
	run(b, `test ! -e /workspace/private; test "$(cat /workspace/installed)" = installed; test "$(cat /home/sandbox/.cache/pip/dependency)" = fixture-dependency; echo task-b > /workspace/private`)
	run(a, `test "$(cat /workspace/private)" = task-a; test "$(cat /home/sandbox/.cache/pip/dependency)" = mutated-by-a`)
	if err := m.Reset(ctx, a.Metadata.ID, "test"); err != nil {
		t.Fatal(err)
	}
	run(b, `test "$(cat /workspace/private)" = task-b; test "$(wc -l < /proc/net/route)" -eq 1`)
	c, cv := build("c")
	if !cv.Reused {
		t.Fatal("reset removed shared resources")
	}
	run(c, `test ! -e /workspace/private; test "$(cat /home/sandbox/.cache/pip/dependency)" = fixture-dependency`)
	t.Log("prepared image reused; setup replays per workspace; dependency seeds reused without cross-task writes; reset preserves another task and future reuse; uid 10001/network none verified")
}
