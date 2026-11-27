//go:build e2e

// weft-provider-e2e is a disposable loopback installation with a compiled Go
// Eino runtime and simulated external inference. It never uses Docker.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
)

const adminToken = "e2e-provider-admin-token"

type fixture struct {
	app             *app.App
	server          *httptest.Server
	model           *httptest.Server
	cancel          context.CancelFunc
	runtime         *exec.Cmd
	root            string
	logs            *os.File
	callbackFailure chan string
}

func (f *fixture) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if f.app != nil {
		f.app.Engine.Shutdown(ctx)
	}
	if f.server != nil {
		f.server.Close()
	}
	f.cancel()
	if f.runtime != nil {
		_ = f.runtime.Wait()
	}
	if f.model != nil {
		f.model.Close()
	}
	if f.app != nil {
		_ = f.app.Close(ctx)
	}
	if f.logs != nil {
		_ = f.logs.Close()
	}
	_ = os.RemoveAll(f.root)
}

func newFixture(addr string) (_ *fixture, err error) {
	root, err := os.MkdirTemp("", "weft-provider-")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &fixture{root: root, cancel: cancel, callbackFailure: make(chan string, 1)}
	defer func() {
		if err != nil {
			f.close()
		}
	}()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	// Assigned immediately so failed setup also closes the reserved listener.
	f.server = httptest.NewUnstartedServer(nil)
	_ = f.server.Listener.Close()
	f.server.Listener = listener
	base := "http://" + listener.Addr().String()
	repo, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(repo, "agents/eino/go.mod")); statErr == nil {
			break
		}
		parent := filepath.Dir(repo)
		if parent == repo {
			return nil, fmt.Errorf("run inside the Weft checkout")
		}
		repo = parent
	}
	binary := filepath.Join(root, "provider-fixture")
	build := exec.CommandContext(ctx, "go", "build", "-tags", "e2e", "-o", binary, "./cmd/provider-fixture")
	build.Dir = filepath.Join(repo, "agents/eino")
	if out, e := build.CombinedOutput(); e != nil {
		return nil, fmt.Errorf("build Eino fixture: %w: %s", e, out)
	}
	f.logs, err = os.Create(filepath.Join(root, "runtime.log"))
	if err != nil {
		return nil, err
	}
	f.runtime = exec.CommandContext(ctx, binary, root)
	// Explicit allowlist: no host credentials or provider configuration enters
	// this process. It receives only execution-scoped callbacks over HTTP.
	f.runtime.Env = []string{"WEFT_URL=" + base}
	f.runtime.Stdout, f.runtime.Stderr = f.logs, f.logs
	if err = f.runtime.Start(); err != nil {
		f.runtime = nil
		return nil, err
	}
	var runtimeURL []byte
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		runtimeURL, err = os.ReadFile(filepath.Join(root, "runtime-url"))
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return nil, fmt.Errorf("Eino fixture did not start: %w", err)
	}
	f.model = httptest.NewServer(&modelFixture{})
	cfg := config.Default()
	cfg.Instance = "provider-browser-e2e"
	cfg.LLM = config.LLM{DevMock: true}
	cfg.Server.InternalURL, cfg.Server.InternalAddr = base, ""
	cfg.Server.WebDir = filepath.Join(repo, "web/dist")
	cfg.Storage = config.Storage{Type: "sqlite", Dir: filepath.Join(root, "store")}
	cfg.Queue = config.Queue{Type: "memory"}
	cfg.Workspaces.Root = filepath.Join(root, "workspaces")
	cfg.Secrets = config.Secrets{KeyFile: filepath.Join(root, "fixture.key"), GenerateKeyFile: true}
	cfg.Auth.Tokens = []config.Token{{Name: "fixture-admin", Role: config.RoleAdmin, Token: adminToken}}
	cfg.Observability.LogLevel = "error"
	cfg.Engine.ReconcileInterval = 50 * time.Millisecond
	if err = cfg.Validate(); err != nil {
		return nil, err
	}
	f.app, err = app.New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	for _, id := range []string{"provider-success", "provider-failure", "provider-missing"} {
		g := &domain.Graph{Metadata: domain.GraphMetadata{Name: id}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "writer", Prompt: "Use the selected provider.", Runtime: domain.RuntimeRef{Kind: domain.RuntimeExternal, Framework: "eino", Agent: "writer", Endpoint: string(runtimeURL)}, Loop: domain.LoopSpec{Mode: domain.LoopOnce}}}}}
		if id == "provider-missing" {
			g.Spec.Nodes[0].Model.Provider = "not-configured"
		}
		if _, _, err = f.app.Engine.SaveGraph(ctx, g, 0, "e2e-fixture"); err != nil {
			return nil, err
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/fixture", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+adminToken {
			http.Error(w, "unauthorized", 401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"modelURL": f.model.URL + "/v1"})
	})
	api := f.app.API.Handler()
	mux.HandleFunc("/internal/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/llm/chat") {
			api.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, r)
		if rec.Code == http.StatusBadGateway {
			select {
			case f.callbackFailure <- rec.Body.String():
			default:
			}
		}
		for k, vs := range rec.Header() {
			w.Header()[k] = vs
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	})
	mux.Handle("/", api)
	f.server.Config.Handler = mux
	f.server.Start()
	if err = f.app.Engine.Start(ctx); err != nil {
		return nil, err
	}
	return f, nil
}

func main() {
	port := os.Getenv("WEFT_PROVIDER_E2E_PORT")
	if port == "" {
		port = "18102"
	}
	f, err := newFixture("127.0.0.1:" + port)
	if err != nil {
		log.Fatal(err)
	}
	defer f.close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}

func containsCredential(b []byte) bool {
	return strings.Contains(string(b), fixtureKey) || strings.Contains(string(b), wrongKey)
}
