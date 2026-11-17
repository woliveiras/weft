package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

// State of a graph's environment.
type State string

const (
	StateAbsent   State = "absent"   // never built, or reset
	StateBuilding State = "building" // a build is running
	StateReady    State = "ready"    // Image can be used
	StateFailed   State = "failed"   // the last build failed; Error says why
)

var (
	// ErrNotDefined: the graph has no spec.environment.
	ErrNotDefined = errors.New("the graph defines no environment (spec.environment)")
	// ErrBuildRunning: a build of this graph's environment is in progress.
	ErrBuildRunning = errors.New("an environment build is already running")
	// ErrNotReady: agents cannot start because the environment is not built.
	ErrNotReady = errors.New("environment not ready")
)

const (
	kvNamespace   = "environment"
	leasePrefix   = "environment/"
	leaseTTL      = 90 * time.Second
	defaultLimit  = 64 << 10 // build log tail kept in the store
	flushInterval = 2 * time.Second
)

// CacheMount is a cache directory and the volume that backs it.
type CacheMount struct {
	Path   string `json:"path"`
	Volume string `json:"volume"`
}

// Status is the persisted state of a graph's environment.
type Status struct {
	Phase         string       `json:"phase,omitempty"`
	ReuseDecision string       `json:"reuseDecision,omitempty"`
	BuildReasons  []string     `json:"buildReasons,omitempty"`
	SharedImage   string       `json:"sharedImage,omitempty"`
	Scope         string       `json:"scope,omitempty"`
	ReuseKey      string       `json:"reuseKey,omitempty"`
	Reused        bool         `json:"reused,omitempty"`
	Inputs        string       `json:"inputs,omitempty"`
	Graph         string       `json:"graph"`
	State         State        `json:"state"`
	Hash          string       `json:"hash,omitempty"`  // definition hash of the last build
	Image         string       `json:"image,omitempty"` // set when ready
	Caches        []CacheMount `json:"caches,omitempty"`
	StartedAt     *time.Time   `json:"startedAt,omitempty"`
	FinishedAt    *time.Time   `json:"finishedAt,omitempty"`
	Error         string       `json:"error,omitempty"`
	Log           string       `json:"log,omitempty"` // tail of the build output
	Actor         string       `json:"actor,omitempty"`
	Instance      string       `json:"instance,omitempty"`
}

// View is a Status compared with the graph's current definition.
type View struct {
	ChangeReasons []string `json:"changeReasons,omitempty"`
	Status
	// Defined is false when the graph has no spec.environment.
	Defined bool `json:"defined"`
	// CurrentHash is the hash of the saved definition.
	CurrentHash string `json:"currentHash,omitempty"`
	// UpToDate: ready and built from the current definition.
	UpToDate bool `json:"upToDate"`
}

// Options wire a Manager.
type Options struct {
	EgressImage       string
	Policy            environmentpolicy.Policy
	Capacity          *sandboxcapacity.Manager
	ProposalValidator func(context.Context, *domain.Graph) error
	Store             storage.Store
	Bus               *events.Bus // optional
	Workspaces        Workspaces
	DockerBin         string
	DockerHost        string
	Instance          string
	Log               *slog.Logger
	// LogLimit is the build log tail kept per environment (default 64 KiB).
	LogLimit int
	// DefaultTimeout bounds builds without spec.environment.timeout (30m).
	DefaultTimeout time.Duration
}

// Workspaces is what the manager needs from workspace.Manager.
type Workspaces interface {
	Ensure(ctx context.Context, g *domain.Graph) (string, error)
	HostPath(p string) string
}

// Manager builds environments and answers which image a sandbox uses.
type Manager struct {
	opt Options

	mu      sync.Mutex
	running map[string]*build // builds owned by this instance, by graph
}

type build struct {
	cancel context.CancelFunc
	done   chan struct{}
	force  bool
}

func New(opt Options) (*Manager, error) {
	if err := opt.Policy.Validate(); err != nil {
		return nil, err
	}
	if opt.Store == nil || opt.Workspaces == nil {
		return nil, errors.New("environment: store and workspaces are required")
	}
	if opt.EgressImage == "" {
		opt.EgressImage = "weft/egress:latest"
	}
	if opt.DockerBin == "" {
		opt.DockerBin = "docker"
	}
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	if opt.LogLimit <= 0 {
		opt.LogLimit = defaultLimit
	}
	if opt.DefaultTimeout <= 0 {
		opt.DefaultTimeout = 30 * time.Minute
	}
	if opt.Instance == "" {
		opt.Instance = "weftd"
	}
	return &Manager{opt: opt, running: map[string]*build{}}, nil
}

// Get reports the environment of g. A build whose owner stopped renewing its
// lease (the instance crashed or restarted) is reported, and stored, as
// failed.
func (m *Manager) Get(ctx context.Context, g *domain.Graph) (*View, error) {
	st, err := m.load(ctx, g.Metadata.ID)
	if err != nil {
		return nil, err
	}
	if st.State == StateBuilding && !m.leaseAlive(ctx, g.Metadata.ID) {
		now := time.Now().UTC()
		st.Phase = ""
		st.State, st.Error, st.FinishedAt = StateFailed, "build interrupted: the Weft instance running it stopped; build again", &now
		if err := m.save(ctx, st); err != nil {
			return nil, err
		}
	}
	v := m.view(g, st)
	if v.UpToDate && m.ValidatePolicy(ctx, g.Spec.Environment) != nil {
		v.UpToDate = false
		v.ChangeReasons = append(v.ChangeReasons, "installation_policy")
	}
	if v.UpToDate && g.Spec.Workspace.Repo != "" {
		dir, err := m.opt.Workspaces.Ensure(ctx, g)
		if err != nil {
			return nil, err
		}
		e, err := readManifests(ctx, dir)
		if err != nil {
			return nil, err
		}
		if st.Inputs != e.SHA256 {
			v.ChangeReasons = append(v.ChangeReasons, "repository_inputs_changed")
		}
		if st.Scope != reuseScope(g.Spec.Workspace.Repo, m.opt.DockerHost) || g.Spec.Workspace.Path != "" {
			v.ChangeReasons = append(v.ChangeReasons, "repository_scope_changed")
		}
		v.UpToDate = st.Inputs == e.SHA256 && st.Scope == reuseScope(g.Spec.Workspace.Repo, m.opt.DockerHost) && g.Spec.Workspace.Path == ""
	}
	return v, nil
}

func (m *Manager) view(g *domain.Graph, st *Status) *View {
	v := &View{Status: *st, Defined: g.Spec.Environment != nil}
	if v.Defined {
		v.CurrentHash = Hash(g.Spec.Environment)
		if st.Hash != "" && st.Hash != v.CurrentHash {
			v.ChangeReasons = append(v.ChangeReasons, "definition_changed")
		}
		v.UpToDate = st.State == StateReady && st.Hash == v.CurrentHash
		if st.ReuseKey != "" && g.Spec.Workspace.Repo == "" {
			v.UpToDate = false
			v.ChangeReasons = append(v.ChangeReasons, "repository_scope_changed")
		}
	}
	return v
}

// Build starts building g's environment in the background and returns the
// building status. Without force, an up-to-date environment is left alone.
func (m *Manager) Build(ctx context.Context, g *domain.Graph, force bool, actor string) (*View, error) {
	env := g.Spec.Environment
	if env == nil {
		return nil, ErrNotDefined
	}
	// Validate before returning a cached image or starting any Docker operation.
	dir := ""
	if env.Origin != nil || env.Context != "" || g.Spec.Workspace.Repo != "" {
		var err error
		dir, err = m.opt.Workspaces.Ensure(ctx, g)
		if err != nil {
			return nil, fmt.Errorf("%w: cannot prepare repository: %v", ErrInvalidDefinition, err)
		}
	}
	if err := m.ValidateDefinition(ctx, g, dir); err != nil {
		return nil, err
	}
	id := g.Metadata.ID
	cur, err := m.Get(ctx, g)
	if err != nil {
		return nil, err
	}
	if !force && cur.UpToDate {
		return cur, nil
	}
	// A local build that already recorded its result is only releasing its
	// lease; wait for it instead of reporting a conflict.
	m.mu.Lock()
	prev := m.running[id]
	m.mu.Unlock()
	if prev != nil && cur.State != StateBuilding {
		select {
		case <-prev.done:
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ok, err := m.opt.Store.AcquireLease(ctx, leasePrefix+id, m.opt.Instance, leaseTTL)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w for graph %s", ErrBuildRunning, id)
	}
	m.mu.Lock()
	if _, busy := m.running[id]; busy {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w for graph %s", ErrBuildRunning, id)
	}
	timeout := env.Timeout.D()
	if timeout <= 0 {
		timeout = m.opt.DefaultTimeout
	}
	bctx, cancel := context.WithTimeout(context.Background(), timeout)
	b := &build{cancel: cancel, done: make(chan struct{}), force: force}
	m.running[id] = b
	m.mu.Unlock()

	now := time.Now().UTC()
	st := &Status{Phase: "preparing", BuildReasons: buildReasons(cur, force), Graph: id, State: StateBuilding, Hash: Hash(env), StartedAt: &now, Actor: actor, Instance: m.opt.Instance}
	if err := m.save(ctx, st); err != nil {
		m.finish(id, b)
		_ = m.opt.Store.ReleaseLease(context.Background(), leasePrefix+id, m.opt.Instance)
		return nil, err
	}
	m.publish(ctx, domain.EvEnvBuildStarted, id, actor, map[string]any{"hash": st.Hash, "force": force, "buildReasons": st.BuildReasons})
	snapshot := *g // the definition being built, independent of later saves
	view := m.view(g, st)
	go m.run(bctx, b, &snapshot, st)
	return view, nil
}

func (m *Manager) finish(id string, b *build) {
	m.mu.Lock()
	if m.running[id] == b {
		delete(m.running, id)
	}
	m.mu.Unlock()
	b.cancel()
	close(b.done)
}

// run executes a build and records its outcome. It owns b.
func (m *Manager) run(ctx context.Context, b *build, g *domain.Graph, st *Status) {
	id := g.Metadata.ID
	defer m.finish(id, b)
	defer func() { _ = m.opt.Store.ReleaseLease(context.Background(), leasePrefix+id, m.opt.Instance) }()

	ctx, span := observability.Start(ctx, "environment.build",
		attribute.String("weft.graph", id), attribute.String("weft.environment.hash", st.Hash))
	ctx = progressContext(ctx)
	logBuf := &tailBuffer{limit: m.opt.LogLimit}
	stopFlush := m.flushWhileRunning(ctx, st, logBuf)

	start := time.Now()
	result := *st
	var image string
	var caches []CacheMount
	var err error
	image, caches, err = m.withBuildCapacity(ctx, g, logBuf, func(ctx context.Context) (string, []CacheMount, error) {
		if g.Spec.Workspace.Repo != "" {
			return m.sharedSteps(ctx, g, &result, b.force, logBuf)
		}
		return m.steps(ctx, g, st.Hash, logBuf)
	})

	stopFlush()
	snapshotProgress(ctx, st)
	st.Phase = ""
	st.ReuseKey, st.Reused, st.Inputs, st.Scope = result.ReuseKey, result.Reused, result.Inputs, result.Scope
	st.SharedImage = result.SharedImage

	now := time.Now().UTC()
	st.FinishedAt, st.Log = &now, logBuf.String()
	outcome := "ready"
	if err != nil {
		outcome = "failed"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("build timed out after %s", time.Since(start).Round(time.Second))
		} else if errors.Is(ctx.Err(), context.Canceled) {
			err = errors.New("build cancelled")
		}
		st.State, st.Error, st.Image, st.Caches = StateFailed, err.Error(), "", nil
	} else {
		st.State, st.Error, st.Image, st.Caches = StateReady, "", image, caches
	}
	observability.EnvironmentBuilds.WithLabelValues(outcome).Inc()
	observability.EnvironmentBuildSeconds.WithLabelValues(outcome).Observe(time.Since(start).Seconds())
	observability.EndSpan(span, err)

	// A reset during the build removed the record; do not resurrect it.
	if errors.Is(ctx.Err(), context.Canceled) {
		if cur, lerr := m.load(context.Background(), id); lerr == nil && cur.State == StateAbsent {
			return
		}
	}
	if serr := m.save(context.Background(), st); serr != nil {
		m.opt.Log.Error("environment status not saved", "graph", id, "err", serr)
	}
	data := map[string]any{"hash": st.Hash, "durationMs": time.Since(start).Milliseconds(), "buildReasons": st.BuildReasons, "reuseDecision": st.ReuseDecision}
	if err != nil {
		data["error"] = st.Error
		m.opt.Log.Warn("environment build failed", "graph", id, "hash", st.Hash, "err", err)
		m.publish(context.Background(), domain.EvEnvFailed, id, st.Actor, data)
		return
	}
	data["image"] = image
	data["reused"] = st.Reused
	data["reuseKey"] = st.ReuseKey
	m.opt.Log.Info("environment ready", "graph", id, "image", image, "duration", time.Since(start).Round(time.Millisecond))
	m.publish(context.Background(), domain.EvEnvReady, id, st.Actor, data)
}

// flushWhileRunning stores the log tail periodically and renews the build
// lease, so the console can follow a long build and other instances know it
// is alive.
func (m *Manager) flushWhileRunning(ctx context.Context, st *Status, logBuf *tailBuffer) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(flushInterval)
		defer t.Stop()
		renew := time.Now()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				snap := *st
				snap.Log = logBuf.String()
				snapshotProgress(ctx, &snap)
				if err := m.save(context.Background(), &snap); err != nil {
					m.opt.Log.Error("environment progress not saved", "graph", st.Graph, "err", err)
				}
				if time.Since(renew) > leaseTTL/3 {
					_, _ = m.opt.Store.AcquireLease(context.Background(), leasePrefix+st.Graph, m.opt.Instance, leaseTTL)
					renew = time.Now()
				}
			}
		}
	}()
	return func() { close(stop); <-done }
}

// steps builds the image: base (pull or Dockerfile), the sandbox user layer,
// then setup commands committed into the final image.
func (m *Manager) steps(ctx context.Context, g *domain.Graph, hash string, log io.Writer) (string, []CacheMount, error) {
	env := g.Spec.Environment
	id := g.Metadata.ID
	final := ImageRef(id, hash)
	setProgress(ctx, "building_image", "private")
	userTag, err := m.prepareImage(ctx, g, final, log)
	if err != nil {
		return "", nil, err
	}
	defer m.docker(context.Background(), nil, io.Discard, "rmi", userTag) //nolint:errcheck

	setProgress(ctx, "setup", "")
	if len(env.Setup) == 0 {
		if err := m.docker(ctx, nil, log, "tag", userTag, final); err != nil {
			return "", nil, err
		}
	} else {
		dir, err := m.opt.Workspaces.Ensure(ctx, g)
		if err != nil {
			return "", nil, fmt.Errorf("prepare workspace: %w", err)
		}
		name := buildContainer(id)
		_ = m.docker(context.Background(), nil, io.Discard, "rm", "-f", name)
		// No terminal: CI=true makes package managers (pnpm, npm, yarn,
		// apt front ends) proceed instead of waiting for a confirmation.
		run := []string{"run", "--name", name, "--network", SetupNetwork(env),
			"--cap-drop", "ALL", "--security-opt", "no-new-privileges", "-e", "CI=true",
			"--label", "weft.graph=" + id, "--label", "weft.environment.build=true",
			"-v", m.opt.Workspaces.HostPath(dir) + ":/workspace", "-w", "/workspace"}
		if env.CPU != "" {
			run = append(run, "--cpus", env.CPU)
		}
		if env.Memory != "" {
			run = append(run, "--memory", env.Memory)
		}
		run = append(run, "--entrypoint", "sh", userTag, "-c", SetupScript(env.Setup))
		fmt.Fprintf(log, "==> Running %d setup command(s) (network %s)\n", len(env.Setup), SetupNetwork(env))
		err = m.docker(ctx, nil, log, run...)
		if err == nil {
			fmt.Fprintln(log, "==> Saving the environment image")
			err = m.docker(ctx, nil, log, "commit", name, final)
		}
		_ = m.docker(context.Background(), nil, io.Discard, "rm", "-f", name)
		if err != nil {
			return "", nil, fmt.Errorf("setup: %w", err)
		}
	}

	var caches []CacheMount
	for _, dir := range env.Caches {
		vol := CacheVolume(id, dir)
		if err := m.docker(ctx, nil, io.Discard, "volume", "create", "--label", "weft.graph="+id, vol); err != nil {
			return "", nil, fmt.Errorf("create cache volume for %s: %w", dir, err)
		}
		caches = append(caches, CacheMount{Path: dir, Volume: vol})
	}
	m.removeOldImages(id, hash)
	fmt.Fprintf(log, "==> Environment ready: %s\n", final)
	return final, caches, nil
}

// prepareImage never mounts the mutable workspace or runs setup.
func (m *Manager) prepareImage(ctx context.Context, g *domain.Graph, final string, log io.Writer) (string, error) {
	if m.opt.Capacity != nil {
		return m.prepareBoundedImage(ctx, g, final, log)
	}
	env, id := g.Spec.Environment, g.Metadata.ID
	hash := Hash(env)
	baseTag, userTag := final+"-base", final+"-user"
	labels := []string{"--label", "weft.graph=" + id, "--label", "weft.environment=" + hash}

	base := strings.TrimSpace(env.Image)
	if base != "" {
		fmt.Fprintf(log, "==> Pulling base image %s\n", base)
		if err := m.docker(ctx, nil, log, "pull", base); err != nil {
			return "", fmt.Errorf("pull %s: %w", base, err)
		}
	} else {
		fmt.Fprintln(log, "==> Building the environment Dockerfile")
		args := append([]string{"build", "--pull", "--network", buildNetwork(env), "-t", baseTag}, labels...)
		if env.Target != "" {
			fmt.Fprintf(log, "==> Selected build stage: %s\n", env.Target)
			args = append(args, "--target", env.Target)
		}
		var input io.Reader = strings.NewReader(env.Dockerfile)
		if env.Context != "" {
			dir, err := m.opt.Workspaces.Ensure(ctx, g)
			if err != nil {
				return "", fmt.Errorf("prepare build context: %w", err)
			}
			snapshot, err := SnapshotContext(ctx, dir, env)
			if err != nil {
				return "", fmt.Errorf("capture build context: %w", err)
			}
			defer snapshot.Close()
			if env.Origin == nil || snapshot.SHA256 != env.Origin.ContextSHA256 {
				return "", fmt.Errorf("%w: build context changed after review", ErrInvalidDefinition)
			}
			fmt.Fprintln(log, "==> Sending validated repository build context")
			input = snapshot.File
			args = append(args, "--file", contextDockerfile)
		}
		if err := m.docker(ctx, input, log, append(args, "-")...); err != nil {
			return "", fmt.Errorf("build Dockerfile: %w", err)
		}
		base = baseTag
		defer m.docker(context.Background(), nil, io.Discard, "rmi", baseTag) //nolint:errcheck
	}

	fmt.Fprintf(log, "==> Adding the sandbox user (uid %d)\n", SandboxUID)
	args := append([]string{"build", "--network", "none", "-t", userTag}, labels...)
	if err := m.docker(ctx, strings.NewReader(UserLayer(base, env.Caches)), log, append(args, "-")...); err != nil {
		return "", fmt.Errorf("add sandbox user (the base image needs /bin/sh): %w", err)
	}

	return userTag, nil
}

// removeOldImages drops images of earlier definitions of the graph.
func (m *Manager) removeOldImages(id, keep string) {
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := m.docker(ctx, nil, &out, "image", "ls", "--format", "{{.Repository}}:{{.Tag}}", Repository(id)); err != nil {
		return
	}
	for _, ref := range strings.Fields(out.String()) {
		if !strings.HasSuffix(ref, ":"+keep) {
			_ = m.docker(ctx, nil, io.Discard, "rmi", ref)
		}
	}
}

// CancelBuild stops only a build owned by this instance. It retains its status,
// images and caches; it is not an environment reset.
func (m *Manager) CancelBuild(ctx context.Context, graphID, actor string) error {
	m.mu.Lock()
	b := m.running[graphID]
	m.mu.Unlock()
	if b == nil {
		if m.leaseAlive(ctx, graphID) {
			return fmt.Errorf("%w: build belongs to another instance", ErrBuildRunning)
		}
		return nil
	}
	m.publish(ctx, "environment.build_cancel_requested", graphID, actor, nil)
	b.cancel()
	select {
	case <-b.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Reset removes the environment of a graph: a running build, its images,
// cache volumes and status. The next build starts from scratch.
func (m *Manager) Reset(ctx context.Context, graphID, actor string) error {
	m.mu.Lock()
	b := m.running[graphID]
	m.mu.Unlock()
	if b != nil {
		b.cancel()
		<-b.done
	} else if m.leaseAlive(ctx, graphID) {
		return fmt.Errorf("%w for graph %s on another instance; reset it after the build ends", ErrBuildRunning, graphID)
	}
	if m.opt.Capacity != nil {
		if err := m.cleanBuildResources(ctx, graphID); err != nil {
			return err
		}
		if err := m.opt.Capacity.Release(ctx, buildContainer(graphID)); err != nil {
			return err
		}
	}
	// Mark absent first so an interrupted build does not record a result.
	if err := m.save(ctx, &Status{Graph: graphID, State: StateAbsent}); err != nil {
		return err
	}
	var errs []string
	var out bytes.Buffer
	filter := "label=weft.graph=" + graphID
	// Only the setup container, by name: agents' sandbox containers carry
	// the same graph label and must never be touched here.
	_ = m.docker(ctx, nil, io.Discard, "rm", "-f", buildContainer(graphID))
	if err := m.docker(ctx, nil, &out, "image", "ls", "--format", "{{.Repository}}:{{.Tag}}", Repository(graphID)); err != nil {
		errs = append(errs, "list images: "+err.Error())
	}
	for _, ref := range strings.Fields(out.String()) {
		if err := m.docker(ctx, nil, io.Discard, "rmi", "-f", ref); err != nil {
			errs = append(errs, "remove "+ref+": "+err.Error())
		}
	}
	out.Reset()
	if err := m.docker(ctx, nil, &out, "volume", "ls", "-q", "--filter", filter); err != nil {
		errs = append(errs, "list caches: "+err.Error())
	}
	for _, v := range strings.Fields(out.String()) {
		if err := m.docker(ctx, nil, io.Discard, "volume", "rm", "-f", v); err != nil {
			errs = append(errs, "remove cache "+v+": "+err.Error()+" (stop the graph's agents first)")
		}
	}
	if err := m.opt.Store.KVDelete(ctx, kvNamespace, graphID); err != nil && !errors.Is(err, storage.ErrNotFound) {
		errs = append(errs, err.Error())
	}
	if err := m.opt.Store.KVDelete(ctx, proxyHistoryNamespace, graphID); err != nil && !errors.Is(err, storage.ErrNotFound) {
		errs = append(errs, "remove proxy diagnostics: "+err.Error())
	}
	m.publish(ctx, domain.EvEnvReset, graphID, actor, map[string]any{"errors": len(errs)})
	if len(errs) > 0 {
		return fmt.Errorf("environment reset incomplete: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Exists reports whether the graph has an environment record (built,
// building, failed), i.e. whether Reset has anything to clean up.
func (m *Manager) Exists(ctx context.Context, graphID string) (bool, error) {
	st, err := m.load(ctx, graphID)
	if err != nil {
		return false, err
	}
	return st.State != StateAbsent, nil
}

// Sandbox is what a docker sandbox needs from a ready environment.
type Sandbox struct {
	AllowedHosts []string
	Companions   []domain.Companion
	Image        string
	Volumes      []string // "volume:/path"
	Network      string   // default network of agent sandboxes
}

// SandboxFor returns the environment image for g, or ErrNotReady with an
// actionable message.
func (m *Manager) SandboxFor(ctx context.Context, g *domain.Graph) (*Sandbox, error) {
	if g.Spec.Environment == nil {
		return nil, ErrNotDefined
	}
	dir := ""
	if g.Spec.Environment.Origin != nil {
		var err error
		dir, err = m.opt.Workspaces.Ensure(ctx, g)
		if err != nil {
			return nil, err
		}
	}
	if err := m.ValidateDefinition(ctx, g, dir); err != nil {
		return nil, err
	}
	v, err := m.Get(ctx, g)
	if err != nil {
		return nil, err
	}
	if !v.UpToDate {
		return nil, notReady(v)
	}
	sb := &Sandbox{AllowedHosts: g.Spec.Environment.Network.AllowedHosts, Companions: g.Spec.Environment.Companions, Image: v.Image, Network: g.Spec.Environment.Network.Run}
	for _, c := range v.Caches {
		sb.Volumes = append(sb.Volumes, c.Volume+":"+c.Path)
	}
	return sb, nil
}

// Check is the start preflight: nil when the environment can serve agents.
func (m *Manager) Check(ctx context.Context, g *domain.Graph) error {
	_, err := m.SandboxFor(ctx, g)
	return err
}

func notReady(v *View) error {
	switch {
	case v.State == StateBuilding:
		return fmt.Errorf("%w: it is being built; agents can start when the build finishes", ErrNotReady)
	case v.State == StateFailed && v.Hash == v.CurrentHash:
		return fmt.Errorf("%w: the last build failed (%s); fix the definition or build again in Graph settings → Environment", ErrNotReady, v.Error)
	case v.State == StateReady && !v.UpToDate:
		return fmt.Errorf("%w: the definition changed or repository inputs/scope changed; rebuild before starting agents", ErrNotReady)
	case v.Hash != "" && v.Hash != v.CurrentHash:
		return fmt.Errorf("%w: the definition changed since the last build; build it in Graph settings → Environment", ErrNotReady)
	default:
		return fmt.Errorf("%w: it has not been built yet; build it in Graph settings → Environment", ErrNotReady)
	}
}

func (m *Manager) leaseAlive(ctx context.Context, graphID string) bool {
	ls, err := m.opt.Store.ListLeases(ctx, leasePrefix+graphID)
	if err != nil {
		return true // cannot tell: do not declare a live build dead
	}
	now := time.Now()
	for _, l := range ls {
		if l.Key == leasePrefix+graphID && l.ExpiresAt.After(now) {
			return true
		}
	}
	return false
}

func (m *Manager) load(ctx context.Context, graphID string) (*Status, error) {
	b, err := m.opt.Store.KVGet(ctx, kvNamespace, graphID)
	if errors.Is(err, storage.ErrNotFound) {
		return &Status{Graph: graphID, State: StateAbsent}, nil
	}
	if err != nil {
		return nil, err
	}
	var st Status
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("environment status of %s: %w", graphID, err)
	}
	return &st, nil
}

func (m *Manager) save(ctx context.Context, st *Status) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return m.opt.Store.KVPut(ctx, kvNamespace, st.Graph, b)
}

func (m *Manager) publish(ctx context.Context, t domain.EventType, graphID, actor string, data map[string]any) {
	if m.opt.Bus == nil {
		return
	}
	ev := domain.NewEvent(t, graphID, data)
	if actor != "" {
		ev = ev.WithActor(actor)
	}
	m.opt.Bus.Publish(ctx, ev)
}

// docker runs one CLI call against the sandbox daemon, streaming output.
func (m *Manager) docker(ctx context.Context, stdin io.Reader, out io.Writer, args ...string) error {
	var err error
	args, err = preparationArgs(ctx, args)
	if err != nil {
		return err
	}
	args = boundedRunArgs(ctx, args)
	cmd := tools.DockerCommand(ctx, m.opt.DockerBin, m.opt.DockerHost, args...)
	cmd.Stdin = stdin
	var tail tailBuffer
	tail.limit = 2048
	w := io.MultiWriter(out, &tail)
	cmd.Stdout, cmd.Stderr = w, w
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg := lastLine(tail.String()); msg != "" {
			return fmt.Errorf("%v: %s", err, msg)
		}
		return err
	}
	return nil
}

func buildContainer(graphID string) string { return "weft-envbuild-" + graphID }

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// tailBuffer keeps the last limit bytes written; safe for concurrent use.
type tailBuffer struct {
	mu        sync.Mutex
	buf       []byte
	limit     int
	truncated bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.limit; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
		t.truncated = true
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.truncated {
		return "[earlier output truncated]\n" + string(t.buf)
	}
	return string(t.buf)
}

// ValidateDefinition checks both filesystem provenance and durable approval.
func (m *Manager) ValidateDefinition(ctx context.Context, g *domain.Graph, dir string) error {
	if err := m.ValidatePolicy(ctx, g.Spec.Environment); err != nil {
		m.opt.Log.Warn("environment definition denied by installation policy", "graph", g.Metadata.ID)
		return err
	}
	if g.Spec.Environment != nil && g.Spec.Environment.Origin != nil && g.Spec.Environment.Origin.Source == "agent" {
		if m.opt.ProposalValidator == nil {
			return fmt.Errorf("%w: agent proposal approval validator is unavailable", ErrInvalidDefinition)
		}
		if err := m.opt.ProposalValidator(ctx, g); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDefinition, err)
		}
	}
	return ValidateAccepted(ctx, dir, g.Spec.Environment)
}
func (m *Manager) SetProposalValidator(fn func(context.Context, *domain.Graph) error) {
	m.opt.ProposalValidator = fn
}

// ValidatePolicy checks installation restrictions before review, build and reuse.
func (m *Manager) ValidatePolicy(ctx context.Context, env *domain.EnvironmentSpec) error {
	policy, err := m.CurrentPolicy(ctx)
	if err != nil {
		return fmt.Errorf("read installation network policy: %w", err)
	}
	if err := policy.Check(env); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDefinition, err)
	}
	if env != nil && env.Network.Setup == "egress" && m.opt.Capacity == nil {
		return fmt.Errorf("%w: preparation egress requires capacity management", ErrInvalidDefinition)
	}
	if env != nil && env.Dockerfile != "" {
		if err := validateRepositoryDockerfile(env.Dockerfile); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDefinition, err)
		}
	}
	return nil
}

// CurrentPolicy reads shared settings for every admission. Storage errors fail
// closed; no controller-local cache can silently retain an older host ceiling.
func (m *Manager) CurrentPolicy(ctx context.Context) (environmentpolicy.Policy, error) {
	return (environmentpolicy.Settings{Store: m.opt.Store, Base: m.opt.Policy}).Effective(ctx)
}
