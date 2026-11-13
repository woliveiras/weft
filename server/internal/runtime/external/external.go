// Package external runs turns in agent processes that speak Runtime
// Protocol v1 over HTTP (docs/runtime-protocol.md). This is how LangGraph,
// AutoGen, CrewAI or hand-written agents in any language join a graph.
//
// The protocol is deliberately request/response per turn and stateless:
// the engine sends TurnInput, the agent answers TurnResult, and in between
// the agent can only reach the platform through a callback URL with a
// token scoped to that one execution. Consequences:
//
//   - The agent process holds no credentials: model keys stay in the LLM
//     gateway, tool permissions are enforced by the tool gateway.
//   - Any number of replicas can share one agent process (or a pool behind
//     a load balancer), because nothing is kept between turns.
//   - The agent cannot claim, ack or route messages itself - the engine
//     does, so delivery guarantees do not depend on agent code being right.
//
// Where the agent runs is decided by the node's isolation mode:
//
//	endpoint set   → an already-running service (compose service, k8s Service)
//	process        → `command` launched as a child process on a free port
//	docker         → `image` launched as a container on the platform network
//	kubernetes     → endpoint required (render manifests with `weft k8s`)
//
// Launched runtimes are shared per node and reference-counted by replicas.
package external

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/propagation"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/runtime"
)

type Options struct {
	// SharedSecret is sent as X-Weft-Runtime-Secret on every call.
	SharedSecret string
	// DockerBin and DockerNetwork are used to launch image-based runtimes.
	DockerBin     string
	DockerNetwork string
	// StartTimeout bounds waiting for a launched runtime to become healthy.
	StartTimeout time.Duration
	// CallbackURL is the platform's internal URL passed to launched runtimes
	// as WEFT_URL (informational; each turn carries its own callback).
	CallbackURL string
}

type Runtime struct {
	opt    Options
	client *http.Client

	mu       sync.Mutex
	launched map[string]*instance // key graph/node
}

type instance struct {
	endpoint string
	refs     int
	stop     func()
}

func New(opt Options) *Runtime {
	if opt.DockerBin == "" {
		opt.DockerBin = "docker"
	}
	if opt.StartTimeout == 0 {
		opt.StartTimeout = 90 * time.Second
	}
	return &Runtime{opt: opt, client: &http.Client{}, launched: map[string]*instance{}}
}

func (r *Runtime) Kind() domain.RuntimeKind { return domain.RuntimeExternal }

func key(rc runtime.ReplicaContext) string { return rc.Graph.Metadata.ID + "/" + rc.Node.ID }

func (r *Runtime) Prepare(ctx context.Context, rc runtime.ReplicaContext) error {
	n := rc.Node
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(rc)
	if inst := r.launched[k]; inst != nil {
		inst.refs++
		return nil
	}
	var inst *instance
	var err error
	switch {
	case n.Runtime.Endpoint != "":
		// An already-running service: nothing to launch, just check it is up.
		inst = &instance{endpoint: n.Runtime.Endpoint, stop: func() {}}
	case n.Isolation.Mode == domain.IsolationKubernetes:
		return errors.New("kubernetes isolation needs runtime.endpoint (deploy the agent with the manifests from `weft k8s render`)")
	case n.Isolation.Mode == domain.IsolationDocker && n.Runtime.Image != "":
		inst, err = r.launchDocker(ctx, rc)
	case len(n.Runtime.Command) > 0:
		inst, err = r.launchProcess(ctx, rc)
	default:
		return errors.New("external runtime needs endpoint, command (process isolation) or image (docker isolation)")
	}
	if err != nil {
		return err
	}
	if err := r.waitHealthy(ctx, inst.endpoint, r.opt.StartTimeout); err != nil {
		inst.stop()
		return err
	}
	inst.refs = 1
	r.launched[k] = inst
	return nil
}

func (r *Runtime) Release(ctx context.Context, rc runtime.ReplicaContext) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(rc)
	inst := r.launched[k]
	if inst == nil {
		return nil
	}
	inst.refs--
	if inst.refs <= 0 {
		inst.stop()
		delete(r.launched, k)
	}
	return nil
}

func (r *Runtime) endpoint(in *runtime.TurnInput) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if inst := r.launched[in.GraphID+"/"+in.NodeID]; inst != nil {
		return inst.endpoint
	}
	return ""
}

func (r *Runtime) Turn(ctx context.Context, in *runtime.TurnInput, env runtime.TurnEnv) (*runtime.TurnResult, error) {
	ep := r.endpoint(in)
	if ep == "" {
		return nil, errors.New("external runtime has no endpoint")
	}
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(ep, "/")+"/v1/turn", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if r.opt.SharedSecret != "" {
		req.Header.Set("X-Weft-Runtime-Secret", r.opt.SharedSecret)
	}
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(req.Header))
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("runtime %s: %w", ep, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("runtime %s: HTTP %d: %s", ep, resp.StatusCode, truncate(string(data), 500))
	}
	var res runtime.TurnResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("runtime %s: invalid TurnResult: %w", ep, err)
	}
	return &res, nil
}

func (r *Runtime) waitHealthy(ctx context.Context, ep string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(ep, "/")+"/v1/health", nil)
		c := &http.Client{Timeout: 3 * time.Second}
		resp, err := c.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
			last = fmt.Errorf("health: HTTP %d", resp.StatusCode)
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("runtime at %s not healthy after %s: %v", ep, timeout, last)
}

func (r *Runtime) launchProcess(ctx context.Context, rc runtime.ReplicaContext) (*instance, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	n := rc.Node
	pctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(pctx, n.Runtime.Command[0], n.Runtime.Command[1:]...)
	cmd.Dir = rc.Workdir
	// The agent process gets a minimal environment: it needs no secrets,
	// because models and tools are reached through the platform.
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + rc.Workdir, "LANG=C.UTF-8",
		fmt.Sprintf("PORT=%d", port), "HOST=127.0.0.1",
		"WEFT_URL=" + r.opt.CallbackURL, "WEFT_RUNTIME_SECRET=" + r.opt.SharedSecret,
		"PYTHONUNBUFFERED=1",
	}
	for k, v := range n.Runtime.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("launch %v: %w", n.Runtime.Command, err)
	}
	go func() { _ = cmd.Wait() }()
	return &instance{endpoint: fmt.Sprintf("http://127.0.0.1:%d", port), stop: cancel}, nil
}

func (r *Runtime) launchDocker(ctx context.Context, rc runtime.ReplicaContext) (*instance, error) {
	n := rc.Node
	name := "weft-rt-" + rc.Graph.Metadata.ID + "-" + n.ID
	_ = exec.Command(r.opt.DockerBin, "rm", "-f", name).Run()
	args := []string{"run", "-d", "--name", name, "--label", "weft.graph=" + rc.Graph.Metadata.ID, "--label", "weft.node=" + n.ID,
		"-e", "PORT=8000", "-e", "WEFT_URL=" + r.opt.CallbackURL, "-e", "WEFT_RUNTIME_SECRET=" + r.opt.SharedSecret,
		"--security-opt", "no-new-privileges", "--cap-drop", "ALL"}
	if r.opt.DockerNetwork != "" {
		args = append(args, "--network", r.opt.DockerNetwork)
	}
	if n.Isolation.CPU != "" {
		args = append(args, "--cpus", n.Isolation.CPU)
	}
	if n.Isolation.Memory != "" {
		args = append(args, "--memory", strings.TrimSuffix(strings.TrimSuffix(n.Isolation.Memory, "i"), "B"))
	}
	for k, v := range n.Runtime.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, n.Runtime.Image)
	out, err := exec.CommandContext(ctx, r.opt.DockerBin, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker run %s: %v: %s", n.Runtime.Image, err, out)
	}
	return &instance{endpoint: "http://" + name + ":8000", stop: func() {
		_ = exec.Command(r.opt.DockerBin, "rm", "-f", name).Run()
	}}, nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
