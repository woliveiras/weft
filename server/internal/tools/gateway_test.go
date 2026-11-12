package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
)

type fakeApprover struct {
	status domain.ApprovalStatus
	answer string
	got    []*domain.Approval
}

func (f *fakeApprover) RequestApproval(_ context.Context, a *domain.Approval) (*domain.Approval, error) {
	f.got = append(f.got, a)
	d := *a
	d.Status, d.Answer = f.status, f.answer
	return &d, nil
}

type captureAuditor struct {
	mu  sync.Mutex
	evs []*domain.Event
}

func (c *captureAuditor) Audit(_ context.Context, ev *domain.Event) {
	c.mu.Lock()
	c.evs = append(c.evs, ev)
	c.mu.Unlock()
}

func (c *captureAuditor) types() []string {
	var out []string
	for _, e := range c.evs {
		out = append(out, string(e.Type))
	}
	return out
}

func principal(dir string, g ...domain.ToolGrant) *Principal {
	return &Principal{GraphID: "g", NodeID: "dev", Replica: 1, ExecutionID: "exe_1", Workdir: dir, Grants: g}
}

func TestGatewayDenyAndAudit(t *testing.T) {
	aud := &captureAuditor{}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, aud)
	p := principal(t.TempDir(), domain.ToolGrant{Name: "filesystem.read"})
	_, err := gw.Invoke(context.Background(), p, "filesystem.write", json.RawMessage(`{"path":"x","content":"y"}`))
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("want ErrDenied, got %v", err)
	}
	if _, err := gw.Invoke(context.Background(), p, "nope", nil); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("want ErrUnknownTool, got %v", err)
	}
	if got := aud.types(); len(got) != 2 || got[0] != "tool.denied" {
		t.Fatalf("events %v", got)
	}
	if aud.evs[0].NodeID != "dev" || aud.evs[0].ExecutionID != "exe_1" {
		t.Fatalf("event attribution: %+v", aud.evs[0])
	}
	if _, err := gw.Invoke(context.Background(), p, "filesystem.read", json.RawMessage(`[1]`)); err == nil {
		t.Fatal("non-object args must fail")
	}
}

func TestGatewayInvokeAndToolErrors(t *testing.T) {
	aud := &captureAuditor{}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, aud)
	p := principal(t.TempDir(), domain.ToolGrant{Name: "filesystem.*"})
	ctx := context.Background()
	res, err := gw.Invoke(ctx, p, "filesystem.write", json.RawMessage(`{"path":"a/b.txt","content":"hello"}`))
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	res, err = gw.Invoke(ctx, p, "filesystem.read", json.RawMessage(`{"path":"a/b.txt"}`))
	if err != nil || res.Content != "hello" {
		t.Fatalf("%v %+v", err, res)
	}
	// A failing tool is a result for the model, not a Go error.
	res, err = gw.Invoke(ctx, p, "filesystem.read", json.RawMessage(`{"path":"missing.txt"}`))
	if err != nil || !res.IsError {
		t.Fatalf("want IsError result, got %v %+v", err, res)
	}
	// Jail violations are refusals.
	if _, err := gw.Invoke(ctx, p, "filesystem.read", json.RawMessage(`{"path":"../etc/passwd"}`)); !errors.Is(err, ErrDenied) {
		t.Fatalf("want ErrDenied, got %v", err)
	}
	types := strings.Join(aud.types(), ",")
	if !strings.Contains(types, "tool.invoked") || !strings.Contains(types, "tool.completed") || !strings.Contains(types, "tool.denied") {
		t.Fatalf("events %s", types)
	}
}

func TestGatewayResultCap(t *testing.T) {
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	gw.MaxResultBytes = 10
	p := principal(t.TempDir(), domain.ToolGrant{Name: "filesystem.*"})
	ctx := context.Background()
	if _, err := gw.Invoke(ctx, p, "filesystem.write", json.RawMessage(`{"path":"f","content":"0123456789abcdef"}`)); err != nil {
		t.Fatal(err)
	}
	res, err := gw.Invoke(ctx, p, "filesystem.read", json.RawMessage(`{"path":"f"}`))
	if err != nil || !res.Truncated || !strings.HasPrefix(res.Content, "0123456789") {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestGatewayApproval(t *testing.T) {
	ctx := context.Background()
	g := domain.ToolGrant{Name: "filesystem.write", RequiresApproval: true}
	rej := &fakeApprover{status: domain.ApprovalRejected}
	gw := NewGateway(Builtin(BuiltinConfig{}), rej, nil)
	p := principal(t.TempDir(), g)
	if _, err := gw.Invoke(ctx, p, "filesystem.write", json.RawMessage(`{"path":"x","content":"y"}`)); !errors.Is(err, ErrRejected) {
		t.Fatalf("want ErrRejected, got %v", err)
	}
	if len(rej.got) != 1 || rej.got[0].Kind != domain.ApprovalTool || rej.got[0].NodeID != "dev" {
		t.Fatalf("approval request %+v", rej.got)
	}
	ok := &fakeApprover{status: domain.ApprovalApproved}
	gw.Approver = ok
	if res, err := gw.Invoke(ctx, p, "filesystem.write", json.RawMessage(`{"path":"x","content":"y"}`)); err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	// No approver configured: deny rather than run unapproved.
	gw.Approver = nil
	if _, err := gw.Invoke(ctx, p, "filesystem.write", json.RawMessage(`{"path":"x","content":"y"}`)); !errors.Is(err, ErrDenied) {
		t.Fatalf("want ErrDenied, got %v", err)
	}
}

func TestGatewayRateLimit(t *testing.T) {
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	now := time.Unix(1000, 0)
	gw.Now = func() time.Time { return now }
	p := principal(t.TempDir(), domain.ToolGrant{Name: "filesystem.stat", RateLimit: 2})
	ctx := context.Background()
	args := json.RawMessage(`{"path":"."}`)
	for i := 0; i < 2; i++ {
		if _, err := gw.Invoke(ctx, p, "filesystem.stat", args); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gw.Invoke(ctx, p, "filesystem.stat", args); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
	now = now.Add(31 * time.Second) // refills one token
	if _, err := gw.Invoke(ctx, p, "filesystem.stat", args); err != nil {
		t.Fatal(err)
	}
}

func TestHumanAsk(t *testing.T) {
	ap := &fakeApprover{status: domain.ApprovalApproved, answer: "use postgres"}
	gw := NewGateway(Builtin(BuiltinConfig{}), ap, nil)
	p := principal(t.TempDir(), domain.ToolGrant{Name: "human.ask"})
	res, err := gw.Invoke(context.Background(), p, "human.ask", json.RawMessage(`{"question":"which db?"}`))
	if err != nil || res.Content != "use postgres" {
		t.Fatalf("%v %+v", err, res)
	}
	if ap.got[0].Kind != domain.ApprovalQuestion {
		t.Fatal("kind")
	}
}

func TestSchemasOnlyGranted(t *testing.T) {
	reg := Builtin(BuiltinConfig{})
	s := reg.Schemas(&Principal{Grants: grants("git.read")})
	if len(s) != 5 {
		t.Fatalf("want 5 git.read tools, got %d", len(s))
	}
	for _, x := range s {
		if !strings.HasPrefix(x.Name, "git.") || !json.Valid(x.Parameters) {
			t.Fatalf("bad schema %+v", x)
		}
	}
	for _, info := range reg.Describe() {
		if !json.Valid(info.Schema) {
			t.Fatalf("invalid schema for %s", info.Name)
		}
	}
}

func TestGuidanceReferenceChecksCheckoutRelativeGrant(t *testing.T) {
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	const ref = ".agents/skills/sample/ref.md"
	p := principal(t.TempDir()+"/src", domain.ToolGrant{Name: "filesystem.read", Paths: []string{ref}})
	called := false
	read := func() string { called = true; return "" }
	if _, err := gw.ReadGuidanceReference(t.Context(), p, "src/"+ref, read); !errors.Is(err, ErrDenied) || called {
		t.Fatalf("subdirectory path should be denied before opening, got %v", err)
	}
	if res, err := gw.ReadGuidanceReference(t.Context(), p, ref, read); err != nil || res.IsError || !called {
		t.Fatalf("checkout path should be allowed, got %+v, %v", res, err)
	}
}

func TestGuidanceReferenceReportsReadFailure(t *testing.T) {
	aud := &captureAuditor{}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, aud)
	p := principal(t.TempDir(), domain.ToolGrant{Name: "filesystem.read"})
	counter := observability.ToolInvocations.WithLabelValues("filesystem.read", "error")
	var before, after dto.Metric
	if err := counter.Write(&before); err != nil {
		t.Fatal(err)
	}
	result, err := gw.ReadGuidanceReference(t.Context(), p, "ref.md", func() string { return "missing" })
	if err != nil || !result.IsError {
		t.Fatalf("expected a failed result, got %+v, %v", result, err)
	}
	if len(aud.evs) != 2 || aud.evs[1].Type != domain.EvToolCompleted || !strings.Contains(string(aud.evs[1].Data), `"outcome":"error"`) {
		t.Fatalf("expected an error completion, got %+v", aud.evs)
	}
	if err := counter.Write(&after); err != nil {
		t.Fatal(err)
	}
	if got := after.GetCounter().GetValue() - before.GetCounter().GetValue(); got != 1 {
		t.Fatalf("error counter increased by %v", got)
	}
}
