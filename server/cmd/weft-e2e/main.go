//go:build e2e

// weft-e2e is a disposable, loopback-only browser test installation. It is
// deliberately excluded from production builds. No real Docker or LLM is used.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/mcpconfig/mcptest"
	"github.com/woliveiras/weft/server/internal/repositoryimport"
	"github.com/woliveiras/weft/server/internal/repositoryimport/importtest"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
)

type analysisProvider struct{}

func (analysisProvider) Name() string                                    { return "e2e" }
func (analysisProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (analysisProvider) Chat(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	if r.Metadata["operation"] == "issue_readiness" {
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: `{"missing":["acceptance"]}`}}, nil
	}
	if strings.Contains(r.System, "Weft task request interpreter") {
		var in struct {
			Request          string `json:"request"`
			SelectedWorkflow string `json:"selectedWorkflow"`
		}
		if err := json.Unmarshal([]byte(r.Messages[0].Content), &in); err != nil {
			return nil, err
		}
		if strings.Contains(in.Request, "fixture-model-error") {
			return nil, fmt.Errorf("fixture model unavailable")
		}
		raw := `{"outcome":"implement","workflowId":"","repository":"","clarification":false}`
		if strings.Contains(in.Request, "https://repository.fixture.example/source.git") {
			raw = `{"outcome":"implement","workflowId":"","repository":"https://repository.fixture.example/source.git","clarification":false}`
		}
		if strings.Contains(in.Request, "fixture-ambiguous") {
			raw = `{"outcome":"freeform","clarification":true}`
		}
		if strings.Contains(in.Request, "fixture-invalid") {
			raw = `{"outcome":"implement","tools":["shell.exec"]}`
		}
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: raw}}, nil
	}

	if len(r.Messages) > 0 && strings.Contains(r.Messages[0].Content, "repository-guidance-browser") {
		context := r.Messages[0].Content
		switch {
		case !strings.Contains(context, "Browser repository body."):
			return guidanceFixtureCall("repository:browser-review", ""), nil
		case !strings.Contains(context, "Browser reference body."):
			return guidanceFixtureCall("repository:browser-review", "references/checklist.md"), nil
		case !strings.Contains(context, "Browser console body."):
			return guidanceFixtureCall("console:browser-guidance", ""), nil
		default:
			return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Guidance exercised."}}, nil
		}
	}

	for _, message := range r.Messages {
		if match := regexp.MustCompile(`Task issue source: read using (mcp\.[a-z0-9_-]+\.[A-Za-z_]+) with arguments (\{[^\n]+\})\.`).FindStringSubmatch(message.Content); match != nil {
			for _, result := range r.Messages {
				if result.Role == llm.RoleTool {
					return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Issue result: " + result.Content}, StopReason: "stop"}, nil
				}
			}
			return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "issue-read", Name: match[1], Arguments: json.RawMessage(match[2])}}}, StopReason: "tool_calls"}, nil
		}
	}

	if strings.Contains(r.System, "temporary analysis assistant") {
		if len(r.Tools) != 0 {
			recursive := false
			for _, tool := range r.Tools {
				if tool.Name == "delegation.request" {
					recursive = true
				}
			}
			if recursive {
				for _, message := range r.Messages {
					if message.Role == llm.RoleTool {
						return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Nested fixture: " + message.Content}, Usage: llm.Usage{InputTokens: 7, OutputTokens: 3}}, nil
					}
				}
				if strings.Contains(r.Messages[0].Content, "leaf") {
					return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Recursive leaf completed"}, Usage: llm.Usage{InputTokens: 7, OutputTokens: 3}}, nil
				}
				return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "recursive-child", Name: "delegation.request", Arguments: json.RawMessage(`{"template":"browser-recursive","objective":"leaf"}`)}}}, Usage: llm.Usage{InputTokens: 7, OutputTokens: 3}}, nil
			}
			for _, message := range r.Messages {
				if message.Role == llm.RoleTool {
					return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Delegated inherited tool completed"}, Usage: llm.Usage{InputTokens: 7, OutputTokens: 3}}, nil
				}
			}
			return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "child-list", Name: "filesystem.list", Arguments: json.RawMessage(`{}`)}}}, Usage: llm.Usage{InputTokens: 7, OutputTokens: 3}}, nil
		}
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Delegated fixture result"}, Usage: llm.Usage{InputTokens: 7, OutputTokens: 3}}, nil
	}
	if len(r.Messages) > 0 && strings.Contains(r.Messages[0].Content, "release-one-journey") {
		return journeyResponse(r)
	}
	if len(r.Messages) > 0 && strings.Contains(r.Messages[0].Content, "delegation-fixture") {
		for _, m := range r.Messages {
			if m.Role == llm.RoleTool {
				return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Parent received: " + m.Content}}, nil
			}
		}
		profile := "analysis"
		if strings.Contains(r.Messages[0].Content, "recursive-profile") {
			profile = "browser-recursive"
		}
		if strings.Contains(r.Messages[0].Content, "inherited-profile") {
			profile = "browser-inherited"
		}
		if strings.Contains(r.Messages[0].Content, "custom-profile") {
			profile = "browser-review"
		}
		args, _ := json.Marshal(map[string]string{"template": profile, "objective": "Summarize supplied fixture text without tools"})
		return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "delegate", Name: "delegation.request", Arguments: args}}}}, nil
	}

	if len(r.Messages) > 0 && (strings.Contains(r.Messages[0].Content, "mcp-argument-policy-fixture") || strings.Contains(r.Messages[0].Content, "mcp-quota-fixture") || strings.Contains(r.Messages[0].Content, "mcp-recovery-fixture") || strings.Contains(r.Messages[0].Content, "mcp-call-fixture") || strings.Contains(r.Messages[0].Content, "mcp-oauth-call-fixture")) {
		for _, m := range r.Messages {
			if m.Role == llm.RoleTool {
				return &llm.ChatResponse{Message: llm.ChatMessage{Content: "MCP execution: " + m.Content}, StopReason: "stop"}, nil
			}
		}
		toolName := "mcp.runtime.search"
		callArgs := json.RawMessage(`{"query":"issue"}`)
		if strings.Contains(r.Messages[0].Content, "mcp-argument-policy-fixture-tuples") {
			toolName = "mcp.tuple-policy.search"
			callArgs = json.RawMessage(`{"query":"issue","workspace":"team-a"}`)
		}
		if strings.Contains(r.Messages[0].Content, "mcp-argument-policy-fixture") && !strings.Contains(r.Messages[0].Content, "fixture-tuples") {
			toolName = "mcp.argument-policy.search"
		}
		if strings.Contains(r.Messages[0].Content, "mcp-quota-fixture") {
			toolName = "mcp.quota-browser.search"
		}
		if strings.Contains(r.Messages[0].Content, "mcp-recovery-fixture") {
			toolName = "mcp.recover-browser.search"
		}
		if strings.Contains(r.Messages[0].Content, "mcp-oauth-call-fixture") {
			toolName = "mcp.oauth-browser.search"
		}
		return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "remote-search", Name: toolName, Arguments: callArgs}}}, StopReason: "tool_calls"}, nil
	}
	if len(r.Messages) > 0 && strings.Contains(r.Messages[0].Content, "prompt-journey-fixture") {
		return promptJourneyResponse(r), nil
	}
	if len(r.Messages) > 0 && strings.Contains(r.Messages[0].Content, "git-inspect-fixture") {
		return gitInspectionResponse(r), nil
	}
	for _, m := range r.Messages {
		if strings.Contains(m.Content, "analysis-failure") {
			return nil, fmt.Errorf("E2E analysis provider unavailable")
		}
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Content: `{"environment":{"image":"alpine:3.22","network":{"setup":"none","run":"none"}},"rationale":"E2E minimal shell, no dependency setup required.","tools":["sh"]}`}}, nil
}
func guidanceFixtureCall(skill, reference string) *llm.ChatResponse {
	args, _ := json.Marshal(map[string]string{"skill": skill, "reference": reference})
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{
		{ID: skill + reference, Name: "guidance.load", Arguments: args},
	}}}
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	if len(os.Args) == 2 && os.Args[1] == "preview-app" {
		return previewApp()
	}
	if os.Getenv("WEFT_E2E_REAL") == "1" {
		return runReal()
	}
	root, err := os.MkdirTemp("", "weft-browser-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	bin := filepath.Join(root, "docker-fixture")
	if err := os.WriteFile(bin, []byte(dockerFixture), 0700); err != nil {
		return err
	}
	cfg := config.Default()
	cfg.Instance = "browser-e2e"
	cfg.Preview.Addr = ""
	cfg.LLM = config.LLM{}
	addr, err := browserAddress(os.Getenv("WEFT_E2E_PORT"))
	if err != nil {
		return err
	}
	cfg.Server.Addr = addr
	cfg.Server.PublicURL = "http://" + addr
	// Only this disposable E2E binary permits a synthetic hosted-origin fixture.
	if os.Getenv("WEFT_E2E_CIMD") == "1" {
		target, _ := url.Parse("http://" + addr)
		proxy := httptest.NewUnstartedServer(httputil.NewSingleHostReverseProxy(target))
		// Close the unused listener allocated by NewUnstartedServer before binding
		// the dedicated loopback fixture port. This never touches installation ports.
		if err := proxy.Listener.Close(); err != nil {
			return err
		}
		listener, err := net.Listen("tcp", "127.0.0.1:18091")
		if err != nil {
			return err
		}
		proxy.Listener = listener
		proxy.StartTLS()
		defer proxy.Close()
		cfg.Server.PublicURL = "https://weft.fixture.example:18091"
	}
	cfg.Server.InternalAddr = ""
	cfg.Server.WebDir = "../web/dist"
	cfg.Storage = config.Storage{Type: "sqlite", Dir: filepath.Join(root, "store")}
	cfg.Queue = config.Queue{Type: "memory"}
	cfg.Workspaces.Root = filepath.Join(root, "ws")
	guidanceRoot := filepath.Join(cfg.Workspaces.Root, "repository-guidance-browser")
	for name, content := range map[string]string{
		"AGENTS.md":                              "Browser root instructions.",
		"src/fixture.md":                         "Agent working directory.",
		".agents/skills/browser-review/SKILL.md": "---\nname: browser-review\ndescription: Browser review catalogue.\n---\nBrowser repository body.\n",
		".agents/skills/browser-review/references/checklist.md": "Browser reference body.",
		".agents/skills/browser-guidance/SKILL.md":              "---\nname: browser-guidance\ndescription: Shadowed browser skill.\n---\nShadowed browser body.\n",
	} {
		target := filepath.Join(guidanceRoot, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return err
		}
	}
	cfg.Secrets = config.Secrets{KeyFile: filepath.Join(root, "fixture.key"), GenerateKeyFile: true}
	cfg.Auth.Tokens = []config.Token{{Name: "e2e-admin", Role: config.RoleAdmin, Token: "e2e-admin-token"}, {Name: "e2e-operator", Role: config.RoleOperator, Token: "e2e-operator-token"}, {Name: "e2e-viewer", Role: config.RoleViewer, Token: "e2e-viewer-token"}}
	cfg.Isolation.Docker.Bin = bin
	cfg.Isolation.Docker.Host = ""
	cfg.Isolation.Docker.IdleTimeout = time.Second
	cfg.Isolation.Docker.Capacity = sandboxcapacity.Policy{TotalCPU: "1", DefaultCPU: "0.1", TotalMemory: "256m", DefaultMemory: "32m", MaxActive: 10}
	cfg.Observability.LogLevel = "error"
	cfg.Engine.ReconcileInterval = 100 * time.Millisecond
	if err := cfg.Validate(); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	a, err := app.New(ctx, cfg)
	if err != nil {
		return err
	}
	importFixture, err := importtest.New(ctx, filepath.Join(cfg.Workspaces.Root, ".import-fixture-remote"))
	if err != nil {
		return err
	}
	defer importFixture.Server.Close()
	if err = a.RepositoryImports.Close(ctx); err != nil {
		return err
	}
	imports, err := repositoryimport.New(repositoryimport.Options{Store: a.Store, Root: filepath.Join(cfg.Workspaces.Root, ".repositories"), Client: importFixture.Client, Log: a.Log})
	if err != nil {
		return err
	}
	a.RepositoryImports = imports
	a.API.RepositoryImports = imports
	quotaTime := time.Now()
	a.MCP.QuotaNow = func() time.Time { return quotaTime }
	fixtureRecovery := mcptest.NewRecovering()
	defer fixtureRecovery.Close()
	fixtureFlat := mcptest.NewAtlassianFlat()
	defer fixtureFlat.Close()
	fixtureIssues := mcptest.NewIssues()
	defer fixtureIssues.Close()
	accessFixtures := map[string]*httptest.Server{}
	for _, status := range []int{401, 403, 429} {
		fixture := mcptest.NewAccessFailure(status, "tools/list")
		defer fixture.Close()
		accessFixtures[fmt.Sprintf("https://access-%d.fixture.invalid/mcp", status)] = fixture
	}
	fixtureMCP := mcptest.New()
	defer fixtureMCP.Close()
	fixtureOAuth := mcptest.NewOAuth("")
	defer fixtureOAuth.Close()
	fixtureOAuth.ClientDocument = func(raw string) bool {
		u, err := url.Parse(raw)
		if err != nil || u.Host != "weft.fixture.example:18091" {
			return false
		}
		response, err := http.Get("http://" + addr + u.Path)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		var doc map[string]any
		return response.StatusCode == 200 && json.NewDecoder(response.Body).Decode(&doc) == nil && doc["client_id"] == raw
	}
	githubOAuth := mcptest.NewGitHubOAuth()
	defer githubOAuth.Close()
	atlassianOAuth := mcptest.NewAtlassianOAuth()
	defer atlassianOAuth.Close()
	gitlabOAuth := mcptest.NewGitLabOAuth("https://gitlab.fixture.example")
	defer gitlabOAuth.Close()
	a.MCP.OAuthClient = mcptest.OAuthClient(githubOAuth, atlassianOAuth, gitlabOAuth)
	for _, in := range []mcpconfig.Input{
		{ID: "atlassian-oauth-fixture-config", Name: atlassianOAuth.URL, Service: "atlassian", Endpoint: "https://mcp.atlassian.com/v2/mcp", OAuth: &mcpconfig.OAuthConfig{Issuer: mcptest.AtlassianIssuer, ClientID: "fixture", Scopes: []string{"read:me", "offline_access", "read:jira:agent-interface", "read:confluence:agent-interface"}}},
		{ID: "gitlab-oauth-fixture-config", Name: gitlabOAuth.URL, Service: "gitlab", Endpoint: "https://gitlab.fixture.example/api/v4/mcp", OAuth: &mcpconfig.OAuthConfig{Issuer: "https://gitlab.fixture.example", ClientID: "fixture", Scopes: []string{"mcp"}}},
	} {
		if _, err := a.MCP.Put(ctx, in, 0, "fixture"); err != nil {
			return err
		}
	}
	if _, err := a.MCP.Put(ctx, mcpconfig.Input{ID: "github-oauth-fixture-config", Name: githubOAuth.URL, Service: "github", Endpoint: "https://api.githubcopilot.com/mcp/", Enabled: false, OAuth: &mcpconfig.OAuthConfig{Issuer: "https://github.com/login/oauth", ClientID: "fixture-client", Scopes: []string{"read:user", "read:org"}}}, 0, "fixture"); err != nil {
		return err
	}
	if _, err := a.MCP.Put(ctx, mcpconfig.Input{ID: "oauth-fixture-config", Name: "OAuth fixture config", Service: "generic", Endpoint: fixtureOAuth.URL + "/mcp", Enabled: false, OAuth: &mcpconfig.OAuthConfig{Issuer: fixtureOAuth.URL, ClientID: "fixture-client", Scopes: []string{"read"}}}, 0, "fixture"); err != nil {
		return err
	}
	headerFixtures := map[string]*httptest.Server{
		"https://langsmith.fixture.example/mcp": mcptest.NewWithHeaders(map[string]string{"Langsmith-Api-Key": "fixture-mcp-token", "Langsmith-Workspace-Id": "11111111-1111-1111-1111-111111111111", "Langsmith-Endpoint": "https://eu.api.smith.langchain.com"}),
		"https://dbt.fixture.example/mcp":       mcptest.NewWithHeaders(map[string]string{"Authorization": "Bearer fixture-mcp-token", "X-Dbt-Prod-Environment-Id": "123"}),
	}
	for _, fixture := range headerFixtures {
		defer fixture.Close()
	}
	a.MCP.Discover = func(ctx context.Context, endpoint, token string) ([]mcpconfig.Tool, error) {
		if fixture := accessFixtures[endpoint]; fixture != nil {
			return mcpconfig.DiscoverWithClient(ctx, fixture.URL, token, fixture.Client())
		}
		if endpoint == "https://issues.fixture.invalid/mcp" {
			return mcpconfig.DiscoverWithClient(ctx, fixtureIssues.URL, token, fixtureIssues.Client())
		}

		if fixture := headerFixtures[endpoint]; fixture != nil {
			return mcpconfig.DiscoverWithClient(ctx, fixture.URL, token, fixture.Client())
		}
		if endpoint == "https://mcp.atlassian.com/v2/mcp?tools=all" {
			return mcpconfig.DiscoverWithClient(ctx, fixtureFlat.URL+"?tools=all", token, fixtureFlat.Client())
		}
		if endpoint == "https://recovery.fixture.invalid/mcp" {
			return mcpconfig.DiscoverWithClient(ctx, fixtureRecovery.URL, token, fixtureRecovery.Client())
		}
		if endpoint != "https://mcp.fixture.invalid/mcp" && endpoint != fixtureOAuth.URL+"/mcp" && endpoint != "https://api.githubcopilot.com/mcp/" && endpoint != "https://mcp.atlassian.com/v2/mcp" && endpoint != "https://gitlab.fixture.example/api/v4/mcp" {
			return nil, fmt.Errorf("unknown fixture endpoint")
		}
		return mcpconfig.DiscoverWithClient(ctx, fixtureMCP.URL, token, fixtureMCP.Client())
	}
	a.MCP.Call = func(ctx context.Context, endpoint, token, name string, args json.RawMessage) (json.RawMessage, error) {
		if endpoint == "https://issues.fixture.invalid/mcp" {
			return mcpconfig.CallWithClient(ctx, fixtureIssues.URL, token, name, args, fixtureIssues.Client())
		}

		if fixture := headerFixtures[endpoint]; fixture != nil {
			return mcpconfig.CallWithClient(ctx, fixture.URL, token, name, args, fixture.Client())
		}
		if endpoint == "https://mcp.atlassian.com/v2/mcp?tools=all" {
			return mcpconfig.CallWithClient(ctx, fixtureFlat.URL+"?tools=all", token, name, args, fixtureFlat.Client())
		}
		if endpoint == "https://recovery.fixture.invalid/mcp" {
			return mcpconfig.CallWithClient(ctx, fixtureRecovery.URL, token, name, args, fixtureRecovery.Client())
		}
		if endpoint != "https://mcp.fixture.invalid/mcp" && endpoint != fixtureOAuth.URL+"/mcp" && endpoint != "https://api.githubcopilot.com/mcp/" && endpoint != "https://mcp.atlassian.com/v2/mcp" && endpoint != "https://gitlab.fixture.example/api/v4/mcp" {
			return nil, fmt.Errorf("unknown fixture endpoint")
		}
		return mcpconfig.CallWithClient(ctx, fixtureMCP.URL, token, name, args, fixtureMCP.Client())
	}
	promptModel := os.Getenv("WEFT_E2E_PROMPT_MODEL") == "1"
	fixtureModel := llm.ProviderConfig{Name: "e2e", Type: "mock", DefaultModel: "fixture"}
	if promptModel {
		// Prompt interpretation rejects providers registered as the development mock.
		fixtureModel.Type = "e2e"
	}
	a.Models.Add(fixtureModel, analysisProvider{})
	a.Tools.MustRegister(trackerTools()...)
	// Browser journeys drive triggers through the API; the scheduled loop
	// must not start tasks behind other journeys.
	a.Triggers.Every = time.Hour
	// Opt-in only for the disposable prompt interpretation browser suite.
	if promptModel {
		entries := []llm.Entry{}
		for _, p := range a.Models.List() {
			provider, _ := a.Models.Get(p.Name)
			entries = append(entries, llm.Entry{Config: llm.ProviderConfig{Name: p.Name, Type: p.Type, DefaultModel: p.DefaultModel}, Provider: provider})
		}
		if err := a.Models.ReplaceProviders(entries, fixtureModel.Name); err != nil {
			return err
		}
	}
	for _, id := range []string{"journey-required", "journey-automatic", "discover-a", "discover-b", "proposal-accept", "proposal-budget", "proposal-reject", "analysis-failure", "build-failure", "retry-build", "cpu-holder", "cpu-waiter", "memory-holder", "memory-waiter", "idle-resume", "preparation-holder", "preparation-waiter", "git-inspect", "git-inspect-readonly", "journal-capture", "activity-approve", "activity-reject"} {
		dir := filepath.Join(cfg.Workspaces.Root, id)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if out, err := exec.CommandContext(ctx, "git", "init", "-q", dir).CombinedOutput(); err != nil {
			return fmt.Errorf("init fixture: %w: %s", err, out)
		}
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"`+id+`"}`), 0600); err != nil {
			return err
		}
		// Identical manifests and repository identity allow cross-task reuse.
		if strings.HasPrefix(id, "discover-") {
			if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"same-repository"}`), 0600); err != nil {
				return err
			}
		}
		if strings.HasPrefix(id, "journey-") || strings.HasPrefix(id, "discover-") || id == "build-failure" || id == "retry-build" {
			body := "FROM alpine:3.22\nRUN echo ready\n"
			if id == "retry-build" {
				body = "FROM alpine:3.22\nRUN E2E_FAIL_ONCE\n"
			}
			if id == "build-failure" {
				body = "FROM alpine:3.22\nRUN E2E_BUILD_FAIL\n"
			}
			if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(body), 0600); err != nil {
				return err
			}
		}
		g := &domain.Graph{Metadata: domain.GraphMetadata{Name: id}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker", Loop: domain.LoopSpec{Mode: domain.LoopOnce}, Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}}}}}

		node := &g.Spec.Nodes[0]
		if strings.HasPrefix(id, "journey-") {
			if err := os.WriteFile(filepath.Join(dir, "journey.txt"), []byte("Baseline journey\n"), 0600); err != nil {
				return err
			}
			for _, args := range [][]string{{"add", "journey.txt"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "journey baseline"}} {
				cmd := exec.CommandContext(ctx, "git", args...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					return fmt.Errorf("journey baseline: %w: %s", err, out)
				}
			}
			node.Runtime.Kind = domain.RuntimeNative
			node.Model = domain.ModelRef{Provider: "e2e", Name: "fixture"}
			node.Prompt = "release-one-journey " + id
			node.Tools = []domain.ToolGrant{{Name: "delegation.request"}, {Name: "filesystem.write", Paths: []string{"journey.txt"}, RequiresApproval: true}}
		}
		if strings.HasPrefix(id, "git-inspect") || strings.HasPrefix(id, "activity-") {
			if err := prepareGitInspectionFixture(ctx, dir, node); err != nil {
				return err
			}
			node.Workspace.ReadOnly = id == "git-inspect-readonly"
			node.Approval.BeforeTurn = strings.HasPrefix(id, "activity-")
			if id == "activity-approve" {
				g.Spec.Channels = []domain.Channel{{ID: "tasks", Type: domain.ChannelQueue, To: []string{"worker"}}}
			}
		}
		switch id {
		case "cpu-holder", "memory-holder", "preparation-holder":
			node.Loop = domain.LoopSpec{Mode: domain.LoopContinuous, Interval: domain.Duration(100 * time.Millisecond)}
			if id == "cpu-holder" || id == "preparation-holder" {
				node.Isolation.CPU = "0.8"
			} else {
				node.Isolation.Memory = "224m"
			}
		case "preparation-waiter":
			g.Spec.Environment = &domain.EnvironmentSpec{Image: "alpine:3.22", CPU: "0.4", Network: domain.EnvironmentNetwork{Setup: "none", Run: "none"}, Setup: []string{"echo preparation-completed"}}
		case "cpu-waiter":
			node.Isolation.CPU = "0.4"
		case "memory-waiter":
			node.Isolation.Memory = "64m"
		case "idle-resume":
			node.Loop = domain.LoopSpec{Mode: domain.LoopEvent}
			if err := os.WriteFile(filepath.Join(dir, "preserved.txt"), []byte("workspace survives suspension\n"), 0600); err != nil {
				return err
			}
		}
		if strings.HasPrefix(id, "discover-") {
			g.Spec.Workspace.Repo = "https://example.invalid/same-repository.git"
		}
		if _, _, err := a.Engine.SaveGraph(ctx, g, 0, "e2e-fixture"); err != nil {
			return err
		}
		if id == "proposal-budget" {
			ledger := []byte(`{"scope":"run","id":"proposal-budget","limit":10,"compacted":10,"entries":{}}`)
			if _, err := a.Store.PutSetting(ctx, cost.BudgetPrefix+"run/"+id, ledger, 0, "e2e-fixture"); err != nil {
				return err
			}
		}
	}
	if err := prepareReadinessFixture(ctx, a); err != nil {
		return err
	}
	if err := prepareBudgetRecovery(ctx, a, cfg.Instance); err != nil {
		return err
	}
	closeExternal, err := prepareExternalDelegation(ctx, a, "http://"+addr)
	if err != nil {
		return err
	}
	defer closeExternal()
	return a.Run(ctx)
}

// browserAddress keeps disposable controllers on loopback, including parallel runs.
func browserAddress(port string) (string, error) {
	if port == "" {
		port = "18089"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid WEFT_E2E_PORT")
	}
	return fmt.Sprintf("127.0.0.1:%d", n), nil
}
