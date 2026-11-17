package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/tools"
)

// DockerDriver tunnels only a registered application's loopback port through
// docker exec stdio. No application port or general network proxy is published.
type DockerDriver struct {
	Bin, Host, RelayBinary string
	Capacity               *sandboxcapacity.Manager
	StartSandbox           func(context.Context, *Record, string, string) (tools.Sandbox, error)
}

func (d *DockerDriver) cmd(ctx context.Context, args ...string) *exec.Cmd {
	bin := d.Bin
	if bin == "" {
		bin = "docker"
	}
	return tools.DockerCommand(ctx, bin, d.Host, args...)
}
func (d *DockerDriver) Start(ctx context.Context, r *Record) error {
	// A controller restart reconnects to the exact existing generation.
	out, e := d.cmd(ctx, "inspect", "--format", "{{.State.Running}}", r.Name()).CombinedOutput()
	if e == nil && strings.TrimSpace(string(out)) == "true" {
		return d.waitReady(ctx, r)
	}
	if e != nil && !strings.Contains(string(out), "No such") {
		return fmt.Errorf("inspect preview: %w", e)
	}
	work := r.Work()
	if _, e = os.Stat(work); errors.Is(e, os.ErrNotExist) {
		if _, e = Candidate(ctx, filepath.Join(r.Directory, "candidate"), work); e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	control := filepath.Join(r.Directory, "control")
	if e = os.MkdirAll(control, 0755); e != nil {
		return e
	}
	dest := filepath.Join(control, "weft-preview")
	if _, e = os.Stat(dest); errors.Is(e, os.ErrNotExist) {
		f, e := os.Open(d.RelayBinary)
		if e != nil {
			return fmt.Errorf("preview relay binary: %w", e)
		}
		defer f.Close()
		out, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0555)
		if e != nil {
			return e
		}
		_, copyErr := io.Copy(out, f)
		closeErr := out.Close()
		if e = errors.Join(copyErr, closeErr); e != nil {
			return e
		}
	}
	config, _ := json.Marshal(map[string]any{"command": r.Request.Command, "cwd": filepath.ToSlash(filepath.Join("/workspace", r.Request.Cwd))})
	if e = WriteControl(control, config); e != nil {
		return e
	}
	if _, e = d.StartSandbox(ctx, r, work, control); e != nil {
		return e
	}
	return d.waitReady(ctx, r)
}
func (d *DockerDriver) waitReady(ctx context.Context, r *Record) error {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		if e := d.Ready(ctx, r); e == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("preview readiness: %w", ctx.Err())
		case <-t.C:
		}
	}
}
func (d *DockerDriver) Ready(ctx context.Context, r *Record) error {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return d.Dial(ctx, r) }, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, "GET", "http://preview"+r.Request.ReadyPath, nil)
	if e != nil {
		return e
	}
	res, e := client.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 400 {
		return fmt.Errorf("readiness returned HTTP %d", res.StatusCode)
	}
	return nil
}
func (d *DockerDriver) Stop(ctx context.Context, r *Record) error {
	// Inspect exact names rather than accepting targets from the HTTP caller.
	names := []string{r.Name()}
	if r.Graph.Spec.Environment != nil {
		for _, c := range r.Graph.Spec.Environment.Companions {
			names = append(names, r.Name()+"-svc-"+c.Name)
		}
		if r.Graph.Spec.Environment.Network.Run == "egress" {
			names = append(names, r.Name()+"-svc-weft-egress")
		}
	}
	for _, name := range names {
		out, e := d.cmd(ctx, "inspect", "--format", "{{.State.Running}}", name).CombinedOutput()
		if e != nil {
			if strings.Contains(string(out), "No such") {
				continue
			}
			return fmt.Errorf("inspect preview stop: %w", e)
		}
		if strings.TrimSpace(string(out)) == "true" {
			if out, e = d.cmd(ctx, "stop", "--time", "5", name).CombinedOutput(); e != nil {
				return fmt.Errorf("stop preview: %w: %s", e, out)
			}
		}
		out, e = d.cmd(ctx, "inspect", "--format", "{{.State.Running}}", name).CombinedOutput()
		if e != nil || strings.TrimSpace(string(out)) != "false" {
			return fmt.Errorf("preview stop unconfirmed; capacity retained")
		}
	}
	if d.Capacity != nil {
		return d.Capacity.Release(ctx, r.Name())
	}
	return nil
}
func (d *DockerDriver) Logs(ctx context.Context, r *Record) (string, error) {
	out, e := d.cmd(ctx, "logs", "--tail", "100", r.Name()).CombinedOutput()
	if len(out) > 64<<10 {
		out = out[len(out)-(64<<10):]
	}
	return string(out), e
}
func (d *DockerDriver) Dial(ctx context.Context, r *Record) (net.Conn, error) {
	lifetime, cancel := context.WithCancel(ctx)
	cmd := d.cmd(lifetime, "exec", "-i", r.Name(), "/weft-preview/weft-preview", "connect", strconv.Itoa(r.Request.Port))
	in, e := cmd.StdinPipe()
	if e != nil {
		cancel()
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		cancel()
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		cancel()
		return nil, e
	}
	// net.Pipe gives the transport genuine deadline support, including upgraded
	// WebSocket streams; closing it cancels the CLI and its container-side relay.
	client, server := net.Pipe()
	go func() {
		defer cancel()
		defer server.Close()
		defer cmd.Wait()
		done := make(chan struct{})
		go func() { defer close(done); _, _ = io.Copy(in, server); _ = in.Close() }()
		_, _ = io.Copy(server, out)
		_ = server.Close()
		<-done
	}()
	return &relayConn{Conn: client, cancel: cancel}, nil
}

type relayConn struct {
	net.Conn
	cancel context.CancelFunc
	once   sync.Once
}

func (c *relayConn) Close() error { c.once.Do(c.cancel); return c.Conn.Close() }

// WriteControl replaces the host-owned config atomically. The container sees a
// read-only mount; repeated starts must also work for a non-root controller.
func WriteControl(directory string, body []byte) error {
	f, err := os.CreateTemp(directory, ".config-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(0444); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(directory, "config.json"))
}
