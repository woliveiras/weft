package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
)

// EgressConfig is supplied by the installation after environment policy validation.
type EgressConfig struct {
	Image string
	Hosts []string
}

// DockerSandboxConfig describes the per-replica sandbox container.
type DockerSandboxConfig struct {
	// Command replaces the idle entrypoint for platform-managed preview services.
	Command []string
	// ValidateAdmission rechecks installation policy on start/resume, including after capacity waits.
	BeforeRemove       func(context.Context) error
	ValidateAdmission  func(context.Context) error
	Egress             *EgressConfig
	RequireStandalone  bool
	ProxyEnv           map[string]string
	DisableExternalDNS bool
	Capacity           *sandboxcapacity.Manager
	EnforceSwap        bool
	// Name of the container (unique per replica).
	Name string
	// Image to run (needs /bin/sh and whatever tools agents call: git, go...).
	Image string
	// HostDir is the workspace path as the Docker daemon sees it. When the
	// platform itself runs in a container this is the host path of the
	// workspace volume, not the path inside the platform container.
	HostDir           string
	WorkspaceReadOnly bool
	// Network: "none" (default) or a docker network name ("bridge").
	Network string
	CPU     string // e.g. "1.5"
	Memory  string // e.g. "512m"
	// DockerBin is the docker CLI (default "docker").
	DockerBin string
	// DockerHost is the daemon address passed to the CLI as DOCKER_HOST
	// (e.g. "tcp://sandbox-daemon:2376"). Empty = the CLI's own default.
	// TLS settings (DOCKER_TLS_VERIFY, DOCKER_CERT_PATH) come from the
	// platform's environment.
	DockerHost string
	Labels     map[string]string
	// Workdir inside the container (default /workspace).
	MountPoint string
	// User to run as inside the container (default: the image's user).
	User string
	// Volumes are extra mounts in docker -v syntax ("name:/path"), e.g. an
	// environment's dependency caches.
	Volumes []string
}

// DockerSandbox runs commands inside a long-lived container dedicated to
// one replica: only the workspace is mounted, networking is off unless the
// node asks for it, and CPU/memory are capped by the kernel. Commands are
// started with `docker exec`.
//
// Trade-off: driving Docker requires the platform to reach a Docker daemon.
// The Compose stack gives it a daemon of its own (the sandbox-daemon
// service, Docker-in-Docker) so sandboxes, images and caches never touch the
// host's Docker; mounting the host's /var/run/docker.sock instead is
// root-equivalent on that host. See docs/sandbox-runtime.md.
// In Kubernetes the equivalent is a sandbox pod (or sidecar) per replica;
// it plugs in by implementing the same Sandbox interface (Exec/Close).
//
// Timeouts kill the `docker exec` client; a command that ignores the hang-up
// may keep running in the container until the sandbox is closed.
type DockerSandbox struct {
	cfg      DockerSandboxConfig
	mu       sync.Mutex
	activity sync.RWMutex
	retained bool
	started  bool
	lifetime context.Context
	stop     context.CancelFunc
}

func NewDockerSandbox(cfg DockerSandboxConfig) (*DockerSandbox, error) {
	if cfg.Egress != nil {
		return nil, fmt.Errorf("egress requires managed proxy group")
	}
	if cfg.Name == "" || cfg.Image == "" || cfg.HostDir == "" {
		return nil, errors.New("docker sandbox: name, image and hostDir are required")
	}
	if cfg.DockerBin == "" {
		cfg.DockerBin = "docker"
	}
	if cfg.Network == "" {
		cfg.Network = "none"
	}
	if cfg.MountPoint == "" {
		cfg.MountPoint = "/workspace"
	}
	if cfg.Capacity != nil {
		n, err := cfg.Capacity.Memory(cfg.Memory)
		if err != nil {
			return nil, err
		}
		cfg.Memory = strconv.FormatInt(n, 10)
		cpu, err := cfg.Capacity.CPU(cfg.CPU)
		if err != nil {
			return nil, err
		}
		cfg.CPU = sandboxcapacity.FormatCPU(cpu)
	}
	lifetime, stop := context.WithCancel(context.Background())
	return &DockerSandbox{cfg: cfg, lifetime: lifetime, stop: stop}, nil
}

func (s *DockerSandbox) Kind() string { return "docker" }

// RunArgs returns the `docker run` arguments (exposed for tests and dry runs).
func (s *DockerSandbox) RunArgs() []string {
	c := s.cfg
	args := []string{"run", "-d", "--name", c.Name, "--restart", "no",
		"--network", c.Network,
		"--security-opt", "no-new-privileges",
		"--cap-drop", "ALL",
		"--label", "weft.sandbox=true",
	}
	keys := make([]string, 0, len(c.Labels))
	for k := range c.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--label", k+"="+c.Labels[k])
	}
	if c.DisableExternalDNS {
		args = append(args, "--dns", "127.0.0.1")
	}
	envKeys := make([]string, 0, len(c.ProxyEnv))
	for k := range c.ProxyEnv {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		args = append(args, "--env", k+"="+c.ProxyEnv[k])
	}
	if c.CPU != "" {
		args = append(args, "--cpus", c.CPU)
	}
	if c.Memory != "" {
		args = append(args, "--memory", c.Memory)
		if c.Capacity != nil || c.EnforceSwap {
			args = append(args, "--memory-swap", c.Memory)
		}
	}
	if c.User != "" {
		args = append(args, "--user", c.User)
	}
	for _, v := range c.Volumes {
		args = append(args, "-v", v)
	}
	mount := c.HostDir + ":" + c.MountPoint
	if c.WorkspaceReadOnly {
		mount += ":ro"
	}
	args = append(args, "-v", mount, "-w", c.MountPoint)
	if len(c.Command) > 0 {
		args = append(args, "--pids-limit", "512", "--log-opt", "max-size=1m", "--log-opt", "max-file=1", "--entrypoint", c.Command[0], c.Image)
		return append(args, c.Command[1:]...)
	}
	args = append(args, "--entrypoint", "sh", c.Image, "-c", "trap 'exit 0' TERM; while :; do sleep 3600 & wait $!; done")
	return args
}

// Start creates (or reuses) the sandbox container. Idempotent.
func (s *DockerSandbox) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	unregister := context.AfterFunc(s.lifetime, cancel)
	defer unregister()
	if s.lifetime.Err() != nil {
		return s.lifetime.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifetime.Err() != nil {
		return s.lifetime.Err()
	}
	if s.started {
		return nil
	}
	if s.cfg.ValidateAdmission != nil {
		if err := s.cfg.ValidateAdmission(ctx); err != nil {
			return err
		}
	}
	if s.cfg.Capacity != nil {
		n, _ := strconv.ParseInt(s.cfg.Memory, 10, 64)
		cpu, _ := sandboxcapacity.CPU(s.cfg.CPU)
		if err := s.cfg.Capacity.ReserveCPU(ctx, s.cfg.Name, n, cpu, s.inventory); err != nil {
			return err
		}
		if s.cfg.ValidateAdmission != nil {
			if err := s.cfg.ValidateAdmission(ctx); err != nil {
				return errors.Join(err, s.cfg.Capacity.Release(context.WithoutCancel(ctx), s.cfg.Name))
			}
		}
		if err := s.startContainer(ctx); err != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if s.retained {
				stopErr := s.stopContainer(cleanup)
				if stopErr != nil {
					return errors.Join(err, stopErr)
				}
				return errors.Join(err, s.cfg.Capacity.Release(cleanup, s.cfg.Name))
			}
			out, removeErr := s.docker(cleanup, "rm", "-f", s.cfg.Name)
			if removeErr == nil || strings.Contains(out, "No such container") {
				return errors.Join(err, s.cfg.Capacity.Release(cleanup, s.cfg.Name))
			}
			return errors.Join(err, fmt.Errorf("sandbox cleanup failed; reservation retained: %v: %s", removeErr, out))
		}
		s.started = true
		return nil
	}
	return s.startContainer(ctx)
}

func (s *DockerSandbox) startContainer(ctx context.Context) error {
	out, err := s.docker(ctx, "inspect", "-f", "{{.State.Running}}", s.cfg.Name)
	if err == nil {
		s.retained = true
	} else if !strings.Contains(out, "No such object") && !strings.Contains(out, "No such container") {
		s.retained = true // ambiguous inspection must never trigger destructive cleanup
		return fmt.Errorf("inspect sandbox: %w: %s", err, out)
	}
	if err == nil && s.cfg.WorkspaceReadOnly {
		format := fmt.Sprintf(`{{range .Mounts}}{{if eq .Destination %q}}{{.RW}}{{end}}{{end}}`, s.cfg.MountPoint)
		rw, inspectErr := s.docker(ctx, "inspect", "--format", format, s.cfg.Name)
		if inspectErr != nil || strings.TrimSpace(rw) != "false" {
			return fmt.Errorf("cannot resume sandbox: workspace mount is not verified read-only")
		}
	}
	if err == nil && s.cfg.RequireStandalone {
		group, e := s.docker(ctx, "inspect", "--format", `{{index .Config.Labels "weft.compose"}}`, s.cfg.Name)
		if e != nil {
			return fmt.Errorf("inspect sandbox ownership: %w", e)
		}
		if strings.TrimSpace(group) != "" && strings.TrimSpace(group) != "<no value>" {
			return fmt.Errorf("sandbox belongs to a managed service/proxy group; stop the previous task before replacement")
		}
	}
	if err == nil && s.cfg.Capacity != nil {
		if out, e := s.docker(ctx, "update", "--cpus", s.cfg.CPU, "--memory", s.cfg.Memory, "--memory-swap", s.cfg.Memory, s.cfg.Name); e != nil {
			return fmt.Errorf("apply sandbox memory/CPU caps: %v: %s", e, out)
		}
	}
	switch {
	case err == nil && strings.TrimSpace(out) == "true":
	case err == nil:
		if out, err := s.docker(ctx, "start", s.cfg.Name); err != nil {
			return fmt.Errorf("docker start %s: %v: %s", s.cfg.Name, err, out)
		}
	default:
		if out, err := s.docker(ctx, s.RunArgs()...); err != nil {
			return fmt.Errorf("docker run %s: %v: %s", s.cfg.Name, err, out)
		}
	}
	s.started = true
	return nil
}

// ExecArgs returns the `docker exec` arguments for req (exposed for tests).
func (s *DockerSandbox) ExecArgs(req ExecRequest) ([]string, error) {
	rel := path.Clean(strings.ReplaceAll(req.Dir, "\\", "/"))
	if rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
		return nil, fmt.Errorf("%w: directory %q escapes the workspace", ErrDenied, req.Dir)
	}
	dir := path.Clean("/" + rel)
	args := []string{"exec"}
	if req.Stdin != "" {
		args = append(args, "-i")
	}
	args = append(args, "-w", path.Join(s.cfg.MountPoint, dir))
	keys := make([]string, 0, len(req.Env))
	for k := range req.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+req.Env[k])
	}
	args = append(args, s.cfg.Name)
	switch {
	case req.Shell != "":
		args = append(args, "sh", "-c", req.Shell)
	case len(req.Argv) > 0:
		args = append(args, req.Argv...)
	default:
		return nil, errors.New("exec: empty command")
	}
	return args, nil
}

func (s *DockerSandbox) Exec(ctx context.Context, req ExecRequest) (*ExecResult, error) {
	s.activity.RLock()
	defer s.activity.RUnlock()
	if err := s.Start(ctx); err != nil {
		return nil, err
	}
	args, err := s.ExecArgs(req)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, clampTimeout(req.Timeout))
	defer cancel()
	cmd := DockerCommand(runCtx, s.cfg.DockerBin, s.cfg.DockerHost, args...)
	limit := req.MaxOutput
	if limit <= 0 {
		limit = DefaultMaxOutput
	}
	stdout, stderr := &cappedBuffer{limit: limit, tail: req.TailOutput}, &cappedBuffer{limit: limit, tail: req.TailOutput}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if req.Stdin != "" {
		cmd.Stdin = strings.NewReader(req.Stdin)
	}
	cmd.WaitDelay = 5 * time.Second
	start := time.Now()
	runErr := cmd.Run()
	res := &ExecResult{
		Stdout: stdout.String(), Stderr: stderr.String(), Duration: time.Since(start),
		Truncated: stdout.truncated || stderr.truncated,
		TimedOut:  errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil,
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if runErr != nil && cmd.ProcessState == nil && !res.TimedOut {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		return nil, fmt.Errorf("docker exec: %w", runErr)
	}
	if res.TimedOut && res.ExitCode == 0 {
		res.ExitCode = -1
	}
	return res, nil
}

// Suspend stops processes but preserves the container filesystem and all mounts.
// Active Exec calls finish first. Only a confirmed stop releases capacity.
func (s *DockerSandbox) Suspend(ctx context.Context) error {
	s.activity.Lock()
	defer s.activity.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.lifetime.Err(); err != nil {
		return err
	}
	if err := s.stopContainer(ctx); err != nil {
		return err
	}
	s.retained = true
	if s.cfg.Capacity != nil {
		return s.cfg.Capacity.Release(ctx, s.cfg.Name)
	}
	return nil
}

func (s *DockerSandbox) stopContainer(ctx context.Context) error {
	s.started = false // a failed CLI call may still have stopped the container
	s.retained = true
	if out, err := s.docker(ctx, "stop", "--time", "10", s.cfg.Name); err != nil {
		return fmt.Errorf("stop sandbox (capacity retained): %w: %s", err, out)
	}
	out, err := s.docker(ctx, "inspect", "-f", "{{.State.Running}}", s.cfg.Name)
	if err != nil || strings.TrimSpace(out) != "false" {
		return fmt.Errorf("sandbox stop not confirmed; capacity retained: %v: %s", err, out)
	}
	s.started = false
	return nil
}

// Close removes the sandbox container.
func (s *DockerSandbox) Close() error {
	s.stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := s.docker(ctx, "rm", "-f", s.cfg.Name)
	if err != nil && !strings.Contains(out, "No such container") {
		return fmt.Errorf("docker rm %s: %v: %s", s.cfg.Name, err, out)
	}
	s.started = false
	if s.cfg.Capacity != nil {
		return s.cfg.Capacity.Release(ctx, s.cfg.Name)
	}
	return nil
}

func (s *DockerSandbox) docker(ctx context.Context, args ...string) (string, error) {
	out, err := DockerCommand(ctx, s.cfg.DockerBin, s.cfg.DockerHost, args...).CombinedOutput()
	return string(out), err
}

// DockerCommand runs the docker CLI against host (DOCKER_HOST), keeping the
// rest of the platform environment (TLS settings, PATH).
func DockerCommand(ctx context.Context, bin, host string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, args...)
	if host != "" {
		cmd.Env = append(os.Environ(), "DOCKER_HOST="+host)
	}
	return cmd
}

// PingDocker reports whether the daemon at host answers, returning its
// server version. It backs the "sandbox" readiness check.
func PingDocker(ctx context.Context, bin, host string) (string, error) {
	if bin == "" {
		bin = "docker"
	}
	out, err := DockerCommand(ctx, bin, host, "version", "--format", "{{.Server.Version}}").CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("docker daemon %s unreachable: %s", displayHost(host), firstLine(text))
	}
	return text, nil
}

func displayHost(host string) string {
	if host == "" {
		return "(default)"
	}
	return host
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func (s *DockerSandbox) inventory(ctx context.Context) ([]sandboxcapacity.Container, error) {
	return DockerCapacityInventory(ctx, s.docker)
}

// DockerCapacityInventory accounts for running execution sandboxes. Environment
// pipelines use durable reservations for their sequential temporary containers.
func DockerCapacityInventory(ctx context.Context, docker func(context.Context, ...string) (string, error)) ([]sandboxcapacity.Container, error) {
	out, err := docker(ctx, "ps", "-q", "--filter", "label=weft.sandbox=true")
	if err != nil {
		return nil, fmt.Errorf("list active sandboxes: %w: %s", err, out)
	}
	ids := strings.Fields(out)
	if len(ids) == 0 {
		return nil, nil
	}
	args := append([]string{"inspect", "--format", `{{.Name}} {{.State.Running}} {{.HostConfig.Memory}} {{.HostConfig.NanoCpus}} {{if index .Config.Labels "weft.capacity-group"}}{{index .Config.Labels "weft.capacity-group"}}{{else}}-{{end}}`}, ids...)
	out, err = docker(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("inspect active sandboxes: %w: %s", err, out)
	}
	var result []sandboxcapacity.Container
	groups := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 && len(fields) != 5 {
			return nil, fmt.Errorf("invalid sandbox inventory response")
		}
		if fields[1] == "false" {
			continue
		}
		if fields[1] != "true" {
			return nil, fmt.Errorf("invalid sandbox state")
		}
		memory, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return nil, err
		}
		nano, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil || nano < 0 {
			return nil, fmt.Errorf("invalid sandbox CPU inventory")
		}
		cpu := nano / 1000000
		if nano%1000000 != 0 {
			cpu++
		}
		name := strings.TrimPrefix(fields[0], "/")
		if len(fields) == 5 && fields[4] != "-" {
			name = fields[4]
			if memory <= 0 || cpu <= 0 {
				return nil, fmt.Errorf("unbounded companion in sandbox group %s", name)
			}
			if index, ok := groups[name]; ok {
				previous := &result[index]
				if memory > math.MaxInt64-previous.Memory || cpu > math.MaxInt64-previous.MilliCPU {
					return nil, fmt.Errorf("sandbox inventory overflow")
				}
				previous.Memory += memory
				previous.MilliCPU += cpu
				previous.Count++
				continue
			}
			groups[name] = len(result)
		}
		result = append(result, sandboxcapacity.Container{Count: 1, MilliCPU: cpu, Name: name, Memory: memory})
	}
	return result, nil
}
