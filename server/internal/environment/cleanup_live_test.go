//go:build !windows

package environment

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestSharedCleanupRealSandbox(t *testing.T) {
	bin := os.Getenv("WEFT_TEST_ENV_DOCKER_BIN")
	if bin == "" {
		t.Skip("run make test-environment-context")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("cleanup-test-%d-", time.Now().UnixNano())
	ws := reuseWorkspaces{root: t.TempDir(), prefix: prefix + "workspace-"}
	m, err := New(Options{Store: store, Workspaces: ws, DockerBin: bin, Capacity: &sandboxcapacity.Manager{Store: store, Scope: prefix, Policy: sandboxcapacity.Policy{MaxActive: 1, TotalMemory: "256m", DefaultMemory: "256m"}}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	graphs := []*domain.Graph{}
	holder := prefix + "suspended"
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		// This name belongs only to this test; rm -f is for fixture teardown, never GC.
		_ = m.docker(cleanup, nil, io.Discard, "rm", "-f", holder)
		for _, g := range graphs {
			if err := m.Reset(cleanup, g.Metadata.ID, "test"); err != nil {
				t.Error(err)
			}
		}
		preview, err := m.CleanupShared(cleanup, nil, true, "test")
		if err != nil {
			t.Error(err)
		} else {
			for _, item := range preview.Generations {
				result, err := m.CleanupShared(cleanup, []string{item.ID}, false, "test")
				if err != nil {
					t.Error(err)
				} else if !result.Generations[0].Removed {
					t.Errorf("fixture cleanup: %+v", result)
				}
			}
		}
		for _, g := range graphs {
			if err := m.docker(cleanup, nil, io.Discard, "volume", "rm", ws.prefix+g.Metadata.ID); err != nil {
				t.Error(err)
			}
		}
	})
	build := func(g *domain.Graph) *View {
		t.Helper()
		if _, err := m.Build(ctx, g, false, "test"); err != nil {
			t.Fatal(err)
		}
		for {
			v, err := m.Get(ctx, g)
			if err != nil {
				t.Fatal(err)
			}
			if v.State == StateFailed {
				t.Fatalf("%s\n%s", v.Error, v.Log)
			}
			if v.State == StateReady {
				return v
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	for _, suffix := range []string{"a", "b"} {
		g := graphWith(&domain.EnvironmentSpec{Image: "alpine:3.22", Caches: []string{"/home/sandbox/.cache/pip"}, Network: domain.EnvironmentNetwork{Setup: "none"}, Setup: []string{"echo installed > /workspace/installed", "echo dependency > /home/sandbox/.cache/pip/dependency"}})
		g.Metadata.ID = prefix + suffix
		g.Spec.Workspace.Repo = "https://example.test/" + prefix
		graphs = append(graphs, g)
	}
	a, b := graphs[0], graphs[1]
	av, bv := build(a), build(b)
	if av.SharedImage != bv.SharedImage || !bv.Reused {
		t.Fatal("fixture did not reuse generation")
	}
	id := generationImage.FindStringSubmatch(av.SharedImage)[1]
	collect := func() CleanupItem {
		t.Helper()
		result, err := m.CleanupShared(ctx, []string{id}, false, "test")
		if err != nil {
			t.Fatal(err)
		}
		return result.Generations[0]
	}
	if item := collect(); item.Removed || item.Eligible || len(item.UsedBy) < 2 {
		t.Fatalf("live graph references: %+v", item)
	}
	if err := m.Reset(ctx, a.Metadata.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if item := collect(); item.Removed || item.Eligible {
		t.Fatalf("other graph lost resources: %+v", item)
	}
	// The surviving task still sees its isolated setup and dependency files.
	var output bytes.Buffer
	if err := m.docker(ctx, nil, &output, "run", "--rm", "--network", "none", "-v", ws.prefix+b.Metadata.ID+":/workspace", "-v", bv.Caches[0].Volume+":"+bv.Caches[0].Path, "--entrypoint", "sh", bv.Image, "-ec", `test "$(cat /workspace/installed)" = installed; test "$(cat /home/sandbox/.cache/pip/dependency)" = dependency; echo preserved > /workspace/private-marker`); err != nil {
		t.Fatalf("%v %s", err, output.String())
	}
	preview, err := m.CleanupShared(ctx, []string{id}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	seed := preview.Generations[0].Caches[0].Volume
	if err := m.docker(ctx, nil, io.Discard, "create", "--name", holder, "--network", "none", "-v", seed+":/seed:ro", av.SharedImage, "true"); err != nil {
		t.Fatal(err)
	}
	if err := m.Reset(ctx, b.Metadata.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if item := collect(); item.Removed || len(item.UsedBy) != 1 || !strings.HasPrefix(item.UsedBy[0], "container:") {
		t.Fatalf("stopped container lost resource: %+v", item)
	}
	if err := m.docker(ctx, nil, io.Discard, "rm", holder); err != nil {
		t.Fatal(err)
	}
	preview, err = m.CleanupShared(ctx, []string{id}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Generations[0].Eligible || preview.Generations[0].Removed {
		t.Fatalf("%+v", preview)
	}
	if item := collect(); !item.Removed || item.Error != "" {
		t.Fatalf("%+v", item)
	}
	if exists, err := m.resourceExists(ctx, "image", av.SharedImage); err != nil || exists {
		t.Fatalf("image retained: %v %v", exists, err)
	}
	if exists, err := m.resourceExists(ctx, "volume", seed); err != nil || exists {
		t.Fatalf("seed retained: %v %v", exists, err)
	}
	rebuilt := build(b)
	if rebuilt.Reused || rebuilt.SharedImage == bv.SharedImage {
		t.Fatal("cleanup left stale reuse pointer")
	}
	if err := m.docker(ctx, nil, io.Discard, "run", "--rm", "--network", "none", "-v", ws.prefix+b.Metadata.ID+":/workspace", "--entrypoint", "sh", rebuilt.Image, "-ec", `test "$(cat /workspace/private-marker)" = preserved`); err != nil {
		t.Fatal("workspace lost", err)
	}
	t.Log("real shared cleanup preserved another task and a stopped container; removed only unused image/seed; rebuild after cleanup succeeded with workspace intact")
}
