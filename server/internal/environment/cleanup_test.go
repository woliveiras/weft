//go:build !windows

package environment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

func cleanupFixture(t *testing.T, h *harness, id string) sharedGeneration {
	t.Helper()
	g := sharedGeneration{ID: id, Key: strings.Repeat("a", 64), Daemon: h.m.cleanupDaemon(), sharedEnvironment: sharedEnvironment{Image: "weft-shared/" + id + ":ready-user", Caches: []CacheMount{{Path: "/home/sandbox/.cache/pip", Volume: CacheVolume(id, "/home/sandbox/.cache/pip")}}}}
	if err := h.m.trackGeneration(context.Background(), g.Key, g.sharedEnvironment); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(g.sharedEnvironment)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.store.KVPut(context.Background(), reuseNamespace, g.Key, data); err != nil {
		t.Fatal(err)
	}
	return g
}
func cleanupOne(t *testing.T, h *harness, g sharedGeneration, dry bool) CleanupItem {
	t.Helper()
	result, err := h.m.CleanupShared(context.Background(), []string{g.ID}, dry, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Generations) != 1 {
		t.Fatalf("%+v", result)
	}
	return result.Generations[0]
}
func TestSharedGenerationOwnership(t *testing.T) {
	good := sharedGeneration{ID: "shared_test", Key: strings.Repeat("a", 64), sharedEnvironment: sharedEnvironment{Image: "weft-shared/shared_test:ready-user"}}
	if err := validateGeneration(good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*sharedGeneration){
		func(g *sharedGeneration) { g.Image = "alpine:latest" },
		func(g *sharedGeneration) { g.ID = "shared_other" },
		func(g *sharedGeneration) { g.Key = "other" },
		func(g *sharedGeneration) { g.Caches = []CacheMount{{Path: "/workspace", Volume: "user-data"}} },
		func(g *sharedGeneration) {
			g.Caches = []CacheMount{{Path: "/home/sandbox/.cache/pip", Volume: "other-task-cache"}}
		},
	} {
		bad := good
		change(&bad)
		if err := validateGeneration(bad); err == nil {
			t.Fatalf("unsafe ownership accepted: %+v", bad)
		}
	}
}

func TestCleanupReferencesAndExplicitSelectionIntegration(t *testing.T) {
	h := setup(t)
	g := cleanupFixture(t, h, "shared_test")
	if _, err := h.m.CleanupShared(context.Background(), nil, false, "test"); !errors.Is(err, ErrInvalidCleanup) {
		t.Fatal(err)
	}
	if _, err := h.m.CleanupShared(context.Background(), []string{"user-volume"}, false, "test"); !errors.Is(err, ErrInvalidCleanup) {
		t.Fatal(err)
	}
	if err := h.m.save(context.Background(), &Status{Graph: "other", State: StateReady, SharedImage: g.Image}); err != nil {
		t.Fatal(err)
	}
	if item := cleanupOne(t, h, g, false); item.Removed || item.Eligible || len(item.UsedBy) != 1 {
		t.Fatalf("%+v", item)
	}
	if err := h.store.KVDelete(context.Background(), kvNamespace, "other"); err != nil {
		t.Fatal(err)
	}
	before := len(h.docker.calls(t))
	if item := cleanupOne(t, h, g, true); !item.Eligible || item.Removed {
		t.Fatalf("%+v", item)
	}
	for _, call := range h.docker.calls(t)[before:] {
		if strings.HasPrefix(call, "rmi ") || strings.Contains(call, "volume rm") {
			t.Fatal("preview deleted resources")
		}
	}
	if item := cleanupOne(t, h, g, false); !item.Removed || item.Error != "" {
		t.Fatalf("%+v", item)
	}
	if _, err := h.store.KVGet(context.Background(), reuseNamespace, g.Key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("stale reuse pointer")
	}
	if _, err := h.store.KVGet(context.Background(), generationsNamespace, g.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("stale generation")
	}
	calls := strings.Join(h.docker.calls(t), "\n")
	if strings.Contains(calls, "rmi -f") || strings.Contains(calls, "volume rm -f") || strings.Contains(calls, "prune") {
		t.Fatal(calls)
	}
}

func TestCleanupDependencyFailuresAndRetry(t *testing.T) {
	for _, failure := range []string{"ps", "volume-inspect", "volume-rm", "rmi"} {
		t.Run(failure, func(t *testing.T) {
			h := setup(t)
			g := cleanupFixture(t, h, "shared_failure")
			h.docker.set(t, "fail-"+failure, "")
			item := cleanupOne(t, h, g, false)
			if item.Removed || item.Error == "" {
				t.Fatalf("%+v", item)
			}
			if _, err := h.store.KVGet(context.Background(), generationsNamespace, g.ID); err != nil {
				t.Fatal("lost retry record")
			}
			if err := os.Remove(filepath.Join(h.docker.dir, "fail-"+failure)); err != nil {
				t.Fatal(err)
			}
			if item := cleanupOne(t, h, g, false); !item.Removed {
				t.Fatalf("%+v", item)
			}
		})
	}
}
func TestCleanupProtectsBuildsLegacyRecordsAndContainers(t *testing.T) {
	for _, kind := range []string{"build", "legacy", "image", "volume", "invalid-inventory", "inspect-failed"} {
		t.Run(kind, func(t *testing.T) {
			h := setup(t)
			g := cleanupFixture(t, h, "shared_protected")
			ctx := context.Background()
			switch kind {
			case "build":
				if ok, err := h.store.AcquireLease(ctx, leasePrefix+"building", "instance", time.Minute); err != nil || !ok {
					t.Fatal(err)
				}
			case "legacy":
				if err := h.m.save(ctx, &Status{Graph: "legacy", State: StateReady, ReuseKey: g.Key}); err != nil {
					t.Fatal(err)
				}
			default:
				h.docker.set(t, "containers", "container-id\n")
				script, err := os.ReadFile(h.docker.bin)
				if err != nil {
					t.Fatal(err)
				}
				reply := "container-id sha256:shared"
				if kind == "volume" {
					reply = "container-id sha256:other " + g.Caches[0].Volume
				}
				if kind == "invalid-inventory" {
					reply = "bad"
				}
				addition := "\n inspect) echo '" + reply + "' ;;\n image-inspect) echo sha256:shared ;;"
				script = []byte(strings.Replace(string(script), "case \"$sub\" in", "case \"$sub\" in"+addition, 1))
				if err := os.WriteFile(h.docker.bin, script, 0755); err != nil {
					t.Fatal(err)
				}
				if kind == "inspect-failed" {
					h.docker.set(t, "fail-inspect", "")
				}
			}
			item := cleanupOne(t, h, g, false)
			if item.Removed || item.Eligible {
				t.Fatalf("%+v", item)
			}
			if len(item.UsedBy) == 0 && item.Error == "" {
				t.Fatal("missing preservation reason")
			}
		})
	}
}

func TestCleanupRechecksAfterConcurrentLease(t *testing.T) {
	h := setup(t)
	g := cleanupFixture(t, h, "shared_race")
	ctx := context.Background()
	lease := "environment-reuse/" + g.Key
	if ok, err := h.store.AcquireLease(ctx, lease, "builder", time.Minute); err != nil || !ok {
		t.Fatal(err)
	}
	done := make(chan *CleanupResult, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := h.m.CleanupShared(ctx, []string{g.ID}, false, "test")
		done <- result
		errs <- err
	}()
	if err := h.m.save(ctx, &Status{Graph: "new-consumer", State: StateReady, SharedImage: g.Image}); err != nil {
		t.Fatal(err)
	}
	if err := h.store.ReleaseLease(ctx, lease, "builder"); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if result.Generations[0].Removed || result.Generations[0].Eligible {
		t.Fatal("deleted newly referenced generation")
	}
}

func TestCleanupTracksForceBuildGenerationsIntegration(t *testing.T) {
	h := setup(t)
	script, err := os.ReadFile(h.docker.bin)
	if err != nil {
		t.Fatal(err)
	}
	script = []byte(strings.Replace(string(script), "case \"$sub\" in", "case \"$sub\" in\n version) echo linux/arm64 ;;", 1))
	if err := os.WriteFile(h.docker.bin, script, 0755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a := graphWith(&domain.EnvironmentSpec{Image: "alpine:3.22", Caches: []string{"/home/sandbox/.cache/pip"}})
	a.Spec.Workspace.Repo = "https://example.test/repo.git"
	b := *a
	b.Metadata.ID = "other"
	build := func(g *domain.Graph, force bool) *View {
		t.Helper()
		if _, err := h.m.Build(ctx, g, force, "test"); err != nil {
			t.Fatal(err)
		}
		v := h.wait(t, g)
		if v.State != StateReady {
			t.Fatalf("%+v", v)
		}
		return v
	}
	first := build(a, false)
	build(&b, false)
	second := build(a, true)
	if first.SharedImage == second.SharedImage || first.SharedImage == "" {
		t.Fatal("force rebuild did not track a new generation")
	}
	old := sharedGeneration{ID: generationImage.FindStringSubmatch(first.SharedImage)[1]}
	if item := cleanupOne(t, h, old, false); item.Removed || item.Eligible {
		t.Fatalf("%+v", item)
	}
	if err := h.m.Reset(ctx, b.Metadata.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if item := cleanupOne(t, h, old, false); !item.Removed {
		t.Fatalf("%+v", item)
	}
	data, err := h.store.KVGet(ctx, reuseNamespace, second.ReuseKey)
	if err != nil {
		t.Fatal(err)
	}
	var current sharedEnvironment
	if err = json.Unmarshal(data, &current); err != nil {
		t.Fatal(err)
	}
	if current.Image != second.SharedImage {
		t.Fatal("removed current generation pointer")
	}
	if err := h.m.Check(ctx, a); err != nil {
		t.Fatal(err)
	}
}

type cleanupFailureStore struct {
	storage.Store
	namespace string
}

func (s cleanupFailureStore) KVList(ctx context.Context, ns, prefix string) (map[string][]byte, error) {
	if ns == s.namespace {
		return nil, errors.New("storage unavailable")
	}
	return s.Store.KVList(ctx, ns, prefix)
}
func TestCleanupFailsClosedOnStorageAndMalformedReferences(t *testing.T) {
	for _, scenario := range []string{"storage", "malformed", "definition", "companion"} {
		t.Run(scenario, func(t *testing.T) {
			h := setup(t)
			g := cleanupFixture(t, h, "shared_state")
			switch scenario {
			case "storage":
				h.m.opt.Store = cleanupFailureStore{Store: h.store, namespace: kvNamespace}
			case "malformed":
				if err := h.store.KVPut(context.Background(), kvNamespace, "unknown", []byte("{")); err != nil {
					t.Fatal(err)
				}
			case "definition", "companion":
				graph := graphWith(nil)
				graph.Spec.Defaults.Isolation.Image = g.Image
				if scenario == "companion" {
					graph.Spec.Defaults.Isolation.Image = ""
					graph.Spec.Environment = &domain.EnvironmentSpec{Image: "alpine", Companions: []domain.Companion{{Name: "cache", Image: g.Image}}}
				}
				if _, err := h.store.SaveGraph(context.Background(), graph, 0); err != nil {
					t.Fatal(err)
				}
			}
			item := cleanupOne(t, h, g, false)
			if item.Removed || item.Eligible {
				t.Fatalf("%+v", item)
			}
			if scenario != "definition" && scenario != "companion" && item.Error == "" {
				t.Fatal("failure hidden")
			}
			calls := strings.Join(h.docker.calls(t), "\n")
			if strings.Contains(calls, "rmi ") || strings.Contains(calls, "volume rm") {
				t.Fatal("removed resource with uncertain references")
			}
		})
	}
}

func TestCleanupRejectsForeignDaemonAndRetainsUntrackedResources(t *testing.T) {
	h := setup(t)
	g := cleanupFixture(t, h, "shared_foreign")
	g.Daemon = "another-daemon"
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.KVPut(context.Background(), generationsNamespace, g.ID, data); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.CleanupShared(context.Background(), []string{g.ID}, false, "test"); !errors.Is(err, ErrInvalidCleanup) {
		t.Fatal(err)
	}
	result, err := h.m.CleanupShared(context.Background(), nil, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Generations) != 0 {
		t.Fatalf("%+v", result)
	}
	if len(h.docker.calls(t)) != 0 {
		t.Fatal("inspected foreign daemon resources")
	}
}
