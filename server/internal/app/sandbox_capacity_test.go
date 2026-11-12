package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestConfiguredSandboxBudgetWiring(t *testing.T) {
	cfg := config.Default()
	if e := yaml.Unmarshal([]byte("auth: {disabled: true}\nisolation:\n  docker:\n    capacity: {maxActive: 1, totalMemory: 128m, defaultMemory: 64m}\n"), cfg); e != nil {
		t.Fatal(e)
	}
	if e := cfg.Validate(); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "docker")
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\ncase \"$1\" in inspect) echo 'No such container'; exit 1;; esac\n"
	if e := os.WriteFile(bin, []byte(script), 0755); e != nil {
		t.Fatal(e)
	}
	cfg.Isolation.Docker.Bin = bin
	store, e := fsstore.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	bus := events.NewBus(store, slog.Default())
	factory := sandboxFactory(cfg, nil, store, bus)
	spec := engine.SandboxSpec{Graph: &domain.Graph{Metadata: domain.GraphMetadata{ID: "capacity"}}, Node: &domain.Node{ID: "worker", Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker, Image: "fixture"}}, HostWorkdir: dir}
	sb, e := factory(context.Background(), spec)
	if e != nil {
		t.Fatal(e)
	}
	if e := sb.Close(); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(log)
	if e != nil || !strings.Contains(string(data), "--memory 67108864 --memory-swap 67108864") {
		t.Fatalf("%s %v", data, e)
	}
	evs, e := store.ListEvents(context.Background(), storage.EventFilter{GraphID: "capacity", Ascending: true})
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, ev := range evs {
		if ev.Type == "sandbox.capacity" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("admission/release events: %+v", evs)
	}
	spec.Node.Isolation.Memory = "256m"
	if _, e := factory(context.Background(), spec); e == nil {
		t.Fatal("over-budget node accepted")
	}
}

func TestInstallationPolicyRejectsNodeNetworkOverride(t *testing.T) {
	cfg := config.Default()
	cfg.Isolation.Docker.EnvironmentPolicy.RunNetworks = []string{"none"}
	cfg.Isolation.Docker.Bin = "/nonexistent-policy-test-docker"
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	factory := sandboxFactory(cfg, nil, store, events.NewBus(store, slog.Default()))
	for _, network := range []string{"bridge", "host", "container:other", "custom"} {
		spec := engine.SandboxSpec{Graph: &domain.Graph{Metadata: domain.GraphMetadata{ID: "policy"}}, Node: &domain.Node{ID: "worker", Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker, Image: "alpine", Network: network}}, HostWorkdir: t.TempDir()}
		if _, err := factory(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "installation environment policy") {
			t.Fatalf("override %s: %v", network, err)
		}
	}
}
