//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
)

// Only inference is scripted. Tool calls pass through the native runtime and
// gateway to the real Docker sandbox. Never turn a failed tool into success.
type realProvider struct{ analysisProvider }

const miseProposal = `{"environment":{"dockerfile":"FROM ghcr.io/jdx/mise:2026.10.4-debian@sha256:2fb7fa9a4120333531a8a70296d4ea152ffd3e7658dcf29ac75774a7d78dec24\nENV MISE_DATA_DIR=/home/sandbox/.local/share/mise MISE_CONFIG_DIR=/home/sandbox/.config/mise MISE_CACHE_DIR=/home/sandbox/.cache/mise\n","setup":["mise trust /workspace/mise.toml","mise install","mise exec -- pnpm install --frozen-lockfile --ignore-scripts --store-dir /home/sandbox/.local/share/pnpm/store"],"caches":["/home/sandbox/.local/share/pnpm/store"],"network":{"setup":"bridge","run":"none"}},"rationale":"Pinned mise and Node toolchain; setup downloads Node and locked pnpm packages using bridge network. Runtime is offline. pnpm lifecycle scripts are disabled; toolchain installation and mutable workspace remain private to each task.","tools":["mise","node","pnpm"]}`

func (realProvider) Chat(ctx context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	if r.Metadata["graph"] == "fixture-preview" {
		return previewInference(r)
	}
	if r.Metadata["purpose"] == "environment.proposal" {
		for _, m := range r.Messages {
			if m.Role == llm.RoleUser && strings.Contains(m.Content, "weft-mise-example") {
				return &llm.ChatResponse{Message: llm.ChatMessage{Content: miseProposal}}, nil
			}
		}
		return analysisProvider{}.Chat(ctx, r)
	}
	for _, m := range r.Messages {
		if m.Role == llm.RoleTool {
			if !strings.HasPrefix(m.Content, "exit code: 0\n") {
				return nil, fmt.Errorf("real sandbox command failed: %s", m.Content)
			}
			return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Real shell verified: " + m.Content}, StopReason: "stop"}, nil
		}
	}
	marker := ""
	for _, m := range r.Messages {
		if m.Role == llm.RoleUser {
			for _, id := range []string{"fixture-real-a", "fixture-real-b", "fixture-source-weft", "fixture-source-devcontainer", "fixture-source-dockerfile", "fixture-source-compose", "fixture-mise-a", "fixture-mise-b"} {
				if m.Content == id {
					marker = id
				}
			}
		}
	}
	if marker == "" {
		return nil, fmt.Errorf("unknown real fixture task")
	}
	command := `test "$(id -u)" = 10001; test ! -e result.txt; printf '%s\n' '` + marker + `' > result.txt; printf 'uid=%s workspace=%s result=' "$(id -u)" "$PWD"; cat result.txt`
	if marker == "fixture-source-weft" {
		command += `; printf '{"lockfileVersion":3}' > package-lock.json`
	}
	if strings.HasPrefix(marker, "fixture-mise-") {
		command += `; test "$(mise exec -- node --version)" = v22.14.0; test "$(mise exec -- pnpm --version)" = 10.33.4; mise exec -- pnpm run build; mise exec -- pnpm test`
	}
	args, err := json.Marshal(map[string]any{"argv": []string{"sh", "-ec", command}})
	if err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "real-shell", Name: "shell.exec", Arguments: args}}}, StopReason: "tool_calls"}, nil
}

func runReal() error {
	// This process runs inside the existing dedicated daemon container. Both
	// controller and daemon see exactly the same temporary workspace paths.
	root, err := os.MkdirTemp("", "weft-browser-real-data-")
	if err != nil {
		return err
	}
	removeRoot := true
	defer func() {
		if removeRoot {
			_ = os.RemoveAll(root)
		}
	}()
	if err := os.Chmod(root, 0755); err != nil {
		return err
	}
	if err := os.WriteFile("pid", []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		return err
	}
	cfg := config.Default()
	cfg.Instance = filepath.Base(root)
	cfg.Preview.Addr = "127.0.0.1:18092"
	cfg.Preview.BaseURL = "http://localhost:18092"
	cfg.Preview.ConsoleOrigins = []string{"http://127.0.0.1:18090"}
	cfg.Preview.RelayBinary = "weft-preview-linux"
	cfg.LLM = config.LLM{}
	cfg.Server.Addr, cfg.Server.InternalAddr, cfg.Server.WebDir = "127.0.0.1:18090", "", "web"
	cfg.Storage = config.Storage{Type: "sqlite", Dir: filepath.Join(root, "store")}
	cfg.Queue = config.Queue{Type: "memory"}
	cfg.Workspaces.Root = filepath.Join(root, "ws")
	cfg.Secrets = config.Secrets{KeyFile: filepath.Join(root, "fixture.key"), GenerateKeyFile: true}
	cfg.Auth.Tokens = []config.Token{{Name: "e2e-admin", Role: config.RoleAdmin, Token: "e2e-admin-token"}}
	cfg.Isolation.Docker.Bin, cfg.Isolation.Docker.Host = "docker", ""
	cfg.Isolation.Docker.Capacity = sandboxcapacity.Policy{TotalCPU: "1", DefaultCPU: "0.5", TotalMemory: "512m", DefaultMemory: "256m", MaxActive: 2}
	cfg.Observability.LogLevel = "error"
	cfg.Engine.ReconcileInterval = 100 * time.Millisecond
	if err := cfg.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := app.New(ctx, cfg)
	if err != nil {
		return err
	}
	a.Models.Add(llm.ProviderConfig{Name: "e2e", DefaultModel: "fixture"}, realProvider{})
	ids := []string{"fixture-real-a", "fixture-real-b", "fixture-source-weft", "fixture-source-devcontainer", "fixture-source-dockerfile", "fixture-source-compose", "fixture-mise-a", "fixture-mise-b"}
	for _, id := range ids {
		dir := filepath.Join(cfg.Workspaces.Root, id)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
			return fmt.Errorf("fixture git: %w: %s", err, out)
		}
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"real-shell-fixture"}`), 0644); err != nil {
			return err
		}
		if strings.HasPrefix(id, "fixture-mise-") {
			for _, name := range []string{".dockerignore", "mise.toml", "package.json", "pnpm-lock.yaml", "sum.cjs", "sum.test.cjs"} {
				data, err := os.ReadFile(filepath.Join("mise-node", name))
				if err != nil {
					return fmt.Errorf("mise fixture %s: %w", name, err)
				}
				if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
					return err
				}
			}
		}
		definitions := map[string]struct{ path, body string }{
			"fixture-source-weft":         {".weft/environment.yaml", "image: alpine:3.22\nnetwork: {setup: none, run: none}\n"},
			"fixture-source-devcontainer": {".devcontainer/devcontainer.json", `{"image":"alpine:3.22","postCreateCommand":"test $(id -u) = 10001"}`},
			"fixture-source-dockerfile":   {"Dockerfile", "FROM alpine:3.22\nCOPY package.json /manifest\n"},
			"fixture-source-compose":      {"compose.yaml", "services:\n  dev:\n    image: alpine:3.22\n"},
		}
		if definition, ok := definitions[id]; ok {
			filename := filepath.Join(dir, definition.path)
			if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(filename, []byte(definition.body), 0644); err != nil {
				return err
			}
		}
		if err := os.Chmod(dir, 0777); err != nil {
			return err
		}
		g := &domain.Graph{Metadata: domain.GraphMetadata{Name: id}, Spec: domain.GraphSpec{
			Workspace: domain.WorkspaceSpec{Repo: "https://example.invalid/" + cfg.Instance + ".git"},
			Nodes:     []domain.Node{{ID: "worker", Prompt: id, Runtime: domain.RuntimeRef{Kind: domain.RuntimeNative}, Loop: domain.LoopSpec{Mode: domain.LoopOnce}, Tools: []domain.ToolGrant{{Name: "shell.exec", Commands: []string{"sh"}}}, Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}}},
		}}
		if _, _, err := a.Engine.SaveGraph(ctx, g, 0, "fixture"); err != nil {
			return err
		}
	}
	if err := seedPreview(ctx, a, cfg); err != nil {
		return err
	}
	ids = append(ids, "fixture-preview")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(signals)
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case err := <-done:
		return err
	case <-signals:
	}
	cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
	defer stop()
	var errs []error
	for _, id := range ids {
		if err := a.Engine.GraphAction(cleanup, id, "kill", "fixture-cleanup"); err != nil {
			errs = append(errs, err)
		}
	}
	for _, id := range ids {
		for {
			status, err := a.Engine.Status(cleanup, id)
			if err != nil {
				errs = append(errs, err)
				break
			}
			if status.State == domain.GraphStopped {
				break
			}
			select {
			case <-cleanup.Done():
				errs = append(errs, cleanup.Err())
			case <-time.After(100 * time.Millisecond):
				continue
			}
			break
		}
		if err := a.Environments.Reset(cleanup, id, "fixture-cleanup"); err != nil {
			errs = append(errs, err)
		}
	}
	if reviews, e := a.Reviews.List(cleanup); e != nil {
		errs = append(errs, e)
	} else {
		for _, r := range reviews {
			if e = a.Reviews.Driver.Stop(cleanup, r); e != nil {
				errs = append(errs, e)
			}
			if out, e := exec.CommandContext(cleanup, "docker", "rm", "-f", r.Name()).CombinedOutput(); e != nil {
				errs = append(errs, fmt.Errorf("preview cleanup: %s: %w", out, e))
			}
		}
	}
	preview, err := a.Environments.CleanupShared(cleanup, nil, true, "fixture-cleanup")
	if err != nil {
		errs = append(errs, err)
	} else {
		var generations []string
		for _, item := range preview.Generations {
			if item.Eligible {
				generations = append(generations, item.ID)
			}
		}
		if len(generations) > 0 {
			result, err := a.Environments.CleanupShared(cleanup, generations, false, "fixture-cleanup")
			if err != nil {
				errs = append(errs, err)
			} else {
				for _, item := range result.Generations {
					if !item.Removed {
						errs = append(errs, fmt.Errorf("cleanup %s: %s", item.ID, item.Error))
					}
				}
			}
		}
	}
	cancel()
	errs = append(errs, <-done)
	err = errors.Join(errs...)
	if err != nil {
		removeRoot = false
		return fmt.Errorf("real E2E cleanup failed; data retained at %s: %w", root, err)
	}
	return nil
}
