package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestPreviewRealDocker(t *testing.T) {
	bin := os.Getenv("WEFT_TEST_ENV_DOCKER_BIN")
	if bin == "" {
		t.Skip("run make test-preview")
	}
	relay := os.Getenv("WEFT_TEST_PREVIEW_RELAY")
	daemon := os.Getenv("WEFT_TEST_SANDBOX_CONTAINER")
	if relay == "" || daemon == "" {
		t.Fatal("preview relay and dedicated daemon required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	docker := func(ctx context.Context, args ...string) (string, error) {
		out, e := tools.DockerCommand(ctx, bin, "", args...).CombinedOutput()
		return string(out), e
	}
	if out, e := docker(ctx, "image", "inspect", "weft/sandbox:latest"); e != nil {
		t.Fatalf("cached sandbox image required: %s %v", out, e)
	}
	m, r := fixture(t)
	r.ID = fmt.Sprintf("%032x", time.Now().UnixNano())
	r.Revision = 0
	r.Request.Command = []string{"python3", "app.py"}
	r.Request.Port = 5173
	r.Directory = filepath.Join(m.Root, r.ID)
	source := `cache='/home/sandbox/.preview-cache/value'
assert open(cache).read() == 'seeded'
open(cache,'w').write('private')
from http.server import HTTPServer, BaseHTTPRequestHandler
class App(BaseHTTPRequestHandler):
 def do_GET(self):
  self.send_response(200); self.send_header('Content-Type','text/html'); self.end_headers(); self.wfile.write(b'<h1>Review candidate</h1>')
 def do_POST(self):
  body=self.rfile.read(int(self.headers.get('Content-Length','0')))
  self.send_response(201); self.end_headers(); self.wfile.write(body)
HTTPServer(('127.0.0.1',5173),App).serve_forever()
`
	if e := os.WriteFile(filepath.Join(r.Source, "app.py"), []byte(source), 0644); e != nil {
		t.Fatal(e)
	}
	hash, e := Candidate(ctx, r.Source, filepath.Join(r.Directory, "candidate"))
	if e != nil {
		t.Fatal(e)
	}
	r.Candidate = hash
	if e = m.save(ctx, r); e != nil {
		t.Fatal(e)
	}
	active, e := tools.DockerCapacityInventory(ctx, docker)
	if e != nil {
		t.Fatal(e)
	}
	memory, cpu, count := int64(128<<20), int64(500), 2
	for _, c := range active {
		memory += c.Memory
		cpu += c.MilliCPU
		count += max(c.Count, 1)
	}
	budget := &sandboxcapacity.Manager{Store: m.Store, Scope: r.ID, Policy: sandboxcapacity.Policy{MaxActive: count, TotalMemory: strconv.FormatInt(memory, 10), DefaultMemory: "64m", TotalCPU: sandboxcapacity.FormatCPU(cpu), DefaultCPU: "0.25"}}
	driver := &DockerDriver{Bin: bin, RelayBinary: relay, Capacity: budget}
	remote := "/tmp/weft-preview-test-" + r.ID
	driver.StartSandbox = func(ctx context.Context, r *Record, work, control string) (tools.Sandbox, error) {
		if out, e := exec.CommandContext(ctx, "docker", "exec", daemon, "mkdir", "-p", remote).CombinedOutput(); e != nil {
			t.Fatalf("mkdir: %s %v", out, e)
		}
		config, _ := json.Marshal(map[string]any{"command": r.Request.Command, "cwd": "/workspace", "caches": []string{"/home/sandbox/.preview-cache"}})
		if e := WriteControl(control, config); e != nil {
			return nil, e
		}
		if out, e := exec.CommandContext(ctx, "docker", "exec", daemon, "sh", "-c", "mkdir -p "+remote+"/cache && printf seeded > "+remote+"/cache/value").CombinedOutput(); e != nil {
			return nil, fmt.Errorf("seed cache: %s: %w", out, e)
		}
		for _, pair := range [][2]string{{work, remote + "/work"}, {control, remote + "/control"}} {
			if out, e := exec.CommandContext(ctx, "docker", "exec", daemon, "mkdir", "-p", pair[1]).CombinedOutput(); e != nil {
				return nil, fmt.Errorf("prepare fixture: %s: %w", out, e)
			}
			archive, e := exec.CommandContext(ctx, "tar", "-C", pair[0], "-cf", "-", ".").Output()
			if e != nil {
				return nil, e
			}
			copy := exec.CommandContext(ctx, "docker", "exec", "-i", daemon, "tar", "-xf", "-", "-C", pair[1])
			copy.Stdin = bytes.NewReader(archive)
			if out, e := copy.CombinedOutput(); e != nil {
				return nil, fmt.Errorf("copy fixture: %s: %w", out, e)
			}
		}
		sb, e := tools.NewDockerSandbox(tools.DockerSandboxConfig{Name: r.Name(), Image: "weft/sandbox:latest", HostDir: remote + "/work", Volumes: []string{remote + "/control:/weft-preview:ro", remote + "/cache:/weft-preview-cache/home/sandbox/.preview-cache:ro"}, DockerBin: bin, Capacity: budget, Command: []string{"/weft-preview/weft-preview", "serve", "/weft-preview/config.json"}})
		if e != nil {
			return nil, e
		}
		return sb, sb.Start(ctx)
	}
	m.Driver = driver
	t.Cleanup(func() {
		for generation := 1; generation <= r.Generation; generation++ {
			if _, e := docker(context.Background(), "rm", "-f", fmt.Sprintf("weft-preview-%s-%d", r.ID, generation)); e != nil {
				t.Error(e)
			}
		}
		if out, e := exec.Command("docker", "exec", daemon, "rm", "-rf", remote).CombinedOutput(); e != nil {
			t.Error(string(out), e)
		}
	})
	if e = m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	r, _ = m.Get(ctx, r.ID)
	if r.Session != "ready" {
		logs, _ := driver.Logs(ctx, r)
		t.Fatalf("preview %s: %s; %s", r.Session, r.Error, logs)
	}
	if out, e := exec.CommandContext(ctx, "docker", "exec", daemon, "cat", remote+"/cache/value").CombinedOutput(); e != nil || string(out) != "seeded" {
		t.Fatal("preview mutated shared cache", string(out), e)
	}
	if out, e := docker(ctx, "inspect", "--format", "{{.HostConfig.NetworkMode}} {{len .HostConfig.PortBindings}}", r.Name()); e != nil || strings.TrimSpace(out) != "none 0" {
		t.Fatal("preview exposed network", out, e)
	}
	gateway := &Gateway{Manager: m, BaseURL: "http://localhost:8082"}
	srv := httptest.NewServer(gateway)
	defer srv.Close()
	token, e := m.Issue(ctx, r.ID, "operator", false)
	if e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/api", strings.NewReader("human test"))
	req.Host = r.ID + ".localhost:8082"
	req.Header.Set(PreviewHeader, token)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 201 || string(body) != "human test" {
		t.Fatal(res.Status, string(body))
	}
	// Exercise cURL as a real external client without printing its credential.
	out, e := exec.CommandContext(ctx, "curl", "--silent", "--fail", "-H", "Host: "+req.Host, "-H", PreviewHeader+": "+token, srv.URL).CombinedOutput()
	if e != nil || !strings.Contains(string(out), "Review candidate") {
		t.Fatal("curl could not access preview", e)
	}
	// A fresh controller reconnects to the persisted running generation.
	m = New(Options{Store: m.Store, Root: m.Root, Instance: "recovered", Driver: driver})
	gateway.Manager = m
	if e = m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Authorize(ctx, r.ID, token, false); e != nil {
		t.Fatal("lost durable access", e)
	}
	// Expiring only the preview stops compute but preserves the pending decision.
	r, _ = m.Get(ctx, r.ID)
	r.SessionExpires = time.Now().Add(-time.Second)
	if e = m.save(ctx, r); e != nil {
		t.Fatal(e)
	}
	if e = m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	r, _ = m.Get(ctx, r.ID)
	if r.Session != "expired" || r.State != "pending" {
		t.Fatal(r.Session, r.State)
	}
	if _, e = m.Authorize(ctx, r.ID, token, false); e == nil {
		t.Fatal("expired token accepted")
	}
	r, e = m.Restart(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	r, _ = m.Get(ctx, r.ID)
	if r.Generation != 2 || r.Session != "ready" {
		t.Fatal(r.Generation, r.Session, r.Error)
	}
	if _, e = m.Authorize(ctx, r.ID, token, false); e == nil {
		t.Fatal("previous generation token accepted")
	}
	if _, e = m.Decide(ctx, r.ID, "approved", "managed", "operator", "validated HTTP", int64(r.Generation)); e != nil {
		t.Fatal(e)
	}
	if e = m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if out, e := docker(ctx, "inspect", "--format", "{{.State.Running}}", r.Name()); e != nil || strings.TrimSpace(out) != "false" {
		t.Fatal("not stopped", out, e)
	}
}
