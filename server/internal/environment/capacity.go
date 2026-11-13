package environment

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/tools"
)

type buildBudgetKey struct{}

const builderCleanupTimeout = time.Minute

type buildBudget struct {
	id     string
	memory int64
	cpu    int64
}

func builderName(id string) string      { return "weft-builder-" + id }
func builderContainer(id string) string { return "buildx_buildkit_" + builderName(id) + "0" }

// Only the graph's reserved builder name is eligible for reconciliation.
func (m *Manager) removeStaleBuilder(ctx context.Context, id, endpoint string, log io.Writer) error {
	var flags []string
	if endpoint != "" {
		flags = []string{"--context", endpoint}
	}
	tls := m.usesBuilderTLSContext()
	var builders bytes.Buffer
	if err := m.builderDocker(ctx, tls, &builders, "list bounded builders", append(flags, "buildx", "ls", "--format", "{{.Name}}")...); err != nil {
		return err
	}
	for _, name := range strings.Fields(builders.String()) {
		if strings.TrimSuffix(name, "*") == builderName(id) {
			return m.builderDocker(ctx, tls, log, "remove stale bounded builder", append(flags, "buildx", "rm", "--force", builderName(id))...)
		}
	}
	return nil
}

// One reservation covers sequential build, cache copies and setup. It survives
// crashes until this graph's next build/reset confirms all workers are gone.
func (m *Manager) withBuildCapacity(ctx context.Context, g *domain.Graph, log io.Writer, run func(context.Context) (string, []CacheMount, error)) (image string, caches []CacheMount, err error) {
	if m.opt.Capacity == nil {
		if g.Spec.Environment.Network.Setup == "egress" {
			return "", nil, fmt.Errorf("preparation egress requires capacity management")
		}
		return run(ctx)
	}
	budget := *m.opt.Capacity
	budget.Notify = func(name, state string) {
		if state == "waiting" {
			setProgress(ctx, "waiting_capacity", "")
		} else {
			setProgress(ctx, "preparing", "")
		}
		fmt.Fprintf(log, "==> Environment capacity: %s\n", state)
		m.opt.Log.Info("environment capacity", "graph", g.Metadata.ID, "state", state)
		m.publish(context.WithoutCancel(ctx), domain.EventType("sandbox.capacity"), g.Metadata.ID, "", map[string]any{"phase": "environment", "state": state})
	}
	memory, err := budget.Memory(g.Spec.Environment.Memory)
	if err != nil {
		return "", nil, err
	}
	cpu, err := budget.CPU(g.Spec.Environment.CPU)
	if err != nil {
		return "", nil, err
	}
	// The graph build lease excludes concurrent recovery of the same pipeline.
	if err := m.cleanBuildResources(ctx, g.Metadata.ID); err != nil {
		return "", nil, err
	}
	if err := budget.Release(ctx, buildContainer(g.Metadata.ID)); err != nil {
		return "", nil, err
	}
	inventory := func(ctx context.Context) ([]sandboxcapacity.Container, error) {
		return tools.DockerCapacityInventory(ctx, func(ctx context.Context, args ...string) (string, error) {
			var out bytes.Buffer
			err := m.docker(ctx, nil, &out, args...)
			return out.String(), err
		})
	}
	count, reservedMemory, reservedCPU := 1, memory, cpu
	if g.Spec.Environment.Network.Setup == "egress" {
		count = 2
		reservedMemory += preparationProxyMemory
		reservedCPU += preparationProxyCPU
	}
	if err := budget.ReserveGroup(ctx, buildContainer(g.Metadata.ID), reservedMemory, reservedCPU, count, inventory); err != nil {
		return "", nil, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), builderCleanupTimeout)
		defer cancel()
		if g.Spec.Environment.Network.Setup == "egress" {
			if captureErr := m.capturePreparationProxy(cleanup, g.Metadata.ID); captureErr != nil {
				fmt.Fprintln(log, "==> Preparation proxy diagnostics could not be saved")
				m.opt.Log.Warn("preparation proxy diagnostics unavailable", "graph", g.Metadata.ID, "err", captureErr)
			}
			fmt.Fprintln(log, "==> Preparation proxy request outcomes (last 50)")
			if logErr := m.docker(cleanup, nil, log, "logs", "--tail", "50", preparationProxy(g.Metadata.ID)); logErr != nil {
				m.opt.Log.Warn("preparation proxy logs unavailable", "graph", g.Metadata.ID, "err", logErr)
			}
		}
		e := m.cleanBuildResources(cleanup, g.Metadata.ID)
		if e == nil {
			e = budget.Release(cleanup, buildContainer(g.Metadata.ID))
		}
		if e != nil {
			fmt.Fprintf(log, "==> Capacity retained: cleanup failed: %v\n", e)
			err = errors.Join(err, fmt.Errorf("environment cleanup (retry build or reset): %w", e))
		}
	}()
	if err := m.ValidatePolicy(ctx, g.Spec.Environment); err != nil {
		return "", nil, err
	}
	if g.Spec.Environment.Network.Setup == "egress" {
		ctx, err = m.startPreparationProxy(ctx, g, log)
		if err != nil {
			return "", nil, err
		}
	}
	return run(context.WithValue(ctx, buildBudgetKey{}, buildBudget{g.Metadata.ID, memory, cpu}))
}

// Names are deterministic so a new control-plane process can recover workers.
func (m *Manager) cleanBuildResources(ctx context.Context, id string) error {
	for _, name := range []string{builderContainer(id), buildContainer(id), buildContainer(id) + "-copy", preparationProxy(id)} {
		if err := m.removeBuildWorker(ctx, name); err != nil {
			return err
		}
	}
	var networks bytes.Buffer
	if err := m.docker(ctx, nil, &networks, "network", "ls", "--filter", "name=^"+preparationNetworkName(id)+"$", "--format", "{{.Name}}"); err != nil {
		return fmt.Errorf("list preparation networks: %w", err)
	}
	for _, name := range strings.Fields(networks.String()) {
		if name == preparationNetworkName(id) {
			if err := m.docker(ctx, nil, io.Discard, "network", "rm", name); err != nil {
				return fmt.Errorf("remove preparation network: %w", err)
			}
		}
	}
	return nil
}

func (m *Manager) removeBuildWorker(ctx context.Context, name string) error {
	var out bytes.Buffer
	if err := m.docker(ctx, nil, &out, "ps", "-aq", "--filter", "name=^/"+regexp.QuoteMeta(name)+"$"); err != nil {
		return fmt.Errorf("list build workers: %w", err)
	}
	if ids := strings.Fields(out.String()); len(ids) > 0 {
		if err := m.docker(ctx, nil, io.Discard, append([]string{"rm", "-f"}, ids...)...); err != nil {
			return fmt.Errorf("remove build worker: %w", err)
		}
	}
	return nil
}

func boundedRunArgs(ctx context.Context, args []string) []string {
	b, ok := ctx.Value(buildBudgetKey{}).(buildBudget)
	if !ok || len(args) == 0 || args[0] != "run" {
		return args
	}
	result := []string{"run", "--memory", strconv.FormatInt(b.memory, 10), "--memory-swap", strconv.FormatInt(b.memory, 10), "--cpus", sandboxcapacity.FormatCPU(b.cpu)}
	named := false
	// All arguments originate in this package, with separate flag/value tokens.
	for i := 1; i < len(args); i++ {
		if args[i] == "--memory" || args[i] == "--memory-swap" || args[i] == "--cpus" {
			i++
			continue
		}
		if args[i] == "--name" {
			named = true
		}
		result = append(result, args[i])
	}
	if !named {
		result = append([]string{result[0], "--name", buildContainer(b.id) + "-copy"}, result[1:]...)
	}
	return result
}

// combinedBuildfile keeps local stages inside one BuildKit solve. The generated
// user layer must inherit the selected stage, not a daemon-local intermediate tag.
func combinedBuildfile(source, target string, caches []string) (string, error) {
	stages, err := graph.DockerfileStages(source)
	if err != nil {
		return "", err
	}
	if len(stages) == 0 {
		return "", fmt.Errorf("Dockerfile requires FROM")
	}
	if err := graph.ValidateBuildTarget(source, target); err != nil {
		return "", err
	}
	selected := len(stages) - 1
	if target != "" {
		for i, name := range stages {
			if name == strings.ToLower(target) {
				selected = i
			}
		}
	}
	lines, err := graph.DockerfileLines(source)
	if err != nil {
		return "", err
	}
	alias := stages[selected]
	if alias == "" {
		alias = "weft-selected"
		// Avoid collisions with both named stages and external image references.
		for strings.Contains(strings.ToLower(source), alias) {
			alias += "x"
		}
		stage := -1
		for i, line := range lines {
			if strings.EqualFold(strings.Fields(line)[0], "FROM") {
				stage++
				if stage == selected {
					lines[i] += " AS " + alias
				}
			}
		}
	}
	return strings.Join(lines, "\n") + "\n" + UserLayer(alias, caches), nil
}

func (m *Manager) prepareBoundedImage(ctx context.Context, g *domain.Graph, final string, log io.Writer) (tag string, err error) {
	b, ok := ctx.Value(buildBudgetKey{}).(buildBudget)
	if !ok {
		return "", fmt.Errorf("environment build requires capacity admission")
	}
	env := g.Spec.Environment
	if _, ok := ctx.Value(preparationKey{}).(preparationNetwork); env.Network.Setup == "egress" && !ok {
		return "", fmt.Errorf("preparation egress proxy is not admitted")
	}
	source := env.Dockerfile
	var snapshot *ContextSnapshot
	if env.Context != "" {
		dir, e := m.opt.Workspaces.Ensure(ctx, g)
		if e != nil {
			return "", e
		}
		snapshot, e = SnapshotContext(ctx, dir, env)
		if e != nil {
			return "", e
		}
		defer snapshot.Close()
		if env.Origin == nil || snapshot.SHA256 != env.Origin.ContextSHA256 {
			return "", fmt.Errorf("%w: build context changed after review", ErrInvalidDefinition)
		}
		source, e = contextBuildfile(source)
		if e != nil {
			return "", e
		}
	}
	var dockerfile string
	if env.Image != "" {
		dockerfile = UserLayer(env.Image, env.Caches)
	} else {
		dockerfile, err = combinedBuildfile(source, env.Target, env.Caches)
		if err != nil {
			return "", err
		}
	}
	var input io.Reader = strings.NewReader(dockerfile)
	args := []string{"buildx", "build", "--network", buildNetwork(env), "--builder", builderName(b.id), "--load", "--pull", "--progress", "plain", "-t", final + "-user", "--label", "weft.graph=" + b.id, "--label", "weft.environment=" + Hash(env)}
	if snapshot != nil {
		f, e := wrappedContext(snapshot.File, dockerfile)
		if e != nil {
			return "", e
		}
		defer func() { f.Close(); os.Remove(f.Name()) }()
		input = f
		args = append(args, "--file", contextDockerfile)
	}
	endpoint, err := m.builderContext(ctx, b.id, log)
	if err != nil {
		return "", err
	}
	var dockerFlags []string
	if endpoint != "" {
		dockerFlags = []string{"--context", endpoint}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), builderCleanupTimeout)
			defer cancel()
			m.warnBuilderCleanup(b.id, log, "remove Docker TLS context", m.removeBuilderContext(cleanup, endpoint))
		}()
	}
	// Physical workers were already reconciled before admission.
	if err := m.removeStaleBuilder(ctx, b.id, endpoint, log); err != nil {
		return "", err
	}
	memory := strconv.FormatInt(b.memory, 10)
	create := []string{"buildx", "create", "--name", builderName(b.id), "--driver", "docker-container", "--driver-opt", "memory=" + memory + ",memory-swap=" + memory + ",restart-policy=no,cpu-period=100000,cpu-quota=" + strconv.FormatInt(b.cpu*100, 10)}
	if n, ok := ctx.Value(preparationKey{}).(preparationNetwork); ok {
		// The worker has no uplink. RUN DNS is disabled; proxy requests use the
		// inspected internal address, never Docker's external DNS forwarding.
		config, err := os.CreateTemp("", "weft-buildkitd-*.toml")
		if err != nil {
			return "", fmt.Errorf("create preparation worker config: %w", err)
		}
		defer os.Remove(config.Name())
		// Buildx reads this file twice, first for entitlements and then for the
		// configuration copied to the worker. A stdin pipe cannot be reread.
		_, err = config.WriteString("[dns]\n  nameservers = [\"127.0.0.1\"]\n[worker.oci]\n  networkMode = \"bridge\"\n")
		if err := errors.Join(err, config.Close()); err != nil {
			return "", fmt.Errorf("write preparation worker config: %w", err)
		}
		create = append(create, "--buildkitd-config", config.Name(), "--driver-opt", "network="+n.name)
		for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
			create = append(create, "--driver-opt", "env."+key+"="+n.proxyURL)
			args = append(args, "--build-arg", key+"="+n.proxyURL)
		}
		for _, key := range []string{"NO_PROXY", "no_proxy"} {
			create = append(create, "--driver-opt", "env."+key+"=")
			args = append(args, "--build-arg", key+"=")
		}
	}
	if endpoint != "" {
		create = append(create, endpoint)
	}
	if err = m.builderDocker(ctx, endpoint != "", log, "create bounded builder", append(dockerFlags, create...)...); err != nil {
		if endpoint != "" {
			return "", err
		}
		return "", fmt.Errorf("create bounded builder: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), builderCleanupTimeout)
		defer cancel()
		e := m.builderDocker(cleanup, endpoint != "", io.Discard, "remove bounded builder", append(dockerFlags, "buildx", "rm", "--force", builderName(b.id))...)
		if e != nil {
			m.warnBuilderCleanup(b.id, log, "remove bounded builder", e)
			// Reconcile the physical worker before setup starts, even when Buildx
			// could not clean up its local record.
			if workerErr := m.removeBuildWorker(cleanup, builderContainer(b.id)); workerErr != nil {
				err = errors.Join(err, workerErr)
			}
		} else {
			fmt.Fprintf(log, "%s removed\n", builderName(b.id))
		}
	}()
	fmt.Fprintf(log, "==> Building with a %d-byte worker memory cap\n", b.memory)
	if err = m.docker(ctx, input, log, append(dockerFlags, append(args, "-")...)...); err != nil {
		return "", fmt.Errorf("bounded Dockerfile build: %w", err)
	}
	return final + "-user", nil
}

// Transform only the generated Dockerfile after validating the original digest.
func wrappedContext(input io.Reader, dockerfile string) (file *os.File, err error) {
	file, err = os.CreateTemp("", "weft-bounded-context-*.tar")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			file.Close()
			os.Remove(file.Name())
		}
	}()
	tr := tar.NewReader(input)
	tw := tar.NewWriter(file)
	found := false
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return file, e
		}
		var body io.Reader = tr
		if h.Name == contextDockerfile {
			h.Size = int64(len(dockerfile))
			body = strings.NewReader(dockerfile)
			found = true
		}
		if e = tw.WriteHeader(h); e != nil {
			return file, e
		}
		if _, e = io.Copy(tw, body); e != nil {
			return file, e
		}
	}
	if !found {
		return file, fmt.Errorf("snapshot lacks generated Dockerfile")
	}
	if err = tw.Close(); err != nil {
		return file, err
	}
	_, err = file.Seek(0, io.SeekStart)
	return file, err
}
