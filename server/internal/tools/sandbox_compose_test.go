package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func composeFixture(t *testing.T) (DockerSandboxConfig, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "docker")
	script := `#!/bin/sh
set -eu
d='` + dir + `'
echo "$*" >> "$d/calls"
if [ "$1" = network ]; then
 shift
 case "$1" in
 inspect) if [ ! -f "$d/network" ]; then echo 'No such network'; exit 1; fi; cat "$d/network";;
 create) for arg in "$@"; do case "$arg" in weft.compose=*) echo "${arg#weft.compose=}" > "$d/network";; esac; done;;
 connect) for arg in "$@"; do last=$arg; done; if [ -f "$d/fail-uplink" ]; then exit 1; fi; echo true > "$d/$last.uplink";;
 rm) rm -f "$d/network";;
 esac
 exit 0
fi
op=$1
last=''
for arg in "$@"; do last=$arg; done
case "$op" in
 inspect)
 if [ ! -f "$d/$last.hash" ]; then echo 'No such container'; exit 1; fi
 case "$*" in *NetworkSettings*) if [ -f "$d/$last.uplink" ]; then cat "$d/$last.uplink"; else echo false; fi;; *Config.Labels*) cat "$d/$last.hash";; *) cat "$d/$last.running";; esac;;
 run)
 name=''; hash=''
 while [ $# -gt 0 ]; do
 case "$1" in --name) shift; name=$1;; --label) shift; case "$1" in weft.compose=*) hash=${1#weft.compose=};; esac;; esac
 shift
 done
 if [ -f "$d/fail-run" ]; then echo 'fixture start failure'; exit 1; fi
 echo "$hash" > "$d/$name.hash"; echo true > "$d/$name.running";;
 start) echo true > "$d/$last.running";;
 stop) if [ -f "$d/fail-stop" ]; then echo 'fixture stop failure'; exit 1; fi; echo false > "$d/$last.running";;
 rm) rm -f "$d/$last.hash" "$d/$last.running";;
 exec) echo ran;;
 esac
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return DockerSandboxConfig{Name: "weft-sbx-test", Image: "fixture", HostDir: "workspace", DockerBin: bin, Capacity: &sandboxcapacity.Manager{Store: store, Scope: "test", Policy: sandboxcapacity.Policy{MaxActive: 2, TotalMemory: "128m", DefaultMemory: "64m", TotalCPU: "1", DefaultCPU: "0.5"}}}, dir
}

var fixtureCompanions = []domain.Companion{{Name: "cache", Image: "fixture", Command: []string{"sleep", "infinity"}}}

func TestComposeRuntimeIntegration(t *testing.T) {
	cfg, dir := composeFixture(t)
	s, err := NewComposeSandbox(cfg, fixtureCompanions)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "calls"))
	for _, want := range []string{"network create --internal", "--network-alias cache", "--cpus 0.500", "--user 10001:10001", "--memory-swap 67108864"} {
		if !strings.Contains(string(data), want) {
			t.Fatal(want, string(data))
		}
	}
	wait, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if err := cfg.Capacity.ReserveCPU(wait, "other", 64<<20, 500, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := s.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	// Recreate controller: persisted containers and definition labels are reused.
	restored, err := NewComposeSandbox(cfg, fixtureCompanions)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Exec(ctx, ExecRequest{Argv: []string{"true"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cfg.Name+"-svc-cache.running"), []byte("false"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Exec(ctx, ExecRequest{Argv: []string{"true"}}); err == nil {
		t.Fatal("runtime dependency failure hidden")
	}
	if err := restored.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restored.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Capacity.ReserveCPU(ctx, "other", 64<<20, 500, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "network")); !os.IsNotExist(err) {
		t.Fatal("network retained", err)
	}
}
func TestComposeFailuresRetainCapacityUntilConfirmedStop(t *testing.T) {
	cfg, dir := composeFixture(t)
	s, err := NewComposeSandbox(cfg, fixtureCompanions)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fail-stop"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Suspend(context.Background()); err == nil {
		t.Fatal("stop failure hidden")
	}
	wait, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := cfg.Capacity.ReserveCPU(wait, "other", 64<<20, 500, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "fail-stop")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fail-run"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	next, _ := NewComposeSandbox(cfg, fixtureCompanions)
	if err := next.Start(context.Background()); err == nil {
		t.Fatal("creation failure hidden")
	}
	if err := cfg.Capacity.ReserveCPU(context.Background(), "other", 64<<20, 500, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
}
func TestComposeRejectsChangedOwnershipAndOversize(t *testing.T) {
	cfg, dir := composeFixture(t)
	if err := os.WriteFile(filepath.Join(dir, cfg.Name+".hash"), []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewComposeSandbox(cfg, fixtureCompanions)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("foreign container adopted")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(data), "run ") || strings.Contains(string(data), "rm ") {
		t.Fatal(string(data))
	}
	cfg.Capacity.Policy.MaxActive = 1
	if _, err := NewComposeSandbox(cfg, fixtureCompanions); err == nil {
		t.Fatal("partial group admission")
	}
	cfg.Capacity.Policy.MaxActive = 2
	cfg.Network = "bridge"
	if _, err := NewComposeSandbox(cfg, fixtureCompanions); err == nil {
		t.Fatal("network widened")
	}
}

func TestEgressGroupLifecycleIntegration(t *testing.T) {
	cfg, dir := composeFixture(t)
	cfg.Egress = &EgressConfig{Image: "proxy:fixture", Hosts: []string{"example.com"}}
	if _, err := NewDockerSandbox(cfg); err == nil {
		t.Fatal("proxy config ignored by plain sandbox")
	}
	s, err := NewComposeSandbox(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "calls"))
	for _, want := range []string{"--internal", "--hosts example.com", "network connect bridge weft-sbx-test-svc-weft-egress", "--env HTTPS_PROXY=http://weft-egress:3128", "--dns 127.0.0.1"} {
		if !strings.Contains(string(data), want) {
			t.Fatal(want, string(data))
		}
	}
	if err := s.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := NewComposeSandbox(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = restored.Start(ctx); err != nil {
		t.Fatal(err)
	}
	changed := cfg
	changed.Egress = &EgressConfig{Image: "proxy:fixture", Hosts: []string{"other.example"}}
	denied, err := NewComposeSandbox(changed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = denied.Start(ctx); err == nil {
		t.Fatal("adopted stale allowlist")
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestEgressUplinkFailureStopsGroup(t *testing.T) {
	cfg, dir := composeFixture(t)
	cfg.Egress = &EgressConfig{Image: "proxy:fixture", Hosts: []string{"example.com"}}
	if err := os.WriteFile(filepath.Join(dir, "fail-uplink"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewComposeSandbox(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("uplink failure ignored")
	}
	data, err := os.ReadFile(filepath.Join(dir, cfg.Name+"-svc-weft-egress.running"))
	if err != nil || strings.TrimSpace(string(data)) != "false" {
		t.Fatal("proxy not stopped", string(data), err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneCannotAdoptProxyGroup(t *testing.T) {
	cfg, dir := composeFixture(t)
	cfg.RequireStandalone = true
	// Unit boundary: this caller must reject group ownership before starting or
	// updating any container, even if no admission manager was supplied.
	cfg.Capacity = nil
	if err := os.WriteFile(filepath.Join(dir, cfg.Name+".hash"), []byte("owned-proxy-group"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cfg.Name+".running"), []byte("false"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewDockerSandbox(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "managed service/proxy group") {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "start ") || strings.Contains(string(data), "update ") {
		t.Fatal("adopted group container", string(data))
	}
}

func TestComposeFinalCaptureFailurePreservesContainers(t *testing.T) {
	cfg, dir := composeFixture(t)
	failed := true
	cfg.BeforeRemove = func(context.Context) error {
		if failed {
			return errors.New("history storage unavailable")
		}
		return nil
	}
	s, err := NewComposeSandbox(cfg, fixtureCompanions)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err == nil {
		t.Fatal("capture failure hidden")
	}
	if _, err := os.Stat(filepath.Join(dir, cfg.Name+".hash")); err != nil {
		t.Fatal("container removed before capture", err)
	}
	failed = false
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, cfg.Name+".hash")); !os.IsNotExist(err) {
		t.Fatal("retry did not remove container", err)
	}
}
