package environment

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestEnvironmentPolicyRealSandbox(t *testing.T) {
	bin := os.Getenv("WEFT_TEST_ENV_DOCKER_BIN")
	if bin == "" {
		t.Skip("run make test-environment-policy")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("policy-test-%d", time.Now().UnixNano())
	ws := reuseWorkspaces{root: t.TempDir(), prefix: id + "-workspace-"}
	policy := environmentpolicy.Policy{SetupNetworks: []string{"none"}, RunNetworks: []string{"none"}}
	m, err := New(Options{Store: store, Workspaces: ws, DockerBin: bin, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := m.docker(ctx, nil, &out, "image", "inspect", "alpine:3.22", "--format", "{{.Id}}"); err != nil {
		t.Fatalf("cached Alpine required: %s %v", out.String(), err)
	}
	env := &domain.EnvironmentSpec{Image: "alpine:3.22", Network: domain.EnvironmentNetwork{Setup: "none"}, Setup: []string{`test "$(id -u)" = 10001`, `test "$(wc -l < /proc/net/route)" -eq 1`, `! wget -q -T 1 -O /dev/null http://192.0.2.1`, `echo isolated > /tmp/policy-proof`}}
	g := graphWith(env)
	g.Metadata.ID = id
	dir, err := ws.Ensure(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ValidateDefinition(ctx, g, dir); err != nil {
		t.Fatal(err)
	}
	final := ImageRef(id, Hash(env))
	buildTag := "weft-policy-test/" + id + ":build"
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		for _, args := range [][]string{{"rm", "-f", buildContainer(id)}, {"rmi", "-f", final, buildTag}, {"volume", "rm", ws.HostPath(dir)}} {
			var output bytes.Buffer
			if err := m.docker(cleanup, nil, &output, args...); err != nil {
				t.Errorf("cleanup %v: %s %v", args, output.String(), err)
			}
		}
	})
	// Exercise Docker's actual RUN isolation using a cached base (no forced pull).
	// Production build argument/admission wiring is covered separately.
	source := "FROM alpine:3.22\nRUN test \"$(wc -l < /proc/net/route)\" -eq 1 && ! wget -q -T 1 -O /dev/null http://192.0.2.1\n"
	out.Reset()
	if err := m.docker(ctx, strings.NewReader(source), &out, "build", "--network", buildNetwork(env), "-t", buildTag, "-"); err != nil {
		t.Fatalf("real build network: %s %v", out.String(), err)
	}
	// The production reuse setup path with finite CPU/memory, using only cached images.
	setupCtx := context.WithValue(ctx, buildBudgetKey{}, buildBudget{id: id, memory: 64 << 20, cpu: 250})
	if _, err := m.setupSharedWorkspace(setupCtx, g, dir, "alpine:3.22", nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := m.save(ctx, &Status{Graph: id, State: StateReady, Hash: Hash(env), Image: final}); err != nil {
		t.Fatal(err)
	}
	ready, err := m.SandboxFor(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	budget := &sandboxcapacity.Manager{Store: store, Scope: id, Policy: sandboxcapacity.Policy{MaxActive: 4, TotalMemory: "4g", DefaultMemory: "64m", TotalCPU: "4", DefaultCPU: "0.25"}}
	sb, err := tools.NewDockerSandbox(tools.DockerSandboxConfig{Name: id, Image: ready.Image, Network: ready.Network, HostDir: ws.HostPath(dir), User: "10001:10001", DockerBin: bin, Capacity: budget})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sb.Close(); err != nil {
			t.Error(err)
		}
	}()
	result, err := sb.Exec(ctx, tools.ExecRequest{Shell: `test "$(id -u)" = 10001; test "$(cat /tmp/policy-proof)" = isolated; test "$(wc -l < /proc/net/route)" -eq 1; ! wget -q -T 1 -O /dev/null http://192.0.2.1`})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("runtime: %+v %v", result, err)
	}
	env.Network.Run = "bridge"
	if _, err := m.SandboxFor(ctx, g); err == nil {
		t.Fatal("cached image escaped policy")
	}
	t.Log("real RUN, setup and runtime verified without egress; cached image denied after network override")
}
