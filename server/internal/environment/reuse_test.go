//go:build !windows

package environment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestReuseKeyBoundaries(t *testing.T) {
	base := []string{"https://example.test/repo", "definition", "lockfiles", "linux/arm64", "dedicated"}
	want := reuseKey(base[0], base[1], base[2], base[3], base[4])
	for i := range base {
		changed := append([]string(nil), base...)
		changed[i] += "changed"
		if reuseKey(changed[0], changed[1], changed[2], changed[3], changed[4]) == want {
			t.Fatalf("input %d did not invalidate key", i)
		}
	}
	if len(want) != 64 {
		t.Fatal(want)
	}
}

func TestReuseLeaseWaitCancellationAndFailure(t *testing.T) {
	h := setup(t)
	ctx := context.Background()
	if ok, err := h.store.AcquireLease(ctx, "environment-reuse/busy", "other", leaseTTL); err != nil || !ok {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(progressContext(ctx), 30*time.Millisecond)
	defer cancel()
	called := false
	err := h.m.withReuseLease(bounded, "busy", io.Discard, func(context.Context) error { called = true; return nil })
	if !errors.Is(err, context.DeadlineExceeded) || called {
		t.Fatalf("%v called=%v", err, called)
	}
	var progress Status
	snapshotProgress(bounded, &progress)
	if progress.Phase != "waiting_reuse" {
		t.Fatalf("missing reuse wait: %+v", progress)
	}
	failure := errors.New("build failed")
	if err := h.m.withReuseLease(ctx, "free", io.Discard, func(context.Context) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := h.m.withReuseLease(ctx, "free", io.Discard, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibleBuildsReuseAndResetPreservesSharedResources(t *testing.T) {
	h := setup(t)
	// Deterministic daemon platform response, using the same CLI boundary as builds.
	script, err := os.ReadFile(h.docker.bin)
	if err != nil {
		t.Fatal(err)
	}
	script = []byte(strings.Replace(string(script), "case \"$sub\" in", "case \"$sub\" in\n  version) echo linux/arm64 ;;", 1))
	if err := os.WriteFile(h.docker.bin, script, 0755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first := graphWith(&domain.EnvironmentSpec{Image: "alpine:3.22", Caches: []string{"/home/sandbox/.cache/pip", "/home/sandbox/.cache/custom"}})
	first.Spec.Workspace.Repo = "https://example.test/repo.git"
	second := *first
	second.Metadata.ID = "second"
	// Independent manager instances cooperate through storage leases.
	otherManager, err := New(h.m.opt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Build(ctx, first, false, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := otherManager.Build(ctx, &second, false, "test"); err != nil {
		t.Fatal(err)
	}
	a, b := h.wait(t, first), h.wait(t, &second)
	data, err := h.store.KVGet(ctx, reuseNamespace, a.ReuseKey)
	if err != nil {
		t.Fatal(err)
	}
	var seed sharedEnvironment
	if err := json.Unmarshal(data, &seed); err != nil {
		t.Fatal(err)
	}
	if len(seed.Caches) != 1 || seed.Caches[0].Path != "/home/sandbox/.cache/pip" || len(a.Caches) != 2 || a.Caches[1].Volume == b.Caches[1].Volume {
		t.Fatal("unknown caches must stay private")
	}
	if a.State != StateReady || b.State != StateReady || a.Image != b.Image || a.Reused == b.Reused {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
	if a.Caches[0].Volume == b.Caches[0].Volume {
		t.Fatal("tasks share writable cache")
	}
	calls := strings.Join(h.docker.calls(t), "\n")
	if strings.Count(calls, "pull alpine:3.22") != 1 {
		t.Fatal(calls)
	}
	if !strings.Contains(calls, ":/tmp/weft-cache-seed:ro") {
		t.Fatal("seed is not read-only")
	}
	before := len(h.docker.calls(t))
	if err := h.m.Reset(ctx, first.Metadata.ID, "test"); err != nil {
		t.Fatal(err)
	}
	reset := strings.Join(h.docker.calls(t)[before:], "\n")
	if strings.Contains(reset, "rmi -f "+a.Image) || strings.Contains(reset, "weft.shared=") {
		t.Fatal(reset)
	}
	if err := h.m.Check(ctx, &second); err != nil {
		t.Fatal(err)
	}
	// Manifest changes invalidate a previously ready environment and select new caches.
	if err := os.WriteFile(filepath.Join(h.ws, "go.sum"), []byte("fixture-v2"), 0644); err != nil {
		t.Fatal(err)
	}
	v, err := h.m.Get(ctx, &second)
	if err != nil || v.UpToDate {
		t.Fatalf("stale: %+v %v", v, err)
	}
	if len(v.ChangeReasons) != 1 || v.ChangeReasons[0] != "repository_inputs_changed" {
		t.Fatalf("input change reason: %+v", v)
	}
	if _, err := h.m.Build(ctx, &second, false, "test"); err != nil {
		t.Fatal(err)
	}
	next := h.wait(t, &second)
	if len(next.BuildReasons) != 1 || next.BuildReasons[0] != "repository_inputs_changed" {
		t.Fatalf("build reason: %+v", next)
	}
	if next.State != StateReady || next.Reused || next.ReuseKey == b.ReuseKey || next.Caches[0].Volume == b.Caches[0].Volume {
		t.Fatalf("invalidated %+v", next)
	}
	// Another repository cannot consume the first repository's resources.
	other := second
	other.Metadata.ID = "other"
	other.Spec.Workspace.Repo = "https://example.test/other.git"
	if _, err := h.m.Build(ctx, &other, false, "test"); err != nil {
		t.Fatal(err)
	}
	if v := h.wait(t, &other); v.State != StateReady || v.Reused || v.ReuseKey == next.ReuseKey {
		t.Fatalf("scope %+v", v)
	}
	// A missing shared image is an explicit dependency error, not a silent hit.
	third := second
	third.Metadata.ID = "third"
	h.docker.set(t, "fail-image-inspect", "")
	if _, err := h.m.Build(ctx, &third, false, "test"); err != nil {
		t.Fatal(err)
	}
	if v := h.wait(t, &third); v.State != StateFailed || !strings.Contains(v.Error, "shared image unavailable") {
		t.Fatalf("failure %+v", v)
	}
	// Force bypasses the broken shared record and publishes a new generation.
	if _, err := h.m.Build(ctx, &third, true, "test"); err != nil {
		t.Fatal(err)
	}
	if v := h.wait(t, &third); v.State != StateReady || v.Reused {
		t.Fatalf("force %+v", v)
	}
}

func TestSharedBuildRejectsAliasedWorkspace(t *testing.T) {
	h := setup(t)
	g := graphWith(&domain.EnvironmentSpec{Image: "alpine"})
	g.Spec.Workspace = domain.WorkspaceSpec{Repo: "https://example.test/repo", Path: "same"}
	if _, err := h.m.Build(context.Background(), g, false, "test"); err != nil {
		t.Fatal(err)
	}
	if v := h.wait(t, g); v.State != StateFailed || !strings.Contains(v.Error, "isolated graph workspace") {
		t.Fatalf("%+v", v)
	}
	if len(h.docker.calls(t)) != 0 {
		t.Fatal("docker started for an aliased workspace")
	}
}

func TestReuseDoesNotPublishFailedSetupOrHideCacheFailures(t *testing.T) {
	h := setup(t)
	script, err := os.ReadFile(h.docker.bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.docker.bin, []byte(strings.Replace(string(script), "case \"$sub\" in", "case \"$sub\" in\n  version) echo linux/arm64 ;;", 1)), 0755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	g := graphWith(&domain.EnvironmentSpec{Image: "alpine", Setup: []string{"echo fixture"}, Caches: []string{"/home/sandbox/.cache/pip"}})
	g.Spec.Workspace.Repo = "https://example.test/repo"
	h.docker.set(t, "fail-run", "")
	if _, err := h.m.Build(ctx, g, false, "test"); err != nil {
		t.Fatal(err)
	}
	v := h.wait(t, g)
	if v.State != StateFailed {
		t.Fatalf("%+v", v)
	}
	if _, err := h.store.KVGet(ctx, reuseNamespace, v.ReuseKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("failed setup published a seed: %v", err)
	}
	calls := strings.Join(h.docker.calls(t), "\n")
	if !strings.Contains(calls, "volume rm weft-env-shared_") || !strings.Contains(calls, "rmi weft-shared/") {
		t.Fatalf("failed build resources not released: %s", calls)
	}
	if err := os.Remove(filepath.Join(h.docker.dir, "fail-run")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Build(ctx, g, false, "test"); err != nil {
		t.Fatal(err)
	}
	v = h.wait(t, g)
	if v.State != StateReady || v.Reused {
		t.Fatalf("retry %+v", v)
	}
	changed := *g
	changed.Spec.Workspace.Repo = "https://example.test/another"
	if v, err := h.m.Get(ctx, &changed); err != nil || v.UpToDate {
		t.Fatalf("repository change %+v %v", v, err)
	}
	next := *g
	next.Metadata.ID = "next"
	h.docker.set(t, "fail-volume-inspect", "")
	if _, err := h.m.Build(ctx, &next, false, "test"); err != nil {
		t.Fatal(err)
	}
	if v := h.wait(t, &next); v.State != StateFailed || !strings.Contains(v.Error, "shared cache unavailable") {
		t.Fatalf("missing seed %+v", v)
	}
	if err := os.Remove(filepath.Join(h.docker.dir, "fail-volume-inspect")); err != nil {
		t.Fatal(err)
	}
	h.docker.set(t, "fail-run", "")
	if _, err := h.m.Build(ctx, &next, false, "test"); err != nil {
		t.Fatal(err)
	}
	if v := h.wait(t, &next); v.State != StateFailed || !strings.Contains(v.Error, "copy compatible cache") {
		t.Fatalf("copy failure %+v", v)
	}
}

func TestCacheSeedEligibility(t *testing.T) {
	for _, dir := range []string{"/home/sandbox/.cache/pip/", "/home/sandbox/go/pkg/mod", "/home/sandbox/.npm/_cacache"} {
		if !shareableCache(dir) {
			t.Fatalf("known cache %s", dir)
		}
	}
	for _, dir := range []string{"/home/sandbox", "/home/sandbox/.cache", "/home/sandbox/.npm", "/home/sandbox/.ssh", "/custom", "/home/sandbox/.cache/pip/../credentials"} {
		if shareableCache(dir) {
			t.Fatalf("unsafe/unknown seed %s", dir)
		}
	}
}
