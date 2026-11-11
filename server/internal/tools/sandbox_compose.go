package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
)

// ComposeSandbox runs a reviewed dependency closure, never a repository-supplied
// compose command. Its entire per-replica group is admitted before any start.
type ComposeSandbox struct {
	egress                               bool
	main                                 *DockerSandbox
	companions                           []domain.Companion
	budget                               *sandboxcapacity.Manager
	memory, cpu, helperMemory, helperCPU int64
	fingerprint, network                 string
	mu                                   sync.Mutex
	activity                             sync.RWMutex
	started                              bool
	ctx                                  context.Context
	cancel                               context.CancelFunc
}

func NewComposeSandbox(cfg DockerSandboxConfig, companions []domain.Companion) (*ComposeSandbox, error) {
	if err := domain.ValidateCompanions(companions); err != nil {
		return nil, err
	}
	egress := cfg.Egress != nil
	companions = append([]domain.Companion(nil), companions...)
	if egress {
		if cfg.Egress.Image == "" || len(cfg.Egress.Hosts) == 0 {
			return nil, fmt.Errorf("egress requires installation proxy image and reviewed hosts")
		}
		if err := domain.ValidateEgressHosts(cfg.Egress.Hosts); err != nil {
			return nil, err
		}
		aliases := []string{}
		for _, c := range companions {
			if c.Name == "weft-egress" {
				return nil, fmt.Errorf("reserved proxy service name")
			}
			aliases = append(aliases, c.Name)
		}
		proxy := domain.Companion{Name: "weft-egress", Image: cfg.Egress.Image}
		if err := domain.ValidateCompanions([]domain.Companion{proxy}); err != nil {
			return nil, err
		}
		proxy.Command = []string{"--hosts", strings.Join(cfg.Egress.Hosts, ",")}
		companions = append(companions, proxy)
		cfg.ProxyEnv = map[string]string{"HTTP_PROXY": "http://weft-egress:3128", "HTTPS_PROXY": "http://weft-egress:3128", "http_proxy": "http://weft-egress:3128", "https_proxy": "http://weft-egress:3128", "NO_PROXY": strings.Join(aliases, ","), "no_proxy": strings.Join(aliases, ",")}
		cfg.DisableExternalDNS = true
	}
	if len(companions) == 0 || cfg.Capacity == nil {
		return nil, fmt.Errorf("Compose runtime requires companions and global capacity")
	}
	if cfg.Network != "" && cfg.Network != "none" {
		return nil, fmt.Errorf("Compose companions require internal-only networking; remove the agent network override")
	}
	cfg.Egress = nil
	main, err := NewDockerSandbox(cfg)
	if err != nil {
		return nil, err
	}
	hm, err := cfg.Capacity.Memory("")
	if err != nil {
		return nil, err
	}
	hc, err := cfg.Capacity.CPU("")
	if err != nil {
		return nil, err
	}
	mm, _ := strconv.ParseInt(main.cfg.Memory, 10, 64)
	mc, _ := sandboxcapacity.CPU(main.cfg.CPU)
	// Validate the aggregate before Docker. Each helper uses installation defaults.
	memory := mm
	cpu := mc
	for range companions {
		total, _ := sandboxcapacity.Bytes(cfg.Capacity.Policy.TotalMemory)
		if hm > total-memory {
			return nil, fmt.Errorf("Compose group exceeds memory budget")
		}
		memory += hm
		cpu += hc
	}
	if _, err := cfg.Capacity.CPU(sandboxcapacity.FormatCPU(cpu)); err != nil {
		return nil, err
	}
	if len(companions)+1 > cfg.Capacity.Policy.MaxActive {
		return nil, fmt.Errorf("Compose group exceeds container count budget")
	}
	spec, _ := json.Marshal(struct {
		Image    string
		Services []domain.Companion
	}{cfg.Image, companions})
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(spec))
	main.cfg.Labels = map[string]string{}
	for k, v := range cfg.Labels {
		main.cfg.Labels[k] = v
	}
	main.cfg.Labels["weft.compose"] = fingerprint
	main.cfg.Labels["weft.capacity-group"] = cfg.Name
	main.cfg.Network = cfg.Name + "-services"
	main.cfg.EnforceSwap = true
	main.cfg.Capacity = nil // group owner alone reserves/releases the aggregate
	ctx, cancel := context.WithCancel(context.Background())
	return &ComposeSandbox{egress: egress, main: main, companions: companions, budget: cfg.Capacity, memory: memory, cpu: cpu, helperMemory: hm, helperCPU: hc, fingerprint: fingerprint, network: main.cfg.Network, ctx: ctx, cancel: cancel}, nil
}
func (s *ComposeSandbox) Kind() string { return "docker" }
func (s *ComposeSandbox) names() []string {
	names := []string{s.main.cfg.Name}
	for _, c := range s.companions {
		names = append(names, s.main.cfg.Name+"-svc-"+c.Name)
	}
	return names
}
func missing(out string) bool {
	return strings.Contains(out, "No such container") || strings.Contains(out, "No such object") || strings.Contains(out, "No such network")
}
func (s *ComposeSandbox) owned(ctx context.Context, kind, name string) (bool, error) {
	template := `{{index .Config.Labels "weft.compose"}}`
	if kind == "network" {
		template = `{{index .Labels "weft.compose"}}`
	}
	args := []string{"inspect", "--format", template, name}
	if kind == "network" {
		args = append([]string{"network"}, args...)
	}
	out, err := s.main.docker(ctx, args...)
	if err != nil {
		if missing(out) || (kind == "network" && strings.Contains(out, "network "+name+" not found")) {
			return false, nil
		}
		return false, fmt.Errorf("inspect Compose %s: %w: %s", kind, err, out)
	}
	if strings.TrimSpace(out) != s.fingerprint {
		return false, fmt.Errorf("Compose %s %s belongs to another definition; stop the previous task before replacement", kind, name)
	}
	return true, nil
}
func (s *ComposeSandbox) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if s.started {
		for _, c := range s.companions {
			if out, err := s.main.docker(ctx, "inspect", "--format", "{{.State.Running}}", s.main.cfg.Name+"-svc-"+c.Name); err != nil || strings.TrimSpace(out) != "true" {
				return fmt.Errorf("companion %s is unavailable; suspend/resume or stop the task to recover (capacity retained): %v: %s", c.Name, err, out)
			}
		}
		return nil
	}
	if s.main.cfg.ValidateAdmission != nil {
		if err := s.main.cfg.ValidateAdmission(ctx); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	for _, name := range s.names() {
		if _, err := s.owned(ctx, "container", name); err != nil {
			return err
		}
	}
	networkExists, err := s.owned(ctx, "network", s.network)
	if err != nil {
		return err
	}
	if err := s.budget.ReserveGroup(ctx, s.main.cfg.Name, s.memory, s.cpu, len(s.companions)+1, s.main.inventory); err != nil {
		return err
	}
	if s.main.cfg.ValidateAdmission != nil {
		if err := s.main.cfg.ValidateAdmission(ctx); err != nil {
			return errors.Join(err, s.budget.Release(context.WithoutCancel(ctx), s.main.cfg.Name))
		}
	}
	if !networkExists {
		args := []string{"network", "create", "--internal", "--label", "weft.compose=" + s.fingerprint}
		if s.egress {
			args = append(args, "--opt", "com.docker.network.bridge.gateway_mode_ipv4=isolated", "--opt", "com.docker.network.bridge.gateway_mode_ipv6=isolated")
		}
		if out, e := s.main.docker(ctx, append(args, s.network)...); e != nil {
			return s.startFailed(ctx, fmt.Errorf("create internal network: %w: %s", e, out))
		}
	}
	for _, c := range s.companions {
		name := s.main.cfg.Name + "-svc-" + c.Name
		exists, e := s.owned(ctx, "container", name)
		if e != nil {
			return s.startFailed(ctx, e)
		}
		if exists {
			out, e := s.main.docker(ctx, "update", "--cpus", sandboxcapacity.FormatCPU(s.helperCPU), "--memory", strconv.FormatInt(s.helperMemory, 10), "--memory-swap", strconv.FormatInt(s.helperMemory, 10), name)
			if e != nil {
				return s.startFailed(ctx, fmt.Errorf("update companion: %w: %s", e, out))
			}
			out, e = s.main.docker(ctx, "start", name)
			if e != nil {
				return s.startFailed(ctx, fmt.Errorf("start companion: %w: %s", e, out))
			}
		} else {
			args := []string{"run", "-d", "--name", name, "--restart", "no", "--network", s.network, "--network-alias", c.Name, "--label", "weft.sandbox=true", "--label", "weft.capacity-group=" + s.main.cfg.Name, "--label", "weft.compose=" + s.fingerprint, "--cpus", sandboxcapacity.FormatCPU(s.helperCPU), "--memory", strconv.FormatInt(s.helperMemory, 10), "--memory-swap", strconv.FormatInt(s.helperMemory, 10), "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "10001:10001", "--workdir", "/tmp", c.Image}
			if s.egress && c.Name == "weft-egress" {
				args[len(args)-2] = "/" // scratch proxy needs no writable working directory
				args = args[:len(args)-1]
				args = append(args, "--read-only", "--pids-limit", "128", "--log-opt", "max-size=10m", "--log-opt", "max-file=2", "--label", "weft.egress=true", "--label", "weft.graph="+s.main.cfg.Labels["weft.graph"], "--label", "weft.egress.phase=runtime", c.Image)
			}
			args = append(args, c.Command...)
			if out, e := s.main.docker(ctx, args...); e != nil {
				return s.startFailed(ctx, fmt.Errorf("create companion %s: %w: %s", c.Name, e, out))
			}
		}
		if s.egress && c.Name == "weft-egress" {
			connected, e := s.main.docker(ctx, "inspect", "--format", `{{if index .NetworkSettings.Networks "bridge"}}true{{else}}false{{end}}`, name)
			if e != nil {
				return s.startFailed(ctx, fmt.Errorf("inspect proxy network: %w", e))
			}
			if strings.TrimSpace(connected) != "true" {
				if out, e := s.main.docker(ctx, "network", "connect", "bridge", name); e != nil {
					return s.startFailed(ctx, fmt.Errorf("connect proxy uplink: %w: %s", e, out))
				}
			}
		}
		if out, e := s.main.docker(ctx, "inspect", "--format", "{{.State.Running}}", name); e != nil || strings.TrimSpace(out) != "true" {
			return s.startFailed(ctx, fmt.Errorf("companion %s did not stay running: %v: %s", c.Name, e, out))
		}
	}
	if exists, e := s.owned(ctx, "container", s.main.cfg.Name); e != nil {
		return s.startFailed(ctx, e)
	} else if exists {
		if out, e := s.main.docker(ctx, "update", "--cpus", s.main.cfg.CPU, "--memory", s.main.cfg.Memory, "--memory-swap", s.main.cfg.Memory, s.main.cfg.Name); e != nil {
			return s.startFailed(ctx, fmt.Errorf("update development sandbox: %w: %s", e, out))
		}
	}
	if err := s.main.Start(ctx); err != nil {
		return s.startFailed(ctx, err)
	}
	s.started = true
	return nil
}

// Preserve writable layers on failures. Capacity is released only after all
// group processes are confirmed stopped; ambiguous cleanup retains the lease.
func (s *ComposeSandbox) startFailed(ctx context.Context, cause error) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	if err := s.stopAll(cleanup); err != nil {
		return errors.Join(cause, err)
	}
	return errors.Join(cause, s.budget.Release(cleanup, s.main.cfg.Name))
}
func (s *ComposeSandbox) stopAll(ctx context.Context) error {
	s.started = false
	for _, name := range s.names() {
		exists, err := s.owned(ctx, "container", name)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if out, err := s.main.docker(ctx, "stop", "--time", "10", name); err != nil {
			return fmt.Errorf("stop Compose group (capacity retained): %w: %s", err, out)
		}
		if out, err := s.main.docker(ctx, "inspect", "--format", "{{.State.Running}}", name); err != nil || strings.TrimSpace(out) != "false" {
			return fmt.Errorf("Compose stop not confirmed (capacity retained): %v: %s", err, out)
		}
	}
	s.main.started = false
	return nil
}
func (s *ComposeSandbox) Suspend(ctx context.Context) error {
	s.activity.Lock()
	defer s.activity.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.stopAll(ctx); err != nil {
		return err
	}
	return s.budget.Release(ctx, s.main.cfg.Name)
}
func (s *ComposeSandbox) Exec(ctx context.Context, req ExecRequest) (*ExecResult, error) {
	s.activity.RLock()
	defer s.activity.RUnlock()
	if err := s.Start(ctx); err != nil {
		return nil, err
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	return s.main.Exec(run, req)
}
func (s *ComposeSandbox) Close() error {
	s.cancel()
	s.main.stop()
	s.activity.Lock()
	defer s.activity.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if s.main.cfg.BeforeRemove != nil {
		if err := s.main.cfg.BeforeRemove(ctx); err != nil {
			return fmt.Errorf("capture proxy history before removal: %w", err)
		}
	}
	for _, name := range s.names() {
		exists, err := s.owned(ctx, "container", name)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		args := []string{"rm", "-f", name}
		if name != s.main.cfg.Name {
			args = []string{"rm", "-f", "-v", name}
		}
		if out, err := s.main.docker(ctx, args...); err != nil {
			return fmt.Errorf("remove Compose container (capacity retained): %w: %s", err, out)
		}
	}
	exists, err := s.owned(ctx, "network", s.network)
	if err != nil {
		return err
	}
	if exists {
		if out, err := s.main.docker(ctx, "network", "rm", s.network); err != nil {
			return fmt.Errorf("remove Compose network: %w: %s", err, out)
		}
	}
	s.started = false
	return s.budget.Release(ctx, s.main.cfg.Name)
}
