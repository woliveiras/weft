package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/eventtest"
)

const (
	adminTok    = "admin-token-123456"
	operatorTok = "operator-token-123456"
	viewerTok   = "viewer-token-123456"
)

func setup(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := setupApp(t, nil)
	return srv
}

// setupApp starts a complete control plane (SQLite, in-memory queue);
// tweak may adjust the configuration first.
func setupApp(t *testing.T, tweak func(*config.Config)) (*httptest.Server, *app.App) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Instance = "test"
	cfg.Storage = config.Storage{Type: "sqlite", Dir: dir}
	cfg.Queue = config.Queue{Type: "memory"}
	cfg.Workspaces.Root = filepath.Join(dir, "ws")
	cfg.Secrets = config.Secrets{KeyFile: filepath.Join(dir, "secret.key"), GenerateKeyFile: true}
	cfg.Observability.LogLevel = "error"
	cfg.Engine.ReconcileInterval = 100 * time.Millisecond
	cfg.LLM.DevMock = true // the deterministic development provider
	cfg.Auth.Tokens = []config.Token{
		{Name: "admin", Role: config.RoleAdmin, Token: adminTok},
		{Name: "operator", Role: config.RoleOperator, Token: operatorTok},
		{Name: "viewer", Role: config.RoleViewer, Token: viewerTok},
	}
	if tweak != nil {
		tweak(cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a, err := app.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.API.Handler())
	t.Cleanup(func() {
		srv.Close()
		cancel()
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		a.Engine.Shutdown(sctx)
		defer a.Close(sctx)
		eventtest.ValidateStored(t, a.Store)
	})
	return srv, a
}

func call(t *testing.T, srv *httptest.Server, method, path, tok, ctype, body string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, b, err)
		}
	}
	return resp.StatusCode
}

const doc = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: api-demo}
spec:
  defaults: {runtime: {kind: echo}}
  nodes: [{id: a}, {id: b}]
  channels:
    - {id: in, to: [a]}
    - {id: ab, from: a, to: [b]}
`

func TestAPIFlow(t *testing.T) {
	srv := setup(t)

	// Authentication and roles.
	if c := call(t, srv, "GET", "/api/v1/graphs", "", "", "", nil); c != 401 {
		t.Fatalf("anonymous list: %d", c)
	}
	if c := call(t, srv, "GET", "/api/v1/graphs", "wrong-token-000000", "", "", nil); c != 401 {
		t.Fatalf("bad token: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs", viewerTok, "application/yaml", doc, nil); c != 403 {
		t.Fatalf("viewer create: %d", c)
	}
	var info map[string]any
	call(t, srv, "GET", "/api/v1/info", viewerTok, "", "", &info)
	if info["nativeProfile"] != false {
		t.Fatal("standard server advertised native host execution")
	}
	if id, _ := info["identity"].(map[string]any); id["role"] != "viewer" {
		t.Fatalf("identity: %v", info["identity"])
	}

	// Validation errors are reported with paths.
	var bad struct {
		Code    string `json:"code"`
		Details struct {
			Errors []struct{ Path string } `json:"errors"`
		} `json:"details"`
	}
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", strings.Replace(doc, "to: [b]", "to: [ghost]", 1), &bad); c != 422 || len(bad.Details.Errors) == 0 {
		t.Fatalf("invalid graph: %d %+v", c, bad)
	}

	// Create (YAML), conflict on duplicate, optimistic concurrency on update.
	var sum struct {
		Graph struct {
			Metadata struct{ ID string }
		}
		Record struct{ Version int }
	}
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, &sum); c != 201 || sum.Graph.Metadata.ID != "api-demo" {
		t.Fatalf("create: %d %+v", c, sum)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); c != 409 {
		t.Fatalf("duplicate create: %d", c)
	}
	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/graphs/api-demo", strings.NewReader(doc))
	req.Header.Set("Authorization", "Bearer "+adminTok)
	req.Header.Set("Content-Type", "application/yaml")
	req.Header.Set("If-Match", "99")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 409 {
		t.Fatalf("stale If-Match: %d", resp.StatusCode)
	}

	// Subscribe to the event stream before acting, from "now".
	sreq, _ := http.NewRequest("GET", srv.URL+"/api/v1/events/stream?graph=api-demo&access_token="+viewerTok, nil)
	sresp, err := http.DefaultClient.Do(sreq)
	if err != nil || sresp.StatusCode != 200 {
		t.Fatalf("stream: %v %v", err, sresp)
	}
	defer sresp.Body.Close()
	events := make(chan string, 256)
	go func() {
		sc := bufio.NewScanner(sresp.Body)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "event: ") {
				events <- strings.TrimPrefix(l, "event: ")
			}
		}
	}()

	// Start and inject; the message reaches b.
	if c := call(t, srv, "POST", "/api/v1/graphs/api-demo/start", adminTok, "", "", nil); c != 200 {
		t.Fatalf("start: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/api-demo/channels/in/messages", adminTok, "application/json", `{"type":"job","payload":{"n":1}}`, nil); c != 201 {
		t.Fatalf("inject: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/api-demo/channels/nope/messages", adminTok, "application/json", `{"type":"job"}`, nil); c != 404 {
		t.Fatalf("inject unknown channel: %d", c)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var recs []struct {
			Status string `json:"status"`
		}
		call(t, srv, "GET", "/api/v1/graphs/api-demo/messages?channel=ab&status=acked", viewerTok, "", "", &recs)
		if len(recs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("message never reached b")
		}
		time.Sleep(100 * time.Millisecond)
	}
	seen := map[string]bool{}
	timeout := time.After(5 * time.Second)
	for !(seen["graph.started"] && seen["message.published"] && seen["execution.completed"]) {
		select {
		case ev := <-events:
			seen[ev] = true
		case <-timeout:
			t.Fatalf("stream missed events, saw %v", seen)
		}
	}

	// Status, and a running graph cannot be deleted.
	var st struct {
		DesiredState string
		Nodes        []struct{ NodeID string }
	}
	call(t, srv, "GET", "/api/v1/graphs/api-demo/status", viewerTok, "", "", &st)
	if st.DesiredState != "running" || len(st.Nodes) != 2 {
		t.Fatalf("status: %+v", st)
	}
	if c := call(t, srv, "DELETE", "/api/v1/graphs/api-demo", adminTok, "", "", nil); c != 409 {
		t.Fatalf("delete running: %d", c)
	}

	// The callback API refuses unknown executions and bad tokens.
	if c := call(t, srv, "POST", "/internal/v1/executions/exe_nope/tools/filesystem.read", adminTok, "application/json", `{}`, nil); c != 401 {
		t.Fatalf("callback with API token: %d", c)
	}

}
