// Package app wires configuration to adapters and starts the control plane.
// It is the composition root: the only package that knows which concrete
// storage, queue, provider, runtime and sandbox implementations exist.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/woliveiras/weft/server/internal/api"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/hitl"
	"github.com/woliveiras/weft/server/internal/llm"
	_ "github.com/woliveiras/weft/server/internal/llm/providers"
	"github.com/woliveiras/weft/server/internal/llmconfig"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/brokers"
	"github.com/woliveiras/weft/server/internal/repositoryimport"
	"github.com/woliveiras/weft/server/internal/review"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/runtime/echo"
	"github.com/woliveiras/weft/server/internal/runtime/external"
	"github.com/woliveiras/weft/server/internal/runtime/native"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/stores"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/trigger"
	"github.com/woliveiras/weft/server/internal/workspace"
	"github.com/woliveiras/weft/server/internal/workspacejournal"
)

// App is a running control plane.
type App struct {
	RepositoryImports *repositoryimport.Manager
	Config            *config.Config
	Log               *slog.Logger
	Store             storage.Store
	Broker            queue.Broker
	Bus               *events.Bus
	Approvals         *hitl.Service
	Models            *llm.Registry
	Providers         *llmconfig.Manager
	MCP               *mcpconfig.Manager
	SCM               scm.Manager
	Triggers          *trigger.Runner
	Tools             *tools.Registry
	Gateway           *tools.Gateway
	WorkspaceMonitor  *tools.WorkspaceMonitor
	WorkspaceJournal  *workspacejournal.Importer
	Engine            *engine.Engine
	// Environments builds project environments on the sandbox daemon.
	Environments   *environment.Manager
	Proposals      *environment.Proposals
	API            *api.Server
	Reviews        *review.Manager
	PreviewGateway *review.Gateway

	shutdownObs func(context.Context) error
}

// New builds every component. Log may be nil (observability.Setup decides).
func New(ctx context.Context, cfg *config.Config) (*App, error) {
	a := &App{Config: cfg}
	log, shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName: cfg.Observability.ServiceName, ServiceVersion: api.Version, Instance: cfg.Instance,
		OTLPEndpoint: cfg.Observability.OTLPEndpoint, SampleRatio: cfg.Observability.TraceSampleRatio,
		LogLevel: cfg.Observability.LogLevel, LogFormat: cfg.Observability.LogFormat,
	})
	if err != nil {
		// Telemetry must never keep the platform from starting.
		fmt.Fprintln(os.Stderr, "observability disabled:", err)
		log, shutdown, _ = observability.Setup(ctx, observability.Config{
			ServiceName: cfg.Observability.ServiceName, Instance: cfg.Instance,
			LogLevel: cfg.Observability.LogLevel, LogFormat: cfg.Observability.LogFormat,
		})
	}
	a.Log, a.shutdownObs = log, shutdown
	slog.SetDefault(log)

	a.Store, err = stores.Open(ctx, stores.Config{Type: cfg.Storage.Type, DSN: cfg.Storage.DSN, Dir: cfg.Storage.Dir})
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	a.Broker, err = openBroker(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("queue: %w", err)
	}
	a.Bus = events.NewBus(a.Store, log)
	a.Approvals = hitl.New(a.Store, a.Bus, log)

	static, staticDefault := cfg.LLM.Static()
	a.Models, err = llm.NewRegistry(static, staticDefault)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	box, keySource, err := loadSecrets(cfg.Secrets)
	if err != nil {
		return nil, fmt.Errorf("secrets: %w", err)
	}
	switch keySource {
	case secrets.SourceNone:
		log.Warn("no installation encryption key: credentials cannot be stored from the console (set WEFT_SECRET_KEY or secrets.keyFile)")
	case secrets.SourceGenerated:
		log.Warn("generated a new installation encryption key; back it up - stored credentials cannot be decrypted without it", "file", cfg.Secrets.KeyFile, "key_id", box.PrimaryKeyID())
	default:
		log.Info("installation encryption key loaded", "source", keySource, "key_id", box.PrimaryKeyID())
	}
	a.Providers, err = llmconfig.New(llmconfig.Options{
		Store: a.Store, Box: box, KeySource: keySource, Registry: a.Models,
		Static: static, StaticDefault: staticDefault, Bus: a.Bus, Log: log,
		InUse: a.providerUsers,
	})
	if err != nil {
		return nil, err
	}
	if err := a.Providers.Load(ctx); err != nil {
		return nil, fmt.Errorf("llm providers: %w", err)
	}
	a.Models.SetObserver(cost.Recorder{Prices: cost.Prices{Store: a.Store}, Bus: a.Bus, Log: log}.Observe)
	// Reservations left open by executions that ended (a restart between
	// dispatch and settlement) are charged in full, never refunded.
	if err := (cost.Budgets{Store: a.Store, Bus: a.Bus}).Reconcile(ctx, func(ctx context.Context, id string) bool {
		x, err := a.Store.GetExecution(ctx, id)
		return errors.Is(err, storage.ErrNotFound) || (err == nil && x.CompletedAt != nil)
	}); err != nil {
		log.Error("monetary budget reconciliation failed; open reservations stay held", "err", err)
	}
	a.MCP = mcpconfig.New(a.Store, box, a.Bus)
	a.SCM = scm.Manager{Store: a.Store, Box: box, Bus: a.Bus}
	a.MCP.AllowLocalProcesses = cfg.NativeProfile
	a.Tools = tools.Builtin(tools.BuiltinConfig{AllowPrivateNetworks: cfg.Tools.AllowPrivateNetworks})
	a.Tools.MustRegister(scm.Tools(a.SCM)...)
	a.Gateway = tools.NewGateway(a.Tools, a.Approvals, a.Bus)
	a.Gateway.TaskAuthorization = (taskauth.Manager{Store: a.Store}).Check
	a.Gateway.ResolveTool = a.MCP.Resolve
	a.Triggers = &trigger.Runner{Triggers: trigger.Manager{Store: a.Store, Bus: a.Bus}, Store: a.Store, Instance: cfg.Instance,
		Gateway: a.Gateway, BaseURL: cfg.Server.PublicURL, Log: log.With("component", "trigger")}

	ws, err := workspace.NewManager(cfg.Workspaces.Root, cfg.Workspaces.HostRoot, cfg.Workspaces.AllowAbsolute)
	if err != nil {
		return nil, err
	}

	a.WorkspaceMonitor = &tools.WorkspaceMonitor{Auditor: a.Bus, Targets: func(ctx context.Context) ([]tools.WorkspaceTarget, error) { return workspaceTargets(ctx, a.Store, ws) }}
	var journalMetrics http.Handler
	if path := os.Getenv("WEFT_WORKSPACE_JOURNAL"); path != "" {
		journalMetrics, err = workspacejournal.MetricsHandler(path)
		if err != nil {
			return nil, fmt.Errorf("workspace journal metrics: %w", err)
		}
		a.WorkspaceJournal = &workspacejournal.Importer{Store: a.Store, Path: path, Scopes: func(ctx context.Context) ([]workspacejournal.Scope, error) {
			targets, err := workspaceTargets(ctx, a.Store, ws)
			if err != nil {
				return nil, err
			}
			root, err := filepath.EvalSymlinks(ws.Root)
			if err != nil {
				return nil, err
			}
			scopes := []workspacejournal.Scope{}
			for _, t := range targets {
				rel, err := filepath.Rel(root, t.Dir)
				if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					continue
				}
				scopes = append(scopes, workspacejournal.Scope{GraphID: t.GraphID, NodeID: t.NodeID, Replica: t.Replica, RelativeRoot: filepath.ToSlash(rel)})
			}
			return scopes, nil
		}}
	}

	secret := ""
	if cfg.Runtimes.SharedSecretEnv != "" {
		secret = os.Getenv(cfg.Runtimes.SharedSecretEnv)
	}
	runtimes := map[domain.RuntimeKind]runtime.Runtime{
		domain.RuntimeNative: native.New(),
		domain.RuntimeEcho:   echo.New(),
		domain.RuntimeExternal: external.New(external.Options{
			SharedSecret: secret, DockerBin: cfg.Isolation.Docker.Bin,
			DockerNetwork: os.Getenv("WEFT_RUNTIME_NETWORK"), CallbackURL: cfg.Server.InternalURL,
		}),
	}

	a.Environments, err = environment.New(environment.Options{
		Store: a.Store, Bus: a.Bus, Workspaces: ws, Instance: cfg.Instance, Log: log,
		DockerBin: cfg.Isolation.Docker.Bin, DockerHost: cfg.Isolation.Docker.Host,
		EgressImage: cfg.Isolation.Docker.EgressImage, Capacity: capacityManager(cfg, a.Store), Policy: cfg.Isolation.Docker.EnvironmentPolicy,
	})
	if err != nil {
		return nil, err
	}
	a.Proposals = &environment.Proposals{Store: a.Store, Approvals: a.Approvals, Models: a.Models, Manager: a.Environments, Directory: ws.GraphDir}
	a.Environments.SetProposalValidator(a.Proposals.ValidateAccepted)
	if cfg.Preview.Addr != "" {
		factory := sandboxFactory(cfg, a.Environments, a.Store, a.Bus)
		driver := &review.DockerDriver{Bin: cfg.Isolation.Docker.Bin, Host: cfg.Isolation.Docker.Host, RelayBinary: cfg.Preview.RelayBinary, Capacity: capacityManager(cfg, a.Store)}
		driver.StartSandbox = func(ctx context.Context, r *review.Record, work, control string) (tools.Sandbox, error) {
			if r.Graph.Spec.Environment != nil && engine.UsesEnvironment(r.Graph, r.Graph.Node(r.NodeID)) {
				env, e := a.Environments.SandboxFor(ctx, r.Graph)
				if e != nil {
					return nil, e
				}
				var caches []string
				for _, v := range env.Volumes {
					parts := strings.SplitN(v, ":", 2)
					if len(parts) != 2 {
						return nil, fmt.Errorf("invalid cache mount")
					}
					caches = append(caches, parts[1])
				}
				body, e := json.Marshal(map[string]any{"command": r.Request.Command, "cwd": filepath.ToSlash(filepath.Join("/workspace", r.Request.Cwd)), "caches": caches})
				if e != nil {
					return nil, e
				}
				if e = review.WriteControl(control, body); e != nil {
					return nil, e
				}
			}
			return factory(ctx, engine.SandboxSpec{Graph: r.Graph, Node: r.Graph.Node(r.NodeID), Replica: r.Replica, Workdir: work, HostWorkdir: ws.HostPath(work), Name: r.Name(), Command: []string{"/weft-preview/weft-preview", "serve", "/weft-preview/config.json"}, ExtraVolumes: []string{ws.HostPath(control) + ":/weft-preview:ro"}, ReadOnlyCaches: true})
		}
		a.Reviews = review.New(review.Options{Store: a.Store, Bus: a.Bus, Root: filepath.Join(ws.Root, ".reviews"), Instance: cfg.Instance, Driver: driver})
		a.PreviewGateway = &review.Gateway{Manager: a.Reviews, BaseURL: cfg.Preview.BaseURL, ConsoleOrigins: cfg.Preview.ConsoleOrigins}
		if err := a.Tools.Register(a.Reviews.Tool()); err != nil {
			return nil, err
		}
	}
	for _, tool := range a.Proposals.AgentTools() {
		if err := a.Tools.Register(tool); err != nil {
			return nil, err
		}
	}
	a.Engine, err = engine.New(engine.Options{
		ValidateToolGrants: a.MCP.ValidateGrants,
		Reviews:            a.Reviews,
		Instance:           cfg.Instance, Store: a.Store, Broker: a.Broker, Bus: a.Bus, Approvals: a.Approvals,
		Models: a.Models, Tools: toolGateway{a.Tools, a.Gateway, a.MCP}, Runtimes: runtimes, Workspaces: ws,
		Sandboxes: sandboxFactory(cfg, a.Environments, a.Store, a.Bus), Environments: a.Environments, EnvironmentProposals: a.Proposals,
		CallbackURL: cfg.Server.InternalURL, Log: log,
		LeaseTTL: cfg.Engine.LeaseTTL, ReconcileInterval: cfg.Engine.ReconcileInterval,
		SandboxIdleTimeout: cfg.Isolation.Docker.IdleTimeout,
		RetryBackoff:       cfg.Engine.RetryBackoff, MaxConcurrentTurns: cfg.Engine.MaxConcurrentTurns,
	})
	if err != nil {
		return nil, err
	}
	// Agent checkpoint tools need the engine; graphs only run after New returns.
	for _, tool := range a.Engine.CheckpointTools() {
		if err := a.Tools.Register(tool); err != nil {
			return nil, err
		}
	}
	a.Proposals.Save = func(ctx context.Context, g *domain.Graph, version int, actor string) error {
		_, _, err := a.Engine.SaveGraph(ctx, g, version, actor)
		return err
	}
	a.Proposals.Start = func(ctx context.Context, id, actor string) error {
		return a.Engine.GraphAction(ctx, id, "start", actor)
	}
	auth, err := api.NewAuthenticator(cfg.Auth)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	if cfg.Auth.Disabled {
		log.Warn("API authentication is DISABLED; never expose this instance beyond localhost")
	}
	a.RepositoryImports, err = repositoryimport.New(repositoryimport.Options{Native: cfg.NativeProfile, Store: a.Store, Root: filepath.Join(ws.Root, ".repositories"), Log: log})
	if err != nil {
		return nil, err
	}
	a.API = api.New(api.Deps{
		RepositoryImports: a.RepositoryImports,
		Reviews:           a.Reviews, PreviewGateway: a.PreviewGateway, MCP: a.MCP, SCM: a.SCM, Gateway: a.Gateway,
		Config: cfg, Engine: a.Engine, Store: a.Store, Broker: a.Broker, Bus: a.Bus, Approvals: a.Approvals,
		Models: a.Models, Providers: a.Providers, Tools: a.Tools, Workspaces: ws, Auth: auth, Log: log,
		Environments: a.Environments, Proposals: a.Proposals, Metrics: journalMetrics,
	})
	a.Triggers.Tasks = triggerTasks{a: a, runner: a.API.TaskRunner}
	return a, nil
}

func loadSecrets(c config.Secrets) (*secrets.Box, secrets.KeySource, error) {
	opts := secrets.Options{KeyFile: c.KeyFile, Generate: c.GenerateKeyFile}
	if c.KeyEnv != "" {
		opts.Key = os.Getenv(c.KeyEnv)
	}
	if c.PreviousKeysEnv != "" {
		opts.Previous = strings.Split(os.Getenv(c.PreviousKeysEnv), ",")
	}
	return secrets.Load(opts)
}

// providerUsers lists graph nodes that select a provider by name, so it is
// not deleted from under them.
func (a *App) providerUsers(ctx context.Context, provider string) ([]string, error) {
	gs, err := a.Store.ListGraphs(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, g := range gs {
		if g.Spec.Defaults.Model.Provider == provider {
			out = append(out, fmt.Sprintf("graph %q (defaults)", g.Metadata.ID))
		}
		for _, n := range g.Spec.Nodes {
			if n.Model.Provider == provider {
				out = append(out, fmt.Sprintf("graph %q node %q", g.Metadata.ID, n.ID))
			}
		}
	}
	return out, nil
}

// refreshProviders applies provider changes made through other instances.
func (a *App) refreshProviders(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := a.Providers.Refresh(ctx); err != nil && ctx.Err() == nil {
				a.Log.Warn("refresh model providers", "err", err)
			}
		}
	}
}

type toolGateway struct {
	reg *tools.Registry
	gw  *tools.Gateway
	mcp *mcpconfig.Manager
}

func (t toolGateway) Invoke(ctx context.Context, p *tools.Principal, name string, args json.RawMessage) (*tools.Result, error) {
	return t.gw.Invoke(ctx, p, name, args)
}

func (t toolGateway) Schemas(p *tools.Principal) []llm.ToolSchema {
	out := t.reg.Schemas(p)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	available, err := t.mcp.Available(ctx)
	if err != nil {
		slog.Error("MCP catalogue unavailable", "error", err)
		return out
	}
	for _, tool := range available {
		if len(tools.MatchingGrants(p, tool)) > 0 {
			out = append(out, llm.ToolSchema{Name: tool.Name(), Description: tool.Description(), Parameters: tool.Schema()})
		}
	}
	return out
}

func (t toolGateway) NeedsApproval(p *tools.Principal, name string) bool {
	tool, ok := t.reg.Get(name)
	if !ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var err error
		tool, err = t.mcp.Resolve(ctx, name)
		ok = err == nil && tool != nil
	}
	if !ok {
		return false
	}
	g, err := tools.Authorize(p, tool)
	return err == nil && (g.RequiresApproval || name == "environment.propose")
}

func openBroker(ctx context.Context, cfg *config.Config) (queue.Broker, error) {
	var last error
	// Brokers in a compose stack may still be starting; retry briefly.
	for i := 0; i < 30; i++ {
		b, err := brokers.Open(ctx, brokers.Config{Type: cfg.Queue.Type, URL: cfg.Queue.URL, Dir: cfg.Queue.Dir, Options: cfg.Queue.Options})
		if err == nil {
			return b, nil
		}
		last = err
		if cfg.Queue.Type == "" || cfg.Queue.Type == "fs" || cfg.Queue.Type == "memory" {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, last
}

func capacityManager(cfg *config.Config, store storage.Store) *sandboxcapacity.Manager {
	host := cfg.Isolation.Docker.Host
	if host == "" {
		host = os.Getenv("DOCKER_HOST")
	}
	scope := fmt.Sprintf("%x", sha256.Sum256([]byte(host)))
	return &sandboxcapacity.Manager{Store: store, Policy: cfg.Isolation.Docker.Capacity, Scope: scope, Notify: func(name, state string) {
		slog.Info("sandbox capacity", "sandbox", name, "state", state)
	}}
}

// sandboxFactory picks the command sandbox per node isolation mode.
//
// Docker-isolated nodes without an image of their own use the graph's
// environment when it defines one (its image, caches and run network);
// otherwise the installation's default sandbox image.
func sandboxFactory(cfg *config.Config, envs *environment.Manager, store storage.Store, bus *events.Bus) engine.SandboxFactory {
	capacity := capacityManager(cfg, store)

	return func(ctx context.Context, s engine.SandboxSpec) (tools.Sandbox, error) {
		switch s.Node.Isolation.Mode {
		case domain.IsolationDocker:
			img := s.Node.Isolation.Image
			net := s.Node.Isolation.Network
			var volumes []string
			var companions []domain.Companion
			var allowedHosts []string
			if engine.UsesEnvironment(s.Graph, s.Node) {
				env, err := envs.SandboxFor(ctx, s.Graph)
				if err != nil {
					return nil, err
				}
				img, volumes = env.Image, env.Volumes
				companions = env.Companions
				allowedHosts = env.AllowedHosts
				if net == "" {
					net = env.Network
				}
			}
			if img == "" {
				img = cfg.Isolation.Docker.Image
			}
			if net == "" {
				net = cfg.Isolation.Docker.Network
			}
			policy, err := (environmentpolicy.Settings{Store: store, Base: cfg.Isolation.Docker.EnvironmentPolicy}).Effective(ctx)
			if err != nil {
				return nil, err
			}
			if err := policy.RunNetwork(net); err != nil {
				return nil, err
			}
			budget := *capacity
			budget.Notify = func(name, state string) {
				slog.Info("sandbox capacity", "graph", s.Graph.Metadata.ID, "node", s.Node.ID, "sandbox", name, "state", state)
				bus.Publish(context.Background(), domain.NewEvent("sandbox.capacity", s.Graph.Metadata.ID, map[string]any{"node": s.Node.ID, "replica": s.Replica, "state": state}))
			}
			sandboxConfig := tools.DockerSandboxConfig{
				BeforeRemove: func(ctx context.Context) error {
					if net != "egress" || envs == nil {
						return nil
					}
					return envs.CollectBlockedHistory(ctx, s.Graph.Metadata.ID)
				},
				ValidateAdmission: func(admissionCtx context.Context) error {
					current, err := (environmentpolicy.Settings{Store: store, Base: cfg.Isolation.Docker.EnvironmentPolicy}).Effective(admissionCtx)
					if err != nil {
						return err
					}
					if engine.UsesEnvironment(s.Graph, s.Node) {
						if err := current.Check(s.Graph.Spec.Environment); err != nil {
							return err
						}
					}
					return current.RunNetwork(net)
				},
				Capacity: &budget,
				Name:     fmt.Sprintf("weft-sbx-%s-%s-%d", s.Graph.Metadata.ID, s.Node.ID, s.Replica),
				Image:    img, HostDir: s.HostWorkdir, Network: net,
				WorkspaceReadOnly: s.Node.Workspace.ReadOnly,
				CPU:               s.Node.Isolation.CPU, Memory: s.Node.Isolation.Memory,
				DockerBin: cfg.Isolation.Docker.Bin, DockerHost: cfg.Isolation.Docker.Host,
				Labels:  map[string]string{"weft.graph": s.Graph.Metadata.ID, "weft.node": s.Node.ID},
				Volumes: volumes,
			}
			if s.Name != "" {
				sandboxConfig.Name = s.Name
			}
			sandboxConfig.Command = s.Command
			if s.ReadOnlyCaches {
				for i, v := range sandboxConfig.Volumes {
					parts := strings.SplitN(v, ":", 2)
					if len(parts) != 2 {
						return nil, fmt.Errorf("invalid managed cache mount")
					}
					sandboxConfig.Volumes[i] = parts[0] + ":/weft-preview-cache" + parts[1] + ":ro"
				}
			}
			sandboxConfig.Volumes = append(sandboxConfig.Volumes, s.ExtraVolumes...)
			if net == "egress" {
				if len(allowedHosts) == 0 {
					return nil, fmt.Errorf("egress requires an accepted environment with allowedHosts")
				}
				sandboxConfig.Egress = &tools.EgressConfig{Image: cfg.Isolation.Docker.EgressImage, Hosts: allowedHosts}
				sandboxConfig.Network = "none"
			}
			if len(allowedHosts) > 0 && net != "egress" && net != "none" {
				return nil, fmt.Errorf("an egress environment cannot override its network with unrestricted bridge")
			}
			if len(companions) > 0 || sandboxConfig.Egress != nil {
				sb, err := tools.NewComposeSandbox(sandboxConfig, companions)
				if err != nil {
					return nil, err
				}
				if err := sb.Start(ctx); err != nil {
					return nil, err
				}
				return sb, nil
			}
			sandboxConfig.RequireStandalone = true
			sb, err := tools.NewDockerSandbox(sandboxConfig)
			if err != nil {
				return nil, err
			}
			if err := sb.Start(ctx); err != nil {
				return nil, err // Start reconciles failures without deleting a resumable container.
			}
			return sb, nil
		case domain.IsolationKubernetes:
			// In Kubernetes the platform pod runs tools in its own (restricted)
			// container; per-replica tool pods are an extension point.
			return &tools.ProcessSandbox{Root: s.Workdir}, nil
		default:
			for _, g := range s.Node.Tools {
				if g.Name == "*" || strings.HasPrefix(g.Name, "shell") {
					slog.Warn("node holds shell.exec under process isolation: commands run as the platform user and can read its environment and files; use isolation.mode docker (or kubernetes) for untrusted work",
						"graph", s.Graph.Metadata.ID, "node", s.Node.ID)
					break
				}
			}
			return &tools.ProcessSandbox{Root: s.Workdir}, nil
		}
	}
}

// Bootstrap loads graph documents from files/directories and starts the
// listed graphs - GitOps-style provisioning for examples and production.
func (a *App) Bootstrap(ctx context.Context) error {
	for _, p := range a.Config.Bootstrap.Graphs {
		files := []string{p}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			files = nil
			for _, pat := range []string{"*.yaml", "*.yml", "*.json"} {
				m, _ := filepath.Glob(filepath.Join(p, pat))
				files = append(files, m...)
			}
		}
		for _, f := range files {
			if err := a.loadGraph(ctx, f); err != nil {
				a.Log.Error("bootstrap graph", "file", f, "err", err)
			}
		}
	}
	for _, id := range a.Config.Bootstrap.Start {
		if err := a.Engine.GraphAction(ctx, id, "start", "bootstrap"); err != nil {
			a.Log.Error("bootstrap start", "graph", id, "err", err)
		}
	}
	return nil
}

func (a *App) loadGraph(ctx context.Context, file string) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	parsed, unknown, err := graph.Decode(b, true)
	if err != nil {
		return err
	}
	if len(unknown) > 0 {
		return &engine.ValidationError{Report: &graph.Report{Errors: unknown}}
	}
	// Compare the effective (normalised) documents, but save the file as
	// written so its defaults keep applying to nodes that do not override them.
	var effective domain.Graph
	if b, err := json.Marshal(parsed); err == nil {
		_ = json.Unmarshal(b, &effective)
	}
	a.Engine.Validate(&effective)
	if cur, _, err := a.Engine.GetGraph(ctx, effective.Metadata.ID, 0); err == nil {
		// Only save when the document changed, so restarts don't pile up versions.
		effective.Metadata.Version = cur.Metadata.Version
		effective.Metadata.CreatedAt, effective.Metadata.UpdatedAt = cur.Metadata.CreatedAt, cur.Metadata.UpdatedAt
		x, _ := yaml.Marshal(cur)
		y, _ := yaml.Marshal(&effective)
		if string(x) == string(y) {
			return nil
		}
	}
	saved, _, err := a.Engine.SaveGraph(ctx, parsed, 0, "bootstrap:"+filepath.Base(file))
	if err == nil {
		a.Log.Info("graph loaded", "graph", saved.Metadata.ID, "file", file)
	}
	return err
}

// Run serves until ctx is cancelled, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	// Reserve every listener before admitting any agents. An occupied console,
	// callback or preview port must not start background work without a UI.
	publicListener, err := net.Listen("tcp", a.Config.Server.Addr)
	if err != nil {
		return fmt.Errorf("console listener: %w", err)
	}
	defer publicListener.Close()
	var previewListener, internalListener net.Listener
	if a.PreviewGateway != nil {
		previewListener, err = net.Listen("tcp", a.Config.Preview.Addr)
		if err != nil {
			return fmt.Errorf("preview listener: %w", err)
		}
		defer previewListener.Close()
	}
	if a.API.InternalHandler() != nil {
		internalListener, err = net.Listen("tcp", a.Config.Server.InternalAddr)
		if err != nil {
			return fmt.Errorf("callback listener: %w", err)
		}
		defer internalListener.Close()
	}
	if err := a.Engine.Start(ctx); err != nil {
		return err
	}
	if err := a.Bootstrap(ctx); err != nil {
		return err
	}
	monitorCtx, stopMonitor := context.WithCancel(ctx)
	monitorDone := make(chan struct{})
	go func() { defer close(monitorDone); a.WorkspaceMonitor.Run(monitorCtx, a.Log) }()
	journalDone := make(chan struct{})
	go func() {
		defer close(journalDone)
		if a.WorkspaceJournal == nil {
			return
		}
		for {
			if err := a.WorkspaceJournal.Import(monitorCtx); err != nil && monitorCtx.Err() == nil {
				a.Log.Warn("workspace journal import failed", "error", err)
			}
			select {
			case <-monitorCtx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
	defer func() { stopMonitor(); <-journalDone }()
	defer func() { stopMonitor(); <-monitorDone }()
	historyCtx, stopHistory := context.WithCancel(ctx)
	historyDone := make(chan struct{})
	go func() { defer close(historyDone); a.Environments.RunProxyHistory(historyCtx) }()
	defer func() { stopHistory(); <-historyDone }()
	go a.refreshProviders(ctx)
	go a.Triggers.Run(ctx)
	go a.Proposals.Run(ctx)
	reviewCtx, stopReviews := context.WithCancel(ctx)
	reviewDone := make(chan struct{})
	go func() {
		defer close(reviewDone)
		if a.Reviews != nil {
			a.Reviews.Run(reviewCtx)
		}
	}()
	defer func() { stopReviews(); <-reviewDone }()
	// Keep callbacks alive while turns drain, then cancel long-lived HTTP streams.
	httpCtx, stopHTTP := context.WithCancel(context.Background())
	defer stopHTTP()
	baseContext := func(net.Listener) context.Context { return httpCtx }
	srv := &http.Server{
		BaseContext:       baseContext,
		Addr:              a.Config.Server.Addr,
		Handler:           a.API.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 3)
	var previewServer *http.Server
	if a.PreviewGateway != nil {
		previewServer = &http.Server{BaseContext: baseContext, Addr: a.Config.Preview.Addr, Handler: a.PreviewGateway, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
		go func() {
			if a.Config.Preview.TLSCert != "" {
				errc <- previewServer.ServeTLS(previewListener, a.Config.Preview.TLSCert, a.Config.Preview.TLSKey)
			} else {
				errc <- previewServer.Serve(previewListener)
			}
		}()
	}
	var internal *http.Server
	if h := a.API.InternalHandler(); h != nil {
		internal = &http.Server{BaseContext: baseContext, Addr: a.Config.Server.InternalAddr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			a.Log.Info("callback API listening", "addr", a.Config.Server.InternalAddr)
			errc <- internal.Serve(internalListener)
		}()
	}
	go func() {
		if a.Config.NativeProfile {
			a.Log.Info("Weft is ready. Open " + a.Config.Server.PublicURL)
			a.Log.Info("Press Ctrl+C to stop. Your tasks and workspaces are retained.")
		}
		if !a.Config.NativeProfile {
			a.Log.Info("listening", "addr", a.Config.Server.Addr, "queue", a.Broker.Name(), "storage", a.Store.Name(),
				"providers", strings.Join(a.Models.Names(), ","), "default_provider", a.Models.DefaultName())
		}
		errc <- srv.Serve(publicListener)
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	started := time.Now()
	a.Log.Info("Stopping Weft; waiting for active agents", "timeout", "60s")
	sctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// Drain turns first, while the listeners are still up: an external agent
	// finishing its turn still needs the callback API.
	a.Engine.Shutdown(sctx)
	a.Log.Info("Agents stopped; closing browser streams and HTTP listeners", "elapsed", time.Since(started).Round(time.Millisecond))
	stopHTTP()
	httpErr := errors.Join(shutdownHTTP(sctx, srv), shutdownHTTP(sctx, previewServer), shutdownHTTP(sctx, internal))
	stopHistory()
	<-historyDone
	stopReviews()
	<-reviewDone
	a.Log.Info("Closing storage, queues and telemetry", "elapsed", time.Since(started).Round(time.Millisecond))
	err = errors.Join(httpErr, a.Close(sctx))
	if err == nil {
		a.Log.Info("Weft stopped; persistent data is retained", "elapsed", time.Since(started).Round(time.Millisecond))
	}
	return err
}

func (a *App) Close(ctx context.Context) error {
	var errs []error
	if a.RepositoryImports != nil {
		if err := a.RepositoryImports.Close(ctx); err != nil {
			return err
		}
	}
	if a.Broker != nil {
		errs = append(errs, a.Broker.Close())
	}
	if a.Store != nil {
		errs = append(errs, a.Store.Close())
	}
	if a.shutdownObs != nil {
		errs = append(errs, a.shutdownObs(ctx))
	}
	return errors.Join(errs...)
}

// shutdownHTTP closes leftover connections on timeout rather than leaving handlers
// alive against closed storage. Preserve the timeout for the caller's diagnostics.
func shutdownHTTP(ctx context.Context, server *http.Server) error {
	if server == nil {
		return nil
	}
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP shutdown: %w", errors.Join(err, server.Close()))
	}
	return nil
}
