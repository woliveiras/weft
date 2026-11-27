package toolchain_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const composeFile = "../../../docker-compose.yml"

func TestComposeSourceBindings(t *testing.T) {
	source, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, port := range []int{8080, 9090, 3000, 11434} {
		if !strings.Contains(string(source), fmt.Sprintf("${WEFT_BIND_ADDRESS:-127.0.0.1}:%d:%d", port, port)) {
			t.Errorf("missing default loopback binding for %d", port)
		}
	}
	if !strings.Contains(string(source), `"127.0.0.1:8082:8082"`) || strings.Contains(string(source), "/var/run/docker.sock") {
		t.Fatal("unexpected preview or host socket binding")
	}
}

type composeService struct {
	Ports []struct {
		HostIP string `json:"host_ip"`
	}
	Networks map[string]json.RawMessage
	Restart  string
	Volumes  []struct{ Type, Source string }
}

func TestComposeRenderedInstallation(t *testing.T) {
	// Only config interpolation runs. The CLI has no daemon socket in the test image.
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("Docker CLI is required to render Compose; make test supplies it")
	}
	file, err := filepath.Abs(composeFile)
	if err != nil {
		t.Fatal(err)
	}
	credentials := map[string]string{"WEFT_ADMIN_TOKEN": "fixture-admin", "WEFT_RUNTIME_SECRET": "fixture-runtime", "POSTGRES_PASSWORD": "fixture-db", "GRAFANA_ADMIN_PASSWORD": "fixture-grafana"}
	render := func(overrides map[string]string) ([]byte, error) {
		t.Helper()
		values := maps.Clone(credentials)
		maps.Copy(values, overrides)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, docker, "compose", "--env-file", os.DevNull, "-f", file, "--profile", "ollama", "config", "--format", "json")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
		for k, v := range values {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		return cmd.CombinedOutput()
	}
	for _, host := range []string{"127.0.0.1", "192.0.2.10"} {
		options := map[string]string{}
		if host != "127.0.0.1" {
			options["WEFT_BIND_ADDRESS"] = host
		}
		raw, err := render(options)
		if err != nil {
			t.Fatalf("render configuration: %v %s", err, raw)
		}
		var config struct{ Services map[string]composeService }
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatal(err)
		}
		svc := config.Services
		for _, name := range []string{"weftd", "grafana", "prometheus", "ollama"} {
			ports := svc[name].Ports
			if len(ports) == 0 || ports[0].HostIP != host {
				t.Fatalf("%s bindings: %+v", name, ports)
			}
			for _, p := range ports[1:] {
				if p.HostIP != "127.0.0.1" {
					t.Fatalf("%s additional port: %+v", name, p)
				}
			}
		}
		if len(svc["weftd"].Ports) != 2 {
			t.Fatal("missing preview port")
		}
		for _, name := range []string{"postgres", "nats", "agents", "sandbox-daemon", "sandbox-image", "tempo", "loki", "otel-collector"} {
			s, ok := svc[name]
			if !ok || len(s.Ports) != 0 {
				t.Errorf("internal service %s", name)
			}
		}
		for name, network := range map[string]string{"sandbox-daemon": "sandbox", "agents": "default"} {
			if !slices.Equal(slices.Collect(maps.Keys(svc[name].Networks)), []string{network}) {
				t.Errorf("%s networks: %v", name, svc[name].Networks)
			}
		}
		for _, name := range []string{"weftd", "postgres", "nats", "agents", "sandbox-daemon"} {
			if svc[name].Restart != "unless-stopped" {
				t.Errorf("restart policy: %s", name)
			}
		}
		for _, v := range [][2]string{{"weftd", "weftd-data"}, {"weftd", "workspaces"}, {"postgres", "pg-data"}, {"nats", "nats-data"}, {"sandbox-daemon", "sandbox-daemon-data"}} {
			found := false
			for _, m := range svc[v[0]].Volumes {
				if m.Type == "volume" && m.Source == v[1] {
					found = true
				}
			}
			if !found {
				t.Errorf("missing volume %s on %s", v[1], v[0])
			}
		}
	}
	for key := range credentials {
		out, err := render(map[string]string{key: ""})
		if err == nil || !strings.Contains(string(out), key) {
			t.Errorf("missing %s was not rejected", key)
		}
	}
}
