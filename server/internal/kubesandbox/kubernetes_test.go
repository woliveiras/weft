//go:build kubernetes_e2e

package kubesandbox

import (
	"context"

	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/tools"
)

func TestLiveReplicaPods(t *testing.T) {
	root := os.Getenv("WEFT_FIXTURE_WORKSPACES")
	if root == "" {
		t.Fatal("set WEFT_FIXTURE_WORKSPACES to the mounted fixture claim")
	}
	ctx := t.Context()
	create := func(index int, ro bool) *Sandbox {
		dir, err := os.MkdirTemp(root, "worktree-")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "identity"), []byte(filepath.Base(dir)), 0644); err != nil {
			t.Fatal(err)
		}
		s, err := New(Replica{Root: root, Workdir: dir, Graph: "fixture", Node: "check", Index: index, Image: "weft/sandbox-go:dev", ReadOnly: ro})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		})
		return s
	}
	a, b := create(0, false), create(1, true)
	exec := func(s *Sandbox, argv ...string) *tools.ExecResult {
		t.Helper()
		r, err := s.Exec(ctx, tools.ExecRequest{Argv: argv})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	var workers sync.WaitGroup
	for _, sb := range []*Sandbox{a, b} {
		workers.Go(func() {
			r, err := sb.Exec(ctx, tools.ExecRequest{Argv: []string{"cat", "identity"}})
			if err != nil || r.Stdout != filepath.Base(sb.workdir) {
				t.Errorf("workspace mismatch: %v %v", r, err)
			}
		})
	}
	workers.Wait()
	if t.Failed() {
		return
	}
	if a.pod.UID == b.pod.UID {
		t.Fatal("replicas share a pod")
	}
	// Resolve once in the controller so a DNS failure cannot satisfy the
	// negative probe. Both clients use the very same numeric destination.
	ips, err := net.DefaultResolver.LookupHost(ctx, "weft-agents")
	if err != nil || len(ips) == 0 {
		t.Fatal(ips, err)
	}
	target := "http://" + net.JoinHostPort(ips[0], "8000") + "/v1/health"
	control, err := (&http.Client{Timeout: 3 * time.Second}).Get(target)
	if err != nil {
		t.Fatal(err)
	}
	control.Body.Close()
	if control.StatusCode != http.StatusOK {
		t.Fatal(control.Status)
	}
	if r := exec(a, "wget", "-T", "2", "-O", "/dev/null", target); r.ExitCode == 0 || !strings.Contains(r.Stderr, "timed out") {
		t.Fatalf("expected a blocked connection, got %+v", r)
	}
	if r := exec(b, "touch", "new-file"); r.ExitCode == 0 {
		t.Fatal("read-only write succeeded")
	}
	if r := exec(a, "test", "-e", "/var/run/secrets/kubernetes.io/serviceaccount/token"); r.ExitCode != 1 {
		t.Fatal("unexpected service account mount", r)
	}
	if r := exec(a, "test", "-e", filepath.Join("/workspace", "..", filepath.Base(b.workdir), "identity")); r.ExitCode != 1 {
		t.Fatal("sibling workspace visible", r)
	}
	if r := exec(a, "touch", "persisted"); r.ExitCode != 0 {
		t.Fatal(r)
	}
	uid := a.pod.UID
	r, err := a.Exec(ctx, tools.ExecRequest{Argv: []string{"sleep", "20"}, Timeout: 100 * time.Millisecond})
	if err != nil || !r.TimedOut || r.ExitCode == 0 {
		t.Fatal(r, err)
	}
	if r := exec(a, "test", "-f", "persisted"); r.ExitCode != 0 || a.pod.UID == uid {
		t.Fatal("timeout did not renew the pod with the same workspace", r)
	}
	run, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	_, err = a.Exec(run, tools.ExecRequest{Argv: []string{"sleep", "20"}})
	cancel()
	if err == nil || a.pod != nil {
		t.Fatal("cancelled sandbox was reused", err)
	}
	// Delete a live pod after the command has actually started.
	if r := exec(a, "test", "-f", "persisted"); r.ExitCode != 0 {
		t.Fatal(r)
	}
	current := a.pod.DeepCopy()
	finished := make(chan error, 1)
	go func() {
		_, err := a.Exec(ctx, tools.ExecRequest{Shell: "touch command-started && sleep 20"})
		finished <- err
	}()
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, err := os.Stat(filepath.Join(a.workdir, "command-started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command never started")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := a.remove(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err == nil {
		t.Fatal("lost exec returned success")
	}
	if r := exec(a, "test", "-f", "persisted"); r.ExitCode != 0 || a.pod.UID == current.UID {
		t.Fatal("pod loss failed to retain the workspace")
	}
	if r := exec(b, "cat", "identity"); r.ExitCode != 0 || strings.TrimSpace(r.Stdout) != filepath.Base(b.workdir) {
		t.Fatal("workspace lost during pod recovery")
	}
	// Simulate two claims backed by different directories while retaining
	// valid pod admission. The submitted command must never reach the pod.
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	wrong := create(2, false)
	wrong.template.Spec.Containers[0].VolumeMounts[0].SubPath = b.template.Spec.Containers[0].VolumeMounts[0].SubPath
	_, err = wrong.Exec(ctx, tools.ExecRequest{Argv: []string{"touch", "wrong-command"}})
	if err == nil || !strings.Contains(err.Error(), "does not expose") {
		t.Fatal("accepted mismatched backing", err)
	}
	if _, err := os.Stat(filepath.Join(b.workdir, "wrong-command")); !os.IsNotExist(err) {
		t.Fatal("command ran against wrong backing", err)
	}

}
