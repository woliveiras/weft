package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
)

// scriptedSandbox records the check request and returns a canned result.
// Fingerprint git probes see "not a repository".
type scriptedSandbox struct {
	kind string
	got  *ExecRequest
	res  *ExecResult
	err  error
}

func (s *scriptedSandbox) Kind() string {
	if s.kind != "" {
		return s.kind
	}
	return "scripted"
}
func (s *scriptedSandbox) Close() error { return nil }
func (s *scriptedSandbox) Exec(_ context.Context, req ExecRequest) (*ExecResult, error) {
	if len(req.Argv) > 0 && req.Argv[0] == "git" {
		return &ExecResult{ExitCode: 128, Stderr: "fatal: not a git repository"}, nil
	}
	s.got = &req
	return s.res, s.err
}

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetCounter().GetValue()
}

// checkWorkdir is a workspace with a "server" subdirectory.
var checkWorkdir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "check-run-")
	if err == nil {
		err = os.Mkdir(filepath.Join(dir, "server"), 0o755)
	}
	if err != nil {
		panic(err)
	}
	checkWorkdir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func checkInvocation(sb Sandbox, grant domain.ToolGrant, checks ...domain.Check) *Invocation {
	p := &Principal{GraphID: "g", NodeID: "dev", Workdir: checkWorkdir, Checks: checks}
	return &Invocation{Principal: p, Grant: grant, Sandbox: sb, Workdir: checkWorkdir, Args: json.RawMessage(`{"name":"test"}`)}
}

func TestCheckRunPassingCheck(t *testing.T) {
	sb := &scriptedSandbox{res: &ExecResult{ExitCode: 0, Stdout: "ok  \tx\t0.1s\n", Duration: 1200 * time.Millisecond}}
	before := counterValue(t, observability.CheckRuns.WithLabelValues("test", "passed"))
	res, err := checkTool{}.Invoke(context.Background(), checkInvocation(sb, domain.ToolGrant{Name: "shell.exec"},
		domain.Check{Name: "test", Command: "go test ./...", Cwd: "server", TimeoutSec: 30}))
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	if sb.got.Shell != "go test ./..." || sb.got.Dir != "server" || sb.got.Timeout != 30*time.Second || sb.got.MaxOutput != checkOutputHead || sb.got.TailOutput != checkOutputTail {
		t.Fatalf("request %+v", sb.got)
	}
	var run CheckRun
	if err := json.Unmarshal(res.Data, &run); err != nil || !run.Passed || run.Check != "test" || run.DurationMs != 1200 || len(run.Failures) != 0 {
		t.Fatalf("%v %+v", err, run)
	}
	if !run.Workspace.Partial || run.Workspace.Head != "" {
		t.Fatalf("outside a repository the fingerprint is partial: %+v", run.Workspace)
	}
	if res.Content != `check "test" passed (exit 0, 1.2s)` {
		t.Fatalf("content %q", res.Content)
	}
	if got := counterValue(t, observability.CheckRuns.WithLabelValues("test", "passed")); got != before+1 {
		t.Fatalf("metric %v -> %v", before, got)
	}
}

func TestCheckRunFailingCheckReturnsParsedFailures(t *testing.T) {
	sb := &scriptedSandbox{res: &ExecResult{ExitCode: 1, Stdout: "--- FAIL: TestAdd (0.00s)\n    calc_test.go:9: got 4, want 3\nFAIL\tx\t0.1s\n", Truncated: true}}
	res, err := checkTool{}.Invoke(context.Background(), checkInvocation(sb, domain.ToolGrant{Name: "shell.exec"}, domain.Check{Name: "test", Command: "go test ./..."}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal("a failing check is a successful tool call")
	}
	if sb.got.Timeout != defaultCheckTimeout {
		t.Fatalf("default timeout %v", sb.got.Timeout)
	}
	for _, want := range []string{`check "test" failed (exit 1`, "1 failure(s) [go]", "- TestAdd calc_test.go:9: got 4, want 3", "output was truncated"} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("content lacks %q:\n%s", want, res.Content)
		}
	}
	var run CheckRun
	_ = json.Unmarshal(res.Data, &run)
	if run.Passed || run.Format != FormatGo || run.FailureCount != 1 || run.Failures[0].Line != 9 {
		t.Fatalf("%+v", run)
	}
}

func TestCheckRunTimeoutIsAFailure(t *testing.T) {
	sb := &scriptedSandbox{res: &ExecResult{ExitCode: 0, TimedOut: true, Stdout: "still running\n"}}
	before := counterValue(t, observability.CheckRuns.WithLabelValues("custom", "timed_out"))
	inv := checkInvocation(sb, domain.ToolGrant{Name: "shell.exec"}, domain.Check{Name: "e2e", Command: "npm run e2e"})
	inv.Args = json.RawMessage(`{"name":"e2e"}`)
	res, err := checkTool{}.Invoke(context.Background(), inv)
	if err != nil || !strings.Contains(res.Content, "failed") || !strings.Contains(res.Content, "timed out") {
		t.Fatalf("%v %q", err, res.Content)
	}
	if got := counterValue(t, observability.CheckRuns.WithLabelValues("custom", "timed_out")); got != before+1 {
		t.Fatal("custom checks are counted under the bounded kind label")
	}
}

func TestCheckRunDockerExecFailureIsAnInfrastructureError(t *testing.T) {
	sb := &scriptedSandbox{kind: "docker", res: &ExecResult{ExitCode: 125, Stderr: "Error response from daemon: container is not running"}}
	before := counterValue(t, observability.CheckRuns.WithLabelValues("test", "infra_error"))
	res, err := checkTool{}.Invoke(context.Background(), checkInvocation(sb, domain.ToolGrant{Name: "shell.exec"}, domain.Check{Name: "test", Command: "go test ./..."}))
	if err != nil {
		t.Fatal(err)
	}
	var run CheckRun
	_ = json.Unmarshal(res.Data, &run)
	if run.Passed || !run.InfraError || run.Format != "" || !strings.Contains(run.Failures[0].Message, "container is not running") {
		t.Fatalf("%+v", run)
	}
	if !strings.Contains(res.Content, `check "test" could not run`) {
		t.Fatalf("content %q", res.Content)
	}
	if counterValue(t, observability.CheckRuns.WithLabelValues("test", "infra_error")) != before+1 {
		t.Fatal("infrastructure errors are counted apart from failures")
	}
	// The same exit status from a process sandbox is the command's own.
	sb.kind = "process"
	res, _ = checkTool{}.Invoke(context.Background(), checkInvocation(sb, domain.ToolGrant{Name: "shell.exec"}, domain.Check{Name: "test", Command: "go test ./..."}))
	run = CheckRun{}
	_ = json.Unmarshal(res.Data, &run)
	if run.InfraError {
		t.Fatal("exit 125 outside docker is a check failure")
	}
}

func TestCheckRunErrors(t *testing.T) {
	ctx := context.Background()
	sb := &scriptedSandbox{res: &ExecResult{}}
	shell := domain.ToolGrant{Name: "shell.exec"}

	if _, err := (checkTool{}).Invoke(ctx, checkInvocation(sb, shell)); !errors.Is(err, errUnknownCheck) || !strings.Contains(err.Error(), "declares no checks") {
		t.Fatalf("no checks: %v", err)
	}
	inv := checkInvocation(sb, shell, domain.Check{Name: "lint", Command: "x"}, domain.Check{Name: "build", Command: "y"})
	if _, err := (checkTool{}).Invoke(ctx, inv); !errors.Is(err, errUnknownCheck) || !strings.Contains(err.Error(), "build, lint") {
		t.Fatalf("unknown check: %v", err)
	}
	inv.Args = json.RawMessage(`{"name":1}`)
	if _, err := (checkTool{}).Invoke(ctx, inv); err == nil {
		t.Fatal("invalid arguments must fail")
	}
	restricted := domain.ToolGrant{Name: "shell.exec", Commands: []string{"go"}}
	if _, err := (checkTool{}).Invoke(ctx, checkInvocation(sb, restricted, domain.Check{Name: "test", Command: "npm test"})); !errors.Is(err, ErrDenied) {
		t.Fatalf("a check outside the command grant is denied: %v", err)
	}
	if _, err := (checkTool{}).Invoke(ctx, checkInvocation(sb, restricted, domain.Check{Name: "test", Command: "go test ./... && rm -rf x"})); !errors.Is(err, ErrDenied) {
		t.Fatalf("shell operators under a restricted grant are denied: %v", err)
	}
	sb.got = nil
	if _, err := (checkTool{}).Invoke(ctx, checkInvocation(sb, restricted, domain.Check{Name: "test", Command: "go test ./..."})); err != nil || sb.got.Shell != "" || strings.Join(sb.got.Argv, " ") != "go test ./..." {
		t.Fatalf("restricted grant runs the checked argv without a shell: %v %+v", err, sb.got)
	}
	if _, err := (checkTool{}).Invoke(ctx, checkInvocation(sb, shell, domain.Check{Name: "test", Command: "x", Cwd: "../outside"})); err == nil {
		t.Fatal("a cwd outside the workspace must fail")
	}
	failing := &scriptedSandbox{err: errors.New("container gone")}
	if _, err := (checkTool{}).Invoke(ctx, checkInvocation(failing, shell, domain.Check{Name: "test", Command: "x"})); err == nil || !strings.Contains(err.Error(), "container gone") {
		t.Fatalf("sandbox failure propagates: %v", err)
	}
}

func TestCheckRunSchemaListsDeclaredChecks(t *testing.T) {
	reg := Builtin(BuiltinConfig{})
	p := &Principal{Grants: []domain.ToolGrant{{Name: "shell.exec"}}}
	for _, s := range reg.Schemas(p) {
		if s.Name == CheckToolName {
			t.Fatal("check.run is hidden when no checks are declared")
		}
	}
	p.Checks = []domain.Check{{Name: "test", Command: "go test ./..."}, {Name: "lint", Command: "go vet ./..."}}
	var found bool
	for _, s := range reg.Schemas(p) {
		if s.Name == CheckToolName {
			found = true
			if !strings.Contains(string(s.Parameters), `"enum":["test","lint"]`) {
				t.Fatalf("schema %s", s.Parameters)
			}
		}
	}
	if !found {
		t.Fatal("check.run is offered with the shell.exec grant and declared checks")
	}
	p.Grants = []domain.ToolGrant{{Name: "filesystem.read"}}
	for _, s := range reg.Schemas(p) {
		if s.Name == CheckToolName {
			t.Fatal("check.run needs the shell.exec grant")
		}
	}
}

// TestCheckRunThroughGatewayWithRealGoChecks runs a passing and a failing
// Go test check in a process sandbox through the gateway, and checks the
// recorded task evidence and the authorization path.
func TestCheckRunThroughGatewayWithRealGoChecks(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("this integration test needs the Go toolchain on PATH: %v", err)
	}
	cache, err := exec.Command(goBin, "env", "GOCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/calc\n\ngo 1.22\n")
	write("calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	write("calc_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n")
	write("broken/broken_test.go", "package broken\n\nimport \"testing\"\n\nfunc TestBroken(t *testing.T) {\n\tt.Errorf(\"got %d, want %d\", 4, 3)\n}\n")

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test"}, args...)...)
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	// The process sandbox's HOME is the workspace: Go's own files there are
	// output, ignored like any build output a check produces.
	write(".gitignore", "/go/\n/.config/\n/.cache/\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "base")
	base := git("rev-parse", "HEAD")

	env := "GOCACHE=" + strings.TrimSpace(string(cache)) + " GOFLAGS=-mod=mod GOTOOLCHAIN=local "
	aud := &captureAuditor{}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, aud)
	p := principal(dir, domain.ToolGrant{Name: "shell.exec"})
	p.Sandbox = &ProcessSandbox{Root: dir, Path: filepath.Dir(goBin) + ":/usr/local/bin:/usr/bin:/bin"}
	p.BaseCommit = base
	p.Checks = []domain.Check{
		{Name: "test", Command: env + "go test -count=1 .", TimeoutSec: 300},
		{Name: "broken", Command: env + "go test -count=1 ./broken/", TimeoutSec: 300},
	}
	ctx := context.Background()

	res, err := gw.Invoke(ctx, p, CheckToolName, json.RawMessage(`{"name":"test"}`))
	if err != nil || !strings.HasPrefix(res.Content, `check "test" passed (exit 0`) {
		t.Fatalf("passing check: %v %+v", err, res)
	}
	res, err = gw.Invoke(ctx, p, CheckToolName, json.RawMessage(`{"name":"broken"}`))
	if err != nil || res.IsError {
		t.Fatalf("failing check: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, `check "broken" failed (exit 1`) || !strings.Contains(res.Content, "- TestBroken broken_test.go:6: got 4, want 3") {
		t.Fatalf("failing check content:\n%s", res.Content)
	}

	var evidence []CheckRun
	for _, ev := range aud.evs {
		if ev.Type != domain.EvCheckCompleted {
			continue
		}
		if ev.GraphID != "g" || ev.NodeID != "dev" || ev.ExecutionID != "exe_1" {
			t.Fatalf("evidence attribution %+v", ev)
		}
		var run CheckRun
		if err := json.Unmarshal(ev.Data, &run); err != nil {
			t.Fatal(err)
		}
		evidence = append(evidence, run)
	}
	if len(evidence) != 2 || !evidence[0].Passed || evidence[1].Passed || evidence[1].Failures[0].Test != "TestBroken" {
		t.Fatalf("evidence %+v", evidence)
	}
	clean := evidence[0].Workspace
	if clean.Head != base || clean.State == "" || clean.Partial || len(clean.CheckFilesChanged) != 0 || evidence[1].Workspace.State != clean.State {
		t.Fatalf("clean workspace fingerprint %+v / %+v", clean, evidence[1].Workspace)
	}

	// An agent that rewrites a test still gets a pass, but the evidence
	// shows a different tree and names the changed test file.
	write("calc_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {}\n")
	write("extra_test.go", "package calc\n")
	res, err = gw.Invoke(ctx, p, CheckToolName, json.RawMessage(`{"name":"test"}`))
	if err != nil || !strings.Contains(res.Content, "note: test or check-definition files changed in this task: calc_test.go, extra_test.go") {
		t.Fatalf("rewritten test: %v %+v", err, res)
	}
	var rewritten CheckRun
	_ = json.Unmarshal(res.Data, &rewritten)
	if !rewritten.Passed || rewritten.Workspace.State == clean.State || rewritten.Workspace.Head != base || rewritten.Workspace.Base != base {
		t.Fatalf("rewritten fingerprint %+v", rewritten.Workspace)
	}
	// Committing the rewrite does not hide it: the comparison is with the base.
	git("add", "-A")
	git("commit", "-qm", "rewrite")
	res, _ = gw.Invoke(ctx, p, CheckToolName, json.RawMessage(`{"name":"test"}`))
	_ = json.Unmarshal(res.Data, &rewritten)
	if rewritten.Workspace.Head == base || strings.Join(rewritten.Workspace.CheckFilesChanged, ",") != "calc_test.go,extra_test.go" {
		t.Fatalf("committed rewrite %+v", rewritten.Workspace)
	}

	// A check is no new privilege: a restricted grant still decides.
	n := len(aud.evs)
	p.Grants = []domain.ToolGrant{{Name: "shell.exec", Commands: []string{"npm"}}}
	if _, err := gw.Invoke(ctx, p, CheckToolName, json.RawMessage(`{"name":"test"}`)); !errors.Is(err, ErrDenied) {
		t.Fatalf("restricted grant: %v", err)
	}
	p.Grants = []domain.ToolGrant{{Name: "filesystem.read"}}
	if _, err := gw.Invoke(ctx, p, CheckToolName, json.RawMessage(`{"name":"test"}`)); !errors.Is(err, ErrDenied) {
		t.Fatalf("no shell grant: %v", err)
	}
	for _, ev := range aud.evs[n:] {
		if ev.Type == domain.EvCheckCompleted {
			t.Fatal("denied checks are not recorded as evidence")
		}
	}
}

func TestCheckRunNeedsTheShellGrantSharesItsRateLimitAndShowsTheCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sb := &scriptedSandbox{res: &ExecResult{}}
	checks := []domain.Check{{Name: "test", Command: "go test ./...", Cwd: "", TimeoutSec: 90}}
	newPrincipal := func(g ...domain.ToolGrant) *Principal {
		p := principal(dir, g...)
		p.Sandbox, p.Checks = sb, checks
		return p
	}
	// A grant naming the tool is not a shell grant.
	for _, name := range []string{"check.run", "check.*"} {
		gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
		if _, err := gw.Invoke(ctx, newPrincipal(domain.ToolGrant{Name: name}), CheckToolName, json.RawMessage(`{"name":"test"}`)); !errors.Is(err, ErrDenied) {
			t.Fatalf("grant %q must not authorize check.run: %v", name, err)
		}
		if len(Builtin(BuiltinConfig{}).Schemas(newPrincipal(domain.ToolGrant{Name: name}))) != 0 {
			t.Fatalf("grant %q must not offer check.run", name)
		}
	}
	for _, name := range []string{"shell.exec", "shell.*", "*"} {
		gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
		if _, err := gw.Invoke(ctx, newPrincipal(domain.ToolGrant{Name: name}), CheckToolName, json.RawMessage(`{"name":"test"}`)); err != nil {
			t.Fatalf("grant %q: %v", name, err)
		}
	}

	// check.run spends shell.exec's allowance instead of adding its own.
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	limited := newPrincipal(domain.ToolGrant{Name: "shell.exec", RateLimit: 1})
	if _, err := gw.Invoke(ctx, limited, "shell.exec", json.RawMessage(`{"command":"true"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.Invoke(ctx, limited, CheckToolName, json.RawMessage(`{"name":"test"}`)); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("shared rate limit: %v", err)
	}

	// Approvals and the audit show the resolved command, cwd and timeout.
	approver := &fakeApprover{status: domain.ApprovalApproved}
	aud := &captureAuditor{}
	gw = NewGateway(Builtin(BuiltinConfig{}), approver, aud)
	if _, err := gw.Invoke(ctx, newPrincipal(domain.ToolGrant{Name: "shell.exec", RequiresApproval: true}), CheckToolName, json.RawMessage(`{"name":"test"}`)); err != nil {
		t.Fatal(err)
	}
	want := `"command":"go test ./..."`
	if len(approver.got) != 1 || !strings.Contains(string(approver.got[0].Detail), want) || !strings.Contains(string(approver.got[0].Detail), `"timeoutSec":90`) {
		t.Fatalf("approval detail %s", approver.got[0].Detail)
	}
	for _, ev := range aud.evs {
		if ev.Type == domain.EvToolInvoked && !strings.Contains(string(ev.Data), want) {
			t.Fatalf("invoked audit lacks the command: %s", ev.Data)
		}
	}
	approver.status = domain.ApprovalRejected
	aud.evs = nil
	if _, err := gw.Invoke(ctx, newPrincipal(domain.ToolGrant{Name: "shell.exec", RequiresApproval: true}), CheckToolName, json.RawMessage(`{"name":"test"}`)); err == nil {
		t.Fatal("rejected approval must fail")
	}
	if len(aud.evs) != 1 || aud.evs[0].Type != domain.EvToolDenied || !strings.Contains(string(aud.evs[0].Data), want) {
		t.Fatalf("denied audit %v", aud.types())
	}
}

func TestCheckEvidenceThatCannotBeDecodedIsCounted(t *testing.T) {
	aud := &captureAuditor{}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, aud)
	before := counterValue(t, observability.CheckEvidenceDropped)
	gw.auditCheck(context.Background(), principal(t.TempDir()), &Result{Data: json.RawMessage(`{"check":1}`)})
	if len(aud.evs) != 0 || counterValue(t, observability.CheckEvidenceDropped) != before+1 {
		t.Fatalf("dropped evidence must be counted, not recorded: %v", aud.types())
	}
}
