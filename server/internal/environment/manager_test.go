package environment

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

// fakeDocker is a docker CLI stand-in. It appends "args" lines to calls.log
// and reacts to files in its directory:
//
//	fail-<subcommand>  exit 1 with "boom: <subcommand> failed" for that subcommand
//	slow-<subcommand>  sleep 30s for that subcommand (to test cancellation)
//	images             lines printed by "image ls"
//	volumes            lines printed by "volume ls"
type fakeDocker struct {
	dir, bin string
}

func newFakeDocker(t *testing.T) *fakeDocker {
	t.Helper()
	dir := t.TempDir()
	f := &fakeDocker{dir: dir, bin: filepath.Join(dir, "docker")}
	script := `#!/bin/sh
d=` + dir + `
echo "$*" >> "$d/calls.log"
[ "$1" != --context ] || shift 2
if [ "$1" = --tlscert ]; then
  for file in "$2" "$4" "$DOCKER_CERT_PATH/ca.pem"; do
    if [ ! -f "$file" ] || [ ! -r "$file" ]; then echo "cannot read $file" >&2; exit 1; fi
  done
  shift 4
fi
sub="$1"; [ "$1" = image ] || [ "$1" = volume ] || [ "$1" = buildx ] || [ "$1" = network ] || [ "$1" = context ] && sub="$1-$2"
[ -f "$d/slow-$sub" ] && sleep 30
if [ -f "$d/fail-$sub" ]; then echo "step output"; echo "boom: $sub failed" >&2; exit 1; fi
case "$sub" in
  inspect) [ -f "$d/proxy-address" ] && cat "$d/proxy-address" ;;
  context-ls) [ ! -f "$d/contexts" ] || cat "$d/contexts" ;;
  buildx-create)
    while [ "$#" -gt 1 ]; do
      if [ "$1" = --buildkitd-config ]; then cat "$2" > "$d/buildkitd-config" || exit 1; fi
      shift
    done
    cat > /dev/null ;;
  pull) echo "Pulling from library"; echo "Status: Downloaded" ;;
  build|buildx-build) cat > /dev/null; echo "Successfully built" ;;
  run) echo "setup ran: $*" ;;
  ps) [ -f "$d/containers" ] && cat "$d/containers" ;;
  image-ls) [ -f "$d/images" ] && cat "$d/images" ;;
  volume-ls) [ -f "$d/volumes" ] && cat "$d/volumes" ;;
esac
exit 0
`
	if err := os.WriteFile(f.bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fakeDocker) set(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeDocker) calls(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "calls.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

type fakeWorkspaces struct{ dir string }

func (w fakeWorkspaces) Ensure(context.Context, *domain.Graph) (string, error) { return w.dir, nil }
func (w fakeWorkspaces) HostPath(p string) string                              { return "/host" + p }

type harness struct {
	m      *Manager
	docker *fakeDocker
	store  storage.Store
	ws     string
}

func setup(t *testing.T) *harness {
	t.Helper()
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := newFakeDocker(t)
	ws := t.TempDir()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m, err := New(Options{Store: st, Bus: events.NewBus(st, log), Workspaces: fakeWorkspaces{ws}, DockerBin: d.bin,
		DockerHost: "tcp://sandbox-daemon:2376", Instance: "test-1", Log: log})
	if err != nil {
		t.Fatal(err)
	}
	return &harness{m: m, docker: d, store: st, ws: ws}
}

func graphWith(env *domain.EnvironmentSpec) *domain.Graph {
	return &domain.Graph{Metadata: domain.GraphMetadata{ID: "bookaneer"}, Spec: domain.GraphSpec{Environment: env}}
}

func goNodeEnv() *domain.EnvironmentSpec {
	return &domain.EnvironmentSpec{
		Image:   "golang:1.26",
		Setup:   []string{"go mod download", "echo 'it''s ready'"},
		Caches:  []string{"/home/sandbox/go/pkg/mod"},
		Network: domain.EnvironmentNetwork{Run: "bridge"},
	}
}

// wait polls until the environment leaves the building state.
func (h *harness) wait(t *testing.T, g *domain.Graph) *View {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v, err := h.m.Get(context.Background(), g)
		if err != nil {
			t.Fatal(err)
		}
		if v.State != StateBuilding {
			return v
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("build did not finish")
	return nil
}

func (h *harness) eventTypes(t *testing.T) []domain.EventType {
	t.Helper()
	evs, err := h.store.ListEvents(context.Background(), storage.EventFilter{GraphID: "bookaneer", Ascending: true})
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.EventType
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

func TestHashTracksOnlyWhatChangesTheImage(t *testing.T) {
	base := goNodeEnv()
	h := Hash(base)
	same := *base
	same.Network.Run, same.Timeout, same.CPU, same.Memory = "none", domain.Duration(time.Hour), "4", "8g"
	same.Caches = []string{"/home/sandbox/go/pkg/mod/"}
	if Hash(&same) != h {
		t.Fatal("run network, timeout, limits and path spelling must not change the hash")
	}
	for name, change := range map[string]func(e *domain.EnvironmentSpec){
		"image":         func(e *domain.EnvironmentSpec) { e.Image = "golang:1.25" },
		"setup":         func(e *domain.EnvironmentSpec) { e.Setup = append(e.Setup, "pnpm install") },
		"setup network": func(e *domain.EnvironmentSpec) { e.Network.Setup = "none" },
		"caches":        func(e *domain.EnvironmentSpec) { e.Caches = append(e.Caches, "/home/sandbox/.cache") },
	} {
		c := *base
		c.Setup = append([]string(nil), base.Setup...)
		c.Caches = append([]string(nil), base.Caches...)
		change(&c)
		if Hash(&c) == h {
			t.Errorf("changing %s must change the hash", name)
		}
	}
}

func TestUserLayerAndSetupScript(t *testing.T) {
	layer := UserLayer("golang:1.26", []string{"/home/sandbox/go/pkg/mod", "/opt/it's"})
	for _, want := range []string{"FROM golang:1.26", "useradd --uid 10001", "adduser -D -u 10001", "ENV HOME=/home/sandbox", "USER 10001",
		"'/home/sandbox/go/pkg/mod'", `'/opt/it'\''s'`, "WORKDIR /workspace"} {
		if !strings.Contains(layer, want) {
			t.Errorf("user layer lacks %q:\n%s", want, layer)
		}
	}
	script := SetupScript([]string{"go mod download", "echo 'x'"})
	if !strings.HasPrefix(script, "set -e\n") || !strings.Contains(script, "echo '$ go mod download'\ngo mod download\n") ||
		!strings.Contains(script, `echo '$ echo '\''x'\'''`) {
		t.Fatalf("script:\n%s", script)
	}
}

func TestBuildFromImageWithSetupAndCaches(t *testing.T) {
	h := setup(t)
	h.docker.set(t, "images", "weft-env/bookaneer:oldhash00000\nweft-env/bookaneer:"+Hash(goNodeEnv())+"\n")
	g := graphWith(goNodeEnv())
	ctx := context.Background()

	v, err := h.m.Build(ctx, g, false, "admin")
	if err != nil || v.State != StateBuilding || v.CurrentHash == "" || v.Actor != "admin" {
		t.Fatalf("build start: %+v %v", v, err)
	}
	v = h.wait(t, g)
	hash := Hash(goNodeEnv())
	if v.State != StateReady || !v.UpToDate || v.Image != "weft-env/bookaneer:"+hash || v.Error != "" {
		t.Fatalf("ready: %+v", v)
	}
	if len(v.Caches) != 1 || v.Caches[0].Volume != CacheVolume("bookaneer", "/home/sandbox/go/pkg/mod") {
		t.Fatalf("caches: %+v", v.Caches)
	}
	for _, want := range []string{"==> Pulling base image golang:1.26", "Status: Downloaded", "==> Running 2 setup command(s) (network bridge)", "==> Environment ready"} {
		if !strings.Contains(v.Log, want) {
			t.Errorf("log lacks %q:\n%s", want, v.Log)
		}
	}

	calls := strings.Join(h.docker.calls(t), "\n")
	for _, want := range []string{
		"pull golang:1.26",
		"build --network none -t weft-env/bookaneer:" + hash + "-user --label weft.graph=bookaneer --label weft.environment=" + hash + " -",
		"run --name weft-envbuild-bookaneer --network bridge --cap-drop ALL --security-opt no-new-privileges -e CI=true",
		"-v /host" + h.ws + ":/workspace -w /workspace --entrypoint sh weft-env/bookaneer:" + hash + "-user -c set -e",
		"commit weft-envbuild-bookaneer weft-env/bookaneer:" + hash,
		"volume create --label weft.graph=bookaneer " + CacheVolume("bookaneer", "/home/sandbox/go/pkg/mod"),
		"rmi weft-env/bookaneer:oldhash00000",
		"rmi weft-env/bookaneer:" + hash + "-user",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("docker calls lack %q:\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "rmi weft-env/bookaneer:"+hash+"\n") {
		t.Errorf("the new image must not be removed:\n%s", calls)
	}

	sb, err := h.m.SandboxFor(ctx, g)
	if err != nil || sb.Image != v.Image || sb.Network != "bridge" || len(sb.Volumes) != 1 || !strings.HasSuffix(sb.Volumes[0], ":/home/sandbox/go/pkg/mod") {
		t.Fatalf("sandbox: %+v %v", sb, err)
	}
	if got := h.eventTypes(t); len(got) != 2 || got[0] != domain.EvEnvBuildStarted || got[1] != domain.EvEnvReady {
		t.Fatalf("events: %v", got)
	}

	// Up to date: a plain build is a no-op; force rebuilds.
	before := len(h.docker.calls(t))
	if v, err := h.m.Build(ctx, g, false, "admin"); err != nil || v.State != StateReady {
		t.Fatalf("no-op build: %+v %v", v, err)
	}
	if len(h.docker.calls(t)) != before {
		t.Fatal("an up-to-date environment must not be rebuilt")
	}
	if v, err := h.m.Build(ctx, g, true, "admin"); err != nil || v.State != StateBuilding {
		t.Fatalf("forced build: %+v %v", v, err)
	}
	h.wait(t, g)
}

func TestBuildFromDockerfileWithoutSetup(t *testing.T) {
	h := setup(t)
	g := graphWith(&domain.EnvironmentSpec{Dockerfile: "FROM alpine:3.22\nRUN apk add --no-cache go\n"})
	if _, err := h.m.Build(context.Background(), g, false, "op"); err != nil {
		t.Fatal(err)
	}
	v := h.wait(t, g)
	hash := Hash(g.Spec.Environment)
	if v.State != StateReady {
		t.Fatalf("%+v", v)
	}
	calls := strings.Join(h.docker.calls(t), "\n")
	for _, want := range []string{
		"build --pull --network default -t weft-env/bookaneer:" + hash + "-base",
		"tag weft-env/bookaneer:" + hash + "-user weft-env/bookaneer:" + hash,
		"rmi weft-env/bookaneer:" + hash + "-base",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("docker calls lack %q:\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "pull ") && !strings.Contains(calls, "build --pull") || strings.Contains(calls, "run --name") {
		t.Errorf("a Dockerfile without setup must not pull separately or run setup:\n%s", calls)
	}
}

func TestFailedBuildIsReportedAndBlocksAgents(t *testing.T) {
	h := setup(t)
	h.docker.set(t, "fail-run", "")
	g := graphWith(goNodeEnv())
	ctx := context.Background()
	if _, err := h.m.Build(ctx, g, false, "admin"); err != nil {
		t.Fatal(err)
	}
	v := h.wait(t, g)
	if v.State != StateFailed || v.UpToDate || !strings.Contains(v.Error, "setup: exit status 1: boom: run failed") {
		t.Fatalf("failed: %+v", v)
	}
	if !strings.Contains(v.Log, "step output") {
		t.Fatalf("log should keep the failing step's output:\n%s", v.Log)
	}
	if calls := strings.Join(h.docker.calls(t), "\n"); !strings.Contains(calls, "rm -f weft-envbuild-bookaneer") || strings.Contains(calls, "commit") {
		t.Fatalf("a failed setup must be cleaned up and not committed:\n%s", calls)
	}
	_, err := h.m.SandboxFor(ctx, g)
	if !errors.Is(err, ErrNotReady) || !strings.Contains(err.Error(), "the last build failed") {
		t.Fatalf("sandbox: %v", err)
	}
	if got := h.eventTypes(t); got[len(got)-1] != domain.EvEnvFailed {
		t.Fatalf("events: %v", got)
	}
}

func TestNotReadyMessages(t *testing.T) {
	h := setup(t)
	g := graphWith(goNodeEnv())
	ctx := context.Background()
	if err := h.m.Check(ctx, g); !errors.Is(err, ErrNotReady) || !strings.Contains(err.Error(), "not been built yet") {
		t.Fatalf("absent: %v", err)
	}
	if _, err := h.m.Build(ctx, g, false, "a"); err != nil {
		t.Fatal(err)
	}
	h.wait(t, g)
	if err := h.m.Check(ctx, g); err != nil {
		t.Fatalf("ready: %v", err)
	}
	changed := graphWith(goNodeEnv())
	changed.Spec.Environment.Setup = []string{"pnpm install"}
	if err := h.m.Check(ctx, changed); !errors.Is(err, ErrNotReady) || !strings.Contains(err.Error(), "definition changed") {
		t.Fatalf("stale: %v", err)
	}
	// A failed build of an older definition is also "changed", not "never built".
	h.docker.set(t, "fail-run", "")
	if _, err := h.m.Build(ctx, g, true, "a"); err != nil {
		t.Fatal(err)
	}
	if v := h.wait(t, g); v.State != StateFailed {
		t.Fatalf("forced failing build: %+v", v)
	}
	if err := h.m.Check(ctx, changed); !strings.Contains(err.Error(), "definition changed") {
		t.Fatalf("changed after a failed build: %v", err)
	}
	if _, err := h.m.Build(ctx, graphWith(nil), false, "a"); !errors.Is(err, ErrNotDefined) {
		t.Fatalf("undefined build: %v", err)
	}
	if _, err := h.m.SandboxFor(ctx, graphWith(nil)); !errors.Is(err, ErrNotDefined) {
		t.Fatalf("undefined sandbox: %v", err)
	}
}

func TestConcurrentBuildIsRejectedAndResetCancels(t *testing.T) {
	h := setup(t)
	h.docker.set(t, "slow-pull", "")
	h.docker.set(t, "images", "weft-env/bookaneer:"+Hash(goNodeEnv())+"\n")
	h.docker.set(t, "volumes", CacheVolume("bookaneer", "/home/sandbox/go/pkg/mod")+"\n")
	g := graphWith(goNodeEnv())
	ctx := context.Background()
	if _, err := h.m.Build(ctx, g, false, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Build(ctx, g, true, "b"); !errors.Is(err, ErrBuildRunning) {
		t.Fatalf("second build: %v", err)
	}

	start := time.Now()
	if err := h.m.Reset(ctx, "bookaneer", "admin"); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("reset must cancel the running build instead of waiting for it")
	}
	v, err := h.m.Get(ctx, g)
	if err != nil || v.State != StateAbsent {
		t.Fatalf("after reset: %+v %v", v, err)
	}
	if ok, _ := h.m.Exists(ctx, "bookaneer"); ok {
		t.Fatal("reset must remove the record")
	}
	calls := strings.Join(h.docker.calls(t), "\n")
	if strings.Contains(calls, "ps -aq") {
		t.Errorf("reset must not select containers by graph label (agents' sandboxes share it):\n%s", calls)
	}
	for _, want := range []string{"rm -f weft-envbuild-bookaneer", "rmi -f weft-env/bookaneer:" + Hash(goNodeEnv()), "volume rm -f " + CacheVolume("bookaneer", "/home/sandbox/go/pkg/mod")} {
		if !strings.Contains(calls, want) {
			t.Errorf("reset calls lack %q:\n%s", want, calls)
		}
	}
	if got := h.eventTypes(t); got[len(got)-1] != domain.EvEnvReset {
		t.Fatalf("events: %v", got)
	}
	// The lease is released: a new build may start.
	os.Remove(filepath.Join(h.docker.dir, "slow-pull"))
	if _, err := h.m.Build(ctx, g, false, "a"); err != nil {
		t.Fatalf("build after reset: %v", err)
	}
	h.wait(t, g)
}

func TestResetReportsDockerFailures(t *testing.T) {
	h := setup(t)
	h.docker.set(t, "images", "weft-env/bookaneer:abc\n")
	h.docker.set(t, "fail-rmi", "")
	err := h.m.Reset(context.Background(), "bookaneer", "admin")
	if err == nil || !strings.Contains(err.Error(), "remove weft-env/bookaneer:abc") {
		t.Fatalf("reset failure: %v", err)
	}
}

func TestInterruptedBuildIsReportedAsFailed(t *testing.T) {
	h := setup(t)
	ctx := context.Background()
	b, _ := json.Marshal(Status{Graph: "bookaneer", State: StateBuilding, Hash: "x", Instance: "gone"})
	if err := h.store.KVPut(ctx, kvNamespace, "bookaneer", b); err != nil {
		t.Fatal(err)
	}
	v, err := h.m.Get(ctx, graphWith(goNodeEnv()))
	if err != nil || v.State != StateFailed || !strings.Contains(v.Error, "interrupted") {
		t.Fatalf("interrupted: %+v %v", v, err)
	}
}

func TestTailBufferKeepsTheEnd(t *testing.T) {
	tb := &tailBuffer{limit: 8}
	_, _ = tb.Write([]byte("0123456789"))
	_, _ = tb.Write([]byte("ab"))
	if got := tb.String(); got != "[earlier output truncated]\n456789ab" {
		t.Fatalf("%q", got)
	}
}

func TestBuildValidatesAcceptedRepositoryBeforeDocker(t *testing.T) {
	h := setup(t)
	writeDiscoveryFile(t, h.ws, ".weft/environment.yaml", "image: alpine")
	env, err := Resolve(context.Background(), h.ws, discoverSelection(t, h.ws, ""))
	if err != nil {
		t.Fatal(err)
	}
	g := graphWith(env)
	writeDiscoveryFile(t, h.ws, ".weft/environment.yaml", "image: debian")
	if _, err := h.m.Build(context.Background(), g, false, "operator"); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("stale definition %v", err)
	}
	if len(h.docker.calls(t)) != 0 {
		t.Fatal("Docker invoked before validation")
	}
	v, err := h.m.Get(context.Background(), g)
	if err != nil || v.State != StateAbsent {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestCancelBuildRetainsStatusAndDoesNotResetCaches(t *testing.T) {
	h := setup(t)
	h.docker.set(t, "slow-pull", "")
	g := graphWith(goNodeEnv())
	ctx := context.Background()
	if _, err := h.m.Build(ctx, g, false, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := h.m.CancelBuild(ctx, g.Metadata.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	v := h.wait(t, g)
	if v.State != StateFailed || v.Error != "build cancelled" {
		t.Fatal(v)
	}
	calls := strings.Join(h.docker.calls(t), "\n")
	if strings.Contains(calls, "volume rm") || strings.Contains(calls, "volume ls") {
		t.Fatal("cancellation reset caches", calls)
	}
	if err := h.m.CancelBuild(ctx, g.Metadata.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	again, err := h.m.Get(ctx, g)
	if err != nil || again.State != v.State || again.Error != v.Error {
		t.Fatal(again, err)
	}
}

func TestCancelBuildDoesNotCancelAnotherInstance(t *testing.T) {
	h := setup(t)
	ctx := context.Background()
	ok, err := h.store.AcquireLease(ctx, leasePrefix+"bookaneer", "other-instance", time.Minute)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err = h.m.CancelBuild(ctx, "bookaneer", "admin"); !errors.Is(err, ErrBuildRunning) {
		t.Fatal(err)
	}
	leases, err := h.store.ListLeases(ctx, leasePrefix+"bookaneer")
	if err != nil || len(leases) != 1 || leases[0].Holder != "other-instance" {
		t.Fatal(leases, err)
	}
}
