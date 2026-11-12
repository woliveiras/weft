package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/tools"
)

type egressWorkspace string

func (w egressWorkspace) Ensure(context.Context, *domain.Graph) (string, error) {
	return string(w), nil
}
func (w egressWorkspace) HostPath(p string) string { return p }

func TestEgressRealSandbox(t *testing.T) {
	bin, image := os.Getenv("WEFT_TEST_ENV_DOCKER_BIN"), os.Getenv("WEFT_TEST_EGRESS_IMAGE")
	if bin == "" || image == "" {
		t.Skip("run make test-egress")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id := fmt.Sprintf("egress-test-%d", time.Now().UnixNano())
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Isolation.Docker.Bin = bin
	cfg.Isolation.Docker.EgressImage = os.Getenv("WEFT_TEST_EGRESS_PROXY_IMAGE")
	if cfg.Isolation.Docker.EgressImage == "" {
		t.Fatal("test proxy image required")
	}
	cfg.Isolation.Docker.EnvironmentPolicy.RunNetworks = []string{"none", "egress"}
	cfg.Isolation.Docker.EnvironmentPolicy.EgressHosts = []string{"example.com", "127.0.0.1.nip.io"}
	cfg.Isolation.Docker.Capacity.DefaultMemory = "64m"
	cfg.Isolation.Docker.Capacity.DefaultCPU = "0.25"
	bus := events.NewBus(store, slog.Default())
	manager, err := environment.New(environment.Options{Store: store, Workspaces: egressWorkspace(id + "-workspace"), DockerBin: bin, Policy: cfg.Isolation.Docker.EnvironmentPolicy})
	if err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: id}, Spec: domain.GraphSpec{Environment: &domain.EnvironmentSpec{Image: image, Network: domain.EnvironmentNetwork{Run: "egress", AllowedHosts: []string{"example.com", "127.0.0.1.nip.io"}}}}}
	// Seed a previously prepared image, exercising the production runtime factory.
	data := fmt.Sprintf(`{"graph":%q,"state":"ready","hash":%q,"image":%q}`, id, environment.Hash(g.Spec.Environment), image)
	if err := store.KVPut(ctx, "environment", id, []byte(data)); err != nil {
		t.Fatal(err)
	}
	spec := engine.SandboxSpec{Graph: g, Node: &domain.Node{ID: "worker", Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}}, HostWorkdir: id + "-workspace"}
	factory := sandboxFactory(cfg, manager, store, bus)
	sandbox, err := factory(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sandbox.Close(); err != nil {
			t.Error(err)
		}
		out, err := tools.DockerCommand(context.Background(), bin, "", "volume", "rm", id+"-workspace").CombinedOutput()
		if err != nil {
			t.Error(string(out), err)
		}
	})
	run := func(command string, success bool) {
		t.Helper()
		result, err := sandbox.Exec(ctx, tools.ExecRequest{Shell: command})
		if err != nil || (result.ExitCode == 0) != success {
			t.Fatalf("%s: %+v %v", command, result, err)
		}
	}
	name := "weft-sbx-" + id + "-worker-0-svc-weft-egress"
	// The allowed fixture name resolves to loopback; policy must still reject it.
	run(`output=$(/probe http://127.0.0.1.nip.io); echo "$output"; test "$output" = 403`, true)
	run("/probe http://example.com", true)
	run("/probe https://example.com", true)
	run("/probe http://example.org", false)
	run("/probe http://169.254.169.254", false)
	run("/probe http://1.1.1.1 direct", false)
	run(`! awk '$2 == "00000000" { found=1 } END { exit !found }' /proc/net/route`, true)
	status, err := manager.ProxyStatus(ctx, id)
	if err != nil || len(status.Proxies) != 1 || status.Proxies[0].State != "running" {
		t.Fatal(status, err)
	}
	found := false
	for _, request := range status.Proxies[0].Requests {
		if request.Host == "example.org" && request.Outcome == "denied" && request.Reason == "host_not_allowed" {
			found = true
		}
	}
	if !found {
		t.Fatal("real denial missing from proxy diagnostics", status)
	}
	suspend, ok := sandbox.(interface{ Suspend(context.Context) error })
	if !ok {
		t.Fatal("missing suspension")
	}
	if err := suspend.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	status, err = manager.ProxyStatus(ctx, id)
	if err != nil || len(status.Proxies) != 1 || status.Proxies[0].State != "exited" {
		t.Fatal("suspended proxy state", status, err)
	}
	settings := environmentpolicy.Settings{Store: store, Base: cfg.Isolation.Docker.EnvironmentPolicy}
	narrow := environmentpolicy.NetworkSettings{SetupNetworks: []string{"bridge"}, RunNetworks: []string{"none", "egress"}, EgressHosts: []string{"example.com"}}
	v, err := settings.Save(ctx, narrow, 0, "test-admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := sandbox.(interface{ Start(context.Context) error }).Start(ctx); err == nil {
		t.Fatal("suspended sandbox resumed with revoked host")
	}
	if _, err := factory(ctx, spec); err == nil {
		t.Fatal("new controller ignored saved network policy")
	}
	narrow.EgressHosts = []string{"example.com", "127.0.0.1.nip.io"}
	if _, err := settings.Save(ctx, narrow, v.Revision, "test-admin"); err != nil {
		t.Fatal(err)
	}
	if err := sandbox.(interface{ Start(context.Context) error }).Start(ctx); err != nil {
		t.Fatal(err)
	}
	sandbox, err = factory(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	run("/probe https://example.com", true)
	logs, err := tools.DockerCommand(ctx, bin, "", "logs", name).CombinedOutput()
	if err != nil || !strings.Contains(string(logs), `"outcome":"denied"`) {
		t.Fatal(string(logs), err)
	}
	if out, err := tools.DockerCommand(ctx, bin, "", "stop", name).CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	if _, err := sandbox.Exec(ctx, tools.ExecRequest{Shell: "/probe http://example.com"}); err == nil {
		t.Fatal("proxy failure not propagated")
	}
	if err := sandbox.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := environment.New(environment.Options{Store: store, Workspaces: egressWorkspace(id + "-workspace"), DockerBin: bin, Policy: cfg.Isolation.Docker.EnvironmentPolicy})
	if err != nil {
		t.Fatal(err)
	}
	history, err := restored.BlockedHistory(ctx, id)
	if err != nil || len(history.Requests) == 0 || history.Limit != 1000 {
		t.Fatal("runtime history lost after container removal/controller recreation", history, err)
	}
	t.Logf("Retained %d denied requests after real proxy removal", len(history.Requests))
	if _, err := restored.SaveRetention(ctx, environment.RetentionPolicy{Days: 1, MaxEvents: 1}, 0, "live-test-admin"); err != nil {
		t.Fatal(err)
	}
	limited, err := manager.BlockedHistory(ctx, id)
	if err != nil || len(limited.Requests) != 1 || limited.RetentionDays != 1 || limited.Limit != 1 {
		t.Fatal("live history ignored changed retention", limited, err)
	}
	if !limited.Requests[0].Time.Equal(history.Requests[0].Time) {
		t.Fatal("did not retain newest denial")
	}
	t.Log("Installation retention update applied across managers to real proxy history")
	t.Log("HTTP/HTTPS allowed, unlisted hosts/private IPs/direct egress blocked, suspension/resume, persisted policy revocation/restoration and proxy failure verified")
}
