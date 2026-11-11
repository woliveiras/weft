package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/woliveiras/weft/server/internal/executioncontrol"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/delegation"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/storage"
)

type delegateProvider struct {
	mu       sync.Mutex
	children int
	request  *llm.ChatRequest
	mode     string
	profile  string
}

func (*delegateProvider) Name() string                                    { return "delegate" }
func (*delegateProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (p *delegateProvider) Chat(ctx context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	if strings.Contains(r.System, "temporary analysis assistant") {
		p.mu.Lock()
		p.children++
		p.request = r
		p.mu.Unlock()
		if p.mode == "recursive" {
			for _, msg := range r.Messages {
				if msg.Role == llm.RoleTool {
					return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Nested result: " + msg.Content}, Usage: llm.Usage{InputTokens: 7, OutputTokens: 3}}, nil
				}
			}
			if !strings.Contains(r.Messages[0].Content, "leaf") {
				args, _ := json.Marshal(map[string]string{"template": p.profile, "objective": "leaf analysis"})
				return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "nested", Name: "delegation.request", Arguments: args}}}, Usage: llm.Usage{InputTokens: 7, OutputTokens: 3}}, nil
			}
		}
		if strings.HasPrefix(p.mode, "inherit") {
			for _, msg := range r.Messages {
				if msg.Role == llm.RoleTool && p.mode != "inherit-repeat" {
					return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Read result: " + msg.Content}, Usage: llm.Usage{InputTokens: 7, OutputTokens: 3}}, nil
				}
			}
			name := "filesystem.read"
			path := "docs/allowed.txt"
			if p.mode == "inherit-missing" {
				path = "docs/missing.txt"
			}
			if p.mode == "inherit-denied" {
				path = "private.txt"
			}
			if p.mode == "inherit-write" {
				name = "filesystem.write"
			}
			args, _ := json.Marshal(map[string]string{"path": path, "content": "forbidden"})
			return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "read", Name: name, Arguments: args}}}, Usage: llm.Usage{InputTokens: 7, OutputTokens: 3}}, nil
		}
		if p.mode == "failure" {
			return nil, errors.New("upstream private diagnostic")
		}
		if p.mode == "timeout" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if p.mode == "tool" {
			return &llm.ChatResponse{Message: llm.ChatMessage{Content: "try", ToolCalls: []llm.ToolCall{{Name: "shell.exec"}}}}, nil
		}
		if p.mode == "empty" {
			return &llm.ChatResponse{}, nil
		}
		if p.mode == "length" {
			return &llm.ChatResponse{Message: llm.ChatMessage{Content: "partial"}, StopReason: "length"}, nil
		}
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: "bounded child answer"}, Usage: llm.Usage{InputTokens: 7, OutputTokens: 3}}, nil
	}
	for _, m := range r.Messages {
		if m.Role == llm.RoleTool && p.mode != "repeat" {
			return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Parent received: " + m.Content}, Usage: llm.Usage{InputTokens: 2, OutputTokens: 1}}, nil
		}
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "maximum children") {
			return &llm.ChatResponse{Message: llm.ChatMessage{Content: m.Content}}, nil
		}
	}
	profile := p.profile
	if profile == "" {
		profile = "analysis"
	}
	args, _ := json.Marshal(map[string]string{"template": profile, "objective": "Analyze supplied text without tools"})
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: domain.NewID("call"), Name: "delegation.request", Arguments: args}}}, Usage: llm.Usage{InputTokens: 2, OutputTokens: 1}}, nil
}
func delegationRig(t *testing.T, mode string, opts ...func(*engine.Options)) (*rig, *delegateProvider) {
	t.Helper()
	dir := t.TempDir()
	p := &delegateProvider{mode: mode}
	options := []func(*engine.Options){func(o *engine.Options) {
		o.Models.(*llm.Registry).Add(llm.ProviderConfig{Name: "delegate"}, p)
		o.MaxConcurrentTurns = 1
	}}
	options = append(options, opts...)
	return newRig(t, openStore(t, dir), memory.New(), dir, "delegate-instance", options...), p
}
func deployDelegation(r *rig, grants, limit string) {
	r.deploy("metadata: {name: delegation}\nspec:\n  nodes:\n    - id: parent\n      runtime: {kind: native}\n      prompt: Delegate analysis\n      model: {provider: delegate, name: fixture}\n      loop: {mode: once}\n      isolation: {mode: process}\n      tools: " + grants + "\n      limits: " + limit + "\n")
}
func delegationApproval(t *testing.T, r *rig) *domain.Approval {
	t.Helper()
	var a *domain.Approval
	eventually(t, 5*time.Second, "delegation approval", func() bool {
		all, _ := r.store.ListApprovals(r.ctx, storage.ApprovalFilter{GraphID: "delegation", Status: domain.ApprovalPending})
		for _, v := range all {
			if v.Kind == domain.ApprovalDelegation {
				a = v
				return true
			}
		}
		return false
	})
	return a
}
func finishedParent(t *testing.T, r *rig) *domain.Execution {
	t.Helper()
	var parent *domain.Execution
	eventually(t, 5*time.Second, "parent completed", func() bool {
		all, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "delegation"})
		for _, x := range all {
			if x.ParentExecutionID == "" && x.Status.Terminal() {
				parent = x
				return true
			}
		}
		return false
	})
	return parent
}
func TestDelegationApprovalResultAndNoAmplification(t *testing.T) {
	r, p := delegationRig(t, "")
	deployDelegation(r, "[delegation.request, filesystem.read]", "{turnTimeout: 10s}")
	a := delegationApproval(t, r)
	p.mu.Lock()
	n := p.children
	p.mu.Unlock()
	if n != 0 {
		t.Fatal("child ran before approval")
	}
	x, err := r.store.GetExecution(r.ctx, a.ExecutionID)
	if err != nil || x.Status != domain.ExecAwaitingApproval || x.ParentExecutionID == "" {
		t.Fatalf("%+v %v", x, err)
	}
	if !strings.Contains(string(a.Detail), `"effectiveTools":[]`) {
		t.Fatal(string(a.Detail))
	}
	if _, err = r.apr.Decide(r.ctx, a.ID, true, "operator", "", ""); err != nil {
		t.Fatal(err)
	}
	parent := finishedParent(t, r)
	if !strings.Contains(parent.Summary, "bounded child answer") {
		t.Fatal(parent.Summary)
	}
	if parent.Usage.InputTokens != 11 || parent.Usage.OutputTokens != 5 {
		t.Fatalf("usage counted incorrectly: %+v", parent.Usage)
	}
	child, _ := r.store.GetExecution(r.ctx, a.ExecutionID)
	if child.Status != domain.ExecSucceeded || child.Summary != "bounded child answer" || child.Usage.InputTokens != 7 {
		t.Fatalf("%+v", child)
	}
	if child.DelegationAudit == nil || child.DelegationAudit.Decision != "approved" || child.DelegationAudit.DecidedBy != "operator" || child.DelegationAudit.DecidedAt == nil || child.DelegationAudit.EffectiveGrants == nil || len(child.DelegationAudit.EffectiveGrants) != 0 || child.DelegationAudit.WorkspaceAccess || child.DelegationAudit.RecursiveAccess || child.DelegationAudit.MaxOutputTokens != 1024 {
		t.Fatalf("incomplete audit: %+v", child.DelegationAudit)
	}
	p.mu.Lock()
	req := p.request
	p.mu.Unlock()
	if len(req.Tools) != 0 || req.Model != "fixture" || req.MaxTokens != 1024 || len(req.Messages) != 1 {
		t.Fatalf("%+v", req)
	}
	g, _ := r.store.GetGraph(r.ctx, "delegation", 0)
	if len(g.Spec.Nodes) != 1 || g.Metadata.Version != 1 {
		t.Fatal("permanent graph changed")
	}
	leases, _ := r.store.ListLeases(r.ctx, "delegation/")
	if len(leases) != 0 {
		t.Fatalf("leaked slots: %v", leases)
	}
}
func TestDelegationRejectionNeverRunsChild(t *testing.T) {
	r, p := delegationRig(t, "")
	deployDelegation(r, "[delegation.request]", "{turnTimeout: 10s}")
	a := delegationApproval(t, r)
	if _, err := r.apr.Decide(r.ctx, a.ID, false, "operator", "no", ""); err != nil {
		t.Fatal(err)
	}
	parent := finishedParent(t, r)
	if !strings.Contains(parent.Summary, "rejected") {
		t.Fatal(parent.Summary)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	child, err := r.store.GetExecution(r.ctx, a.ExecutionID)
	if err != nil || child.DelegationAudit == nil || child.DelegationAudit.Decision != "rejected" {
		t.Fatalf("rejection audit: %+v %v", child, err)
	}
	if p.children != 0 {
		t.Fatal("ran rejected child")
	}
}
func TestDelegationFailurePaths(t *testing.T) {
	for _, mode := range []string{"failure", "empty", "tool", "length", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			r, _ := delegationRig(t, mode)
			deployDelegation(r, "[delegation.request]", "{turnTimeout: 2s}")
			a := delegationApproval(t, r)
			if _, err := r.apr.Decide(r.ctx, a.ID, true, "operator", "", ""); err != nil {
				t.Fatal(err)
			}
			finishedParent(t, r)
			child, _ := r.store.GetExecution(r.ctx, a.ExecutionID)
			if !child.Status.Terminal() || child.Status == domain.ExecSucceeded {
				t.Fatalf("%+v", child)
			}
			if strings.Contains(child.Error, "private diagnostic") {
				t.Fatal("upstream error leaked")
			}
		})
	}
}
func TestDelegationAdmissionFailsClosed(t *testing.T) {
	for _, mode := range []string{"budget", "capacity", "no-grant", "wildcard"} {
		t.Run(mode, func(t *testing.T) {
			r, p := delegationRig(t, "")
			grants := "[delegation.request]"
			limits := "{turnTimeout: 5s}"
			switch mode {
			case "budget":
				limits = "{turnTimeout: 5s, maxTokens: 100}"
			case "capacity":
				for i := 0; i < 4; i++ {
					ok, err := r.store.AcquireLease(r.ctx, "delegation/slot/"+string(rune('0'+i)), "other-instance", time.Minute)
					if err != nil || !ok {
						t.Fatal(err)
					}
				}
			case "no-grant":
				grants = "[]"
			case "wildcard":
				grants = "['*']"
			}
			deployDelegation(r, grants, limits)
			finishedParent(t, r)
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.children != 0 {
				t.Fatal("admission failure ran child")
			}
			as, _ := r.store.ListApprovals(r.ctx, storage.ApprovalFilter{GraphID: "delegation"})
			if len(as) != 0 {
				t.Fatal("admission failure created approval")
			}
		})
	}
}
func TestDelegationChildrenLimit(t *testing.T) {
	r, p := delegationRig(t, "repeat")
	deployDelegation(r, "[delegation.request]", "{turnTimeout: 10s}")
	for i := 0; i < 3; i++ {
		a := delegationApproval(t, r)
		if _, err := r.apr.Decide(r.ctx, a.ID, true, "operator", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	parent := finishedParent(t, r)
	if !strings.Contains(parent.Summary, "maximum children") {
		t.Fatal(parent.Summary)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.children != 3 {
		t.Fatal(p.children)
	}
}

func TestDelegationWaitingCancellationExpiresApproval(t *testing.T) {
	r, p := delegationRig(t, "")
	deployDelegation(r, "[delegation.request]", "{turnTimeout: 1s}")
	a := delegationApproval(t, r)
	finishedParent(t, r)
	current, _ := r.store.GetApproval(r.ctx, a.ID)
	if current.Status != domain.ApprovalExpired {
		t.Fatalf("%+v", current)
	}
	child, _ := r.store.GetExecution(r.ctx, a.ExecutionID)
	if child.Status != domain.ExecCancelled {
		t.Fatalf("%+v", child)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.children != 0 {
		t.Fatal("cancelled child ran")
	}
}
func TestDelegationConcurrentGlobalCapacity(t *testing.T) {
	r, p := delegationRig(t, "", func(o *engine.Options) { o.MaxConcurrentTurns = 10 })
	for i := 0; i < 5; i++ {
		r.deploy("metadata: {name: delegate-capacity-" + string(rune('a'+i)) + "}\nspec:\n  nodes:\n    - id: parent\n      runtime: {kind: native}\n      model: {provider: delegate, name: fixture}\n      prompt: delegate\n      loop: {mode: once}\n      limits: {turnTimeout: 10s}\n      tools: [delegation.request]\n")
	}
	var approvals []*domain.Approval
	eventually(t, 5*time.Second, "four reserved children and one capacity refusal", func() bool {
		approvals, _ = r.store.ListApprovals(r.ctx, storage.ApprovalFilter{Status: domain.ApprovalPending})
		xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{})
		refused := false
		for _, x := range xs {
			if strings.Contains(x.Summary, "capacity exhausted") {
				refused = true
			}
		}
		return len(approvals) == 4 && refused
	})
	leases, _ := r.store.ListLeases(r.ctx, "delegation/")
	if len(leases) != 4 {
		t.Fatal(len(leases))
	}
	for _, a := range approvals {
		if _, err := r.apr.Decide(r.ctx, a.ID, false, "operator", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, 5*time.Second, "capacity released", func() bool { ls, _ := r.store.ListLeases(r.ctx, "delegation/"); return len(ls) == 0 })
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.children != 0 {
		t.Fatal("unapproved child executed")
	}
}
func TestDelegationRestartInterruptsPendingChild(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	ctx := context.Background()
	if err := s.PutExecution(ctx, &domain.Execution{ID: "child", ParentExecutionID: "parent", ApprovalID: "approval", DelegationAudit: &domain.DelegationAudit{Decision: "pending"}, GraphID: "g", NodeID: "n", Instance: "restarted", Status: domain.ExecAwaitingApproval, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutApproval(ctx, &domain.Approval{ID: "approval", Kind: domain.ApprovalDelegation, GraphID: "g", ExecutionID: "child", Status: domain.ApprovalPending, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, s, memory.New(), dir, "restarted")
	x, _ := r.store.GetExecution(ctx, "child")
	a, _ := r.store.GetApproval(ctx, "approval")
	if x.Status != domain.ExecInterrupted || a.Status != domain.ApprovalExpired || x.DelegationAudit.Decision != "expired" {
		t.Fatalf("%+v %+v", x, a)
	}
}

type failChildStore struct {
	storage.Store
	mu           sync.Mutex
	failed       bool
	terminalOnly bool
}

func (s *failChildStore) PutExecution(ctx context.Context, x *domain.Execution) error {
	s.mu.Lock()
	fail := x.ParentExecutionID != "" && x.ApprovalID != "" && !s.failed && (!s.terminalOnly || x.CompletedAt != nil)
	if fail {
		s.failed = true
	}
	s.mu.Unlock()
	if fail {
		return errors.New("injected child persistence failure")
	}
	return s.Store.PutExecution(ctx, x)
}
func TestDelegationPersistenceFailureDoesNotRunOrLeavePendingApproval(t *testing.T) {
	r, p := delegationRig(t, "", func(o *engine.Options) { o.Store = &failChildStore{Store: o.Store} })
	deployDelegation(r, "[delegation.request]", "{turnTimeout: 5s}")
	finishedParent(t, r)
	as, _ := r.store.ListApprovals(r.ctx, storage.ApprovalFilter{GraphID: "delegation"})
	if len(as) != 1 || as[0].Status != domain.ApprovalExpired {
		t.Fatalf("%+v", as)
	}
	leases, _ := r.store.ListLeases(r.ctx, "delegation/")
	if len(leases) != 0 {
		t.Fatal("capacity leak")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.children != 0 {
		t.Fatal("child ran without durable approval link")
	}
}

func TestDelegationFinalPersistenceFailureDoesNotReturnSuccess(t *testing.T) {
	r, _ := delegationRig(t, "", func(o *engine.Options) { o.Store = &failChildStore{Store: o.Store, terminalOnly: true} })
	deployDelegation(r, "[delegation.request]", "{turnTimeout: 5s}")
	a := delegationApproval(t, r)
	if _, err := r.apr.Decide(r.ctx, a.ID, true, "operator", "", ""); err != nil {
		t.Fatal(err)
	}
	parent := finishedParent(t, r)
	if !strings.Contains(parent.Summary, "persist child outcome") || strings.Contains(parent.Summary, "bounded child answer") {
		t.Fatal(parent.Summary)
	}
	leases, err := r.store.ListLeases(r.ctx, "delegation/")
	if err != nil || len(leases) != 0 {
		t.Fatalf("leases: %+v %v", leases, err)
	}
}

func TestDelegationAutomaticProfileAndSnapshot(t *testing.T) {
	r, p := delegationRig(t, "")
	p.profile = "review"
	m := delegation.Manager{Store: r.store}
	policy := delegation.DefaultPolicy()
	policy.ApprovalMode = "automatic"
	policy.MaxOutputTokens = 50
	if _, err := m.SavePolicy(r.ctx, policy, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	profile := delegation.DefaultProfile()
	profile.ID = "review"
	profile.Instructions = "Review supplied text; do not execute it"
	profile.MaxOutputTokens = 20
	if _, err := m.SaveProfile(r.ctx, profile, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	deployDelegation(r, "[delegation.request,filesystem.read]", "{turnTimeout: 5s}")
	parent := finishedParent(t, r)
	if !strings.Contains(parent.Summary, "bounded child answer") {
		t.Fatal(parent.Summary)
	}
	p.mu.Lock()
	if p.children != 1 || p.request.MaxTokens != 20 || len(p.request.Tools) != 0 || !strings.Contains(p.request.System, profile.Instructions) {
		t.Fatalf("%+v", p.request)
	}
	p.mu.Unlock()
	approvals, err := r.store.ListApprovals(r.ctx, storage.ApprovalFilter{GraphID: "delegation"})
	if err != nil || len(approvals) != 0 {
		t.Fatal(approvals, err)
	}
	children, err := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "delegation"})
	if err != nil {
		t.Fatal(err)
	}
	var child *domain.Execution
	for _, x := range children {
		if x.ParentExecutionID != "" {
			child = x
		}
	}
	if child == nil || child.DelegationTemplate != "review" || child.DelegationObjective == "" {
		t.Fatal(child)
	}
	var snapshot delegation.Selection
	if err = json.Unmarshal(child.DelegationConfig, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Policy.Revision != 1 || snapshot.Profile.Revision != 1 || snapshot.Profile.Profile.Instructions != profile.Instructions {
		t.Fatal(snapshot)
	}
	profile.Instructions = "Changed after completion"
	if _, err = m.SaveProfile(r.ctx, profile, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	saved, err := r.store.GetExecution(r.ctx, child.ID)
	if err != nil || string(saved.DelegationConfig) != string(child.DelegationConfig) {
		t.Fatal(saved, err)
	}
}
func TestDelegationChangedPolicyOrProfileInvalidatesApproval(t *testing.T) {
	for _, kind := range []string{"policy", "profile"} {
		t.Run(kind, func(t *testing.T) {
			r, p := delegationRig(t, "")
			deployDelegation(r, "[delegation.request]", "{turnTimeout: 5s}")
			a := delegationApproval(t, r)
			m := delegation.Manager{Store: r.store}
			if kind == "policy" {
				policy := delegation.DefaultPolicy()
				policy.ApprovalMode = "automatic"
				if _, err := m.SavePolicy(r.ctx, policy, 0, "admin"); err != nil {
					t.Fatal(err)
				}
			} else {
				profile := delegation.DefaultProfile()
				profile.Enabled = false
				if _, err := m.SaveProfile(r.ctx, profile, 0, "admin"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.apr.Decide(r.ctx, a.ID, true, "operator", "", ""); err != nil {
				t.Fatal(err)
			}
			parent := finishedParent(t, r)
			if !strings.Contains(parent.Summary, "configuration changed") {
				t.Fatal(parent.Summary)
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.children != 0 {
				t.Fatal("stale child ran")
			}
		})
	}
}
func TestDelegationConfiguredAdmissionAndResultLimits(t *testing.T) {
	for _, mode := range []string{"disabled", "profile-disabled", "objective", "result", "output", "children", "duration"} {
		t.Run(mode, func(t *testing.T) {
			providerMode := ""
			if mode == "children" {
				providerMode = "repeat"
			}
			if mode == "duration" {
				providerMode = "timeout"
			}
			r, p := delegationRig(t, providerMode)
			m := delegation.Manager{Store: r.store}
			policy := delegation.DefaultPolicy()
			policy.ApprovalMode = "automatic"
			switch mode {
			case "disabled":
				policy.Enabled = false
			case "objective":
				policy.MaxObjectiveBytes = 1
			case "result":
				policy.MaxResultBytes = 1
			case "output":
				policy.MaxOutputTokens = 1
			case "children":
				policy.MaxChildrenPerExecution = 1
			case "duration":
				policy.MaxDurationSeconds = 1
			}
			if _, err := m.SavePolicy(r.ctx, policy, 0, "admin"); err != nil {
				t.Fatal(err)
			}
			if mode == "profile-disabled" {
				profile := delegation.DefaultProfile()
				profile.Enabled = false
				if _, err := m.SaveProfile(r.ctx, profile, 0, "admin"); err != nil {
					t.Fatal(err)
				}
			}
			deployDelegation(r, "[delegation.request]", "{turnTimeout: 5s}")
			parent := finishedParent(t, r)
			if strings.Contains(parent.Summary, `"status":"succeeded"`) {
				t.Fatal("limit ignored", parent.Summary)
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if (mode == "disabled" || mode == "profile-disabled" || mode == "objective") && p.children != 0 {
				t.Fatal("admission bypass")
			}
			if mode == "children" && p.children != 1 {
				t.Fatal(p.children)
			}
		})
	}
}
func TestDelegationAutomaticDoesNotBypassExplicitGrant(t *testing.T) {
	r, p := delegationRig(t, "")
	policy := delegation.DefaultPolicy()
	policy.ApprovalMode = "automatic"
	if _, err := (delegation.Manager{Store: r.store}).SavePolicy(r.ctx, policy, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	deployDelegation(r, "['*']", "{turnTimeout: 5s}")
	finishedParent(t, r)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.children != 0 {
		t.Fatal("automatic policy granted tools")
	}
}

func TestDelegationAutomaticRetainsGenericToolApproval(t *testing.T) {
	r, p := delegationRig(t, "")
	policy := delegation.DefaultPolicy()
	policy.ApprovalMode = "automatic"
	if _, err := (delegation.Manager{Store: r.store}).SavePolicy(r.ctx, policy, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	deployDelegation(r, "[{name: delegation.request, requiresApproval: true}]", "{turnTimeout: 5s}")
	var a *domain.Approval
	eventually(t, 3*time.Second, "generic approval", func() bool {
		xs, _ := r.store.ListApprovals(r.ctx, storage.ApprovalFilter{GraphID: "delegation", Status: domain.ApprovalPending})
		for _, x := range xs {
			if x.Kind == domain.ApprovalTool {
				a = x
				return true
			}
		}
		return false
	})
	p.mu.Lock()
	n := p.children
	p.mu.Unlock()
	if n != 0 {
		t.Fatal("generic approval bypassed")
	}
	if _, err := r.apr.Decide(r.ctx, a.ID, false, "operator", "", ""); err != nil {
		t.Fatal(err)
	}
	finishedParent(t, r)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.children != 0 {
		t.Fatal("rejected tool ran")
	}
}

func TestDelegationExecutionControls(t *testing.T) {
	for _, scope := range []string{"child", "tree"} {
		for _, running := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/running=%t", scope, running), func(t *testing.T) {
				r, p := delegationRig(t, "timeout")
				deployDelegation(r, "[delegation.request]", "{turnTimeout: 10s}")
				a := delegationApproval(t, r)
				if running {
					if _, err := r.apr.Decide(r.ctx, a.ID, true, "operator", "", ""); err != nil {
						t.Fatal(err)
					}
					eventually(t, time.Second, "child running", func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.children == 1 })
				}
				result, err := (executioncontrol.Manager{Store: r.store}).Cancel(r.ctx, a.ExecutionID, scope, "operator")
				if err != nil || !result.Created {
					t.Fatal(result, err)
				}
				parent := finishedParent(t, r)
				want := domain.ExecSucceeded
				if scope == "tree" {
					want = domain.ExecCancelled
				}
				if parent.Status != want {
					t.Fatalf("parent %+v", parent)
				}
				child, err := r.store.GetExecution(r.ctx, a.ExecutionID)
				if err != nil || child.Status != domain.ExecCancelled {
					t.Fatalf("child %+v %v", child, err)
				}
				approval, _ := r.store.GetApproval(r.ctx, a.ID)
				if !running && approval.Status != domain.ApprovalExpired {
					t.Fatal(approval)
				}
				leases, _ := r.store.ListLeases(r.ctx, "delegation/slot/")
				if len(leases) != 0 {
					t.Fatal("capacity leaked", leases)
				}
				graph, _ := r.store.GetGraph(r.ctx, "delegation", 0)
				if graph.Metadata.Version != 1 || len(graph.Spec.Nodes) != 1 {
					t.Fatal("graph mutated")
				}
			})
		}
	}
}

func TestDelegationInheritedToolsUseRealGateway(t *testing.T) {
	for _, mode := range []string{"inherit", "inherit-denied", "inherit-write", "inherit-repeat", "inherit-missing"} {
		t.Run(mode, func(t *testing.T) {
			r, p := delegationRig(t, mode)
			p.profile = "reader"
			manager := delegation.Manager{Store: r.store}
			policy := delegation.DefaultPolicy()
			policy.ApprovalMode = "automatic"
			if _, err := manager.SavePolicy(r.ctx, policy, 0, "admin"); err != nil {
				t.Fatal(err)
			}
			profile := delegation.DefaultProfile()
			profile.ID = "reader"
			profile.Tools = []string{"filesystem.read"}
			if _, err := manager.SaveProfile(r.ctx, profile, 0, "admin"); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(r.dir, "ws", "delegation")
			if err := os.MkdirAll(filepath.Join(dir, "docs"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "docs", "allowed.txt"), []byte("allowed content"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "private.txt"), []byte("private content"), 0600); err != nil {
				t.Fatal(err)
			}
			deployDelegation(r, `[delegation.request, {name: filesystem.read, paths: ["docs/**"]}]`, "{turnTimeout: 10s, maxToolCalls: 4}")
			parent := finishedParent(t, r)
			children, err := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{ParentExecutionID: parent.ID})
			if err != nil || len(children) != 1 {
				t.Fatalf("%v %v", children, err)
			}
			child := children[0]
			if child.ToolCalls < 1 || child.ToolCalls > 3 || parent.ToolCalls != child.ToolCalls+1 {
				t.Fatalf("invalid call totals: parent=%d child=%d", parent.ToolCalls, child.ToolCalls)
			}
			if mode == "inherit" {
				budget, err := manager.Budget(r.ctx, parent.ID)
				if err != nil || budget == nil || budget.Reserved != 0 || budget.Charged != parent.Usage.InputTokens+parent.Usage.OutputTokens {
					t.Fatalf("budget %+v parent %+v err %v", budget, parent.Usage, err)
				}
				if child.Status != domain.ExecSucceeded || !strings.Contains(parent.Summary, "allowed content") || child.Usage.InputTokens != 14 {
					t.Fatalf("%+v %+v", child, parent)
				}
			} else if child.Status != domain.ExecFailed || strings.Contains(parent.Summary, "private content") {
				t.Fatalf("%+v", child)
			}
			if child.DelegationAudit.Decision != "automatic" || len(child.DelegationAudit.EffectiveGrants) != 1 || child.DelegationAudit.EffectiveGrants[0].Paths[0] != "docs/**" {
				t.Fatalf("%+v", child.DelegationAudit)
			}
			p.mu.Lock()
			schemas := p.request.Tools
			p.mu.Unlock()
			for _, schema := range schemas {
				if schema.Name == "filesystem.write" || schema.Name == "delegation.request" || schema.Name == "state_get" {
					t.Fatal("ungranted schema", schema.Name)
				}
			}
			data, err := os.ReadFile(filepath.Join(dir, "docs", "allowed.txt"))
			if err != nil || string(data) != "allowed content" {
				t.Fatal("workspace modified", err)
			}
		})
	}
}

func TestDelegationMissingInheritedGrantRejectsBeforeChild(t *testing.T) {
	r, p := delegationRig(t, "inherit")
	p.profile = "reader"
	manager := delegation.Manager{Store: r.store}
	profile := delegation.DefaultProfile()
	profile.ID = "reader"
	profile.Tools = []string{"filesystem.read"}
	if _, err := manager.SaveProfile(r.ctx, profile, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	deployDelegation(r, "[delegation.request]", "{turnTimeout: 5s}")
	parent := finishedParent(t, r)
	if !strings.Contains(parent.Summary, "does not explicitly grant filesystem.read") {
		t.Fatal(parent.Summary)
	}
	children, err := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{ParentExecutionID: parent.ID})
	if err != nil || len(children) != 0 {
		t.Fatalf("%v %v", children, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.children != 0 {
		t.Fatal("ungranted child ran")
	}
}
func TestDelegationInheritedToolRejectionStopsChild(t *testing.T) {
	r, p := delegationRig(t, "inherit")
	p.profile = "reader"
	manager := delegation.Manager{Store: r.store}
	policy := delegation.DefaultPolicy()
	policy.ApprovalMode = "automatic"
	if _, err := manager.SavePolicy(r.ctx, policy, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	profile := delegation.DefaultProfile()
	profile.ID = "reader"
	profile.Tools = []string{"filesystem.read"}
	if _, err := manager.SaveProfile(r.ctx, profile, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	deployDelegation(r, `[delegation.request, {name: filesystem.read, requiresApproval: true}]`, "{turnTimeout: 10s}")
	var approval *domain.Approval
	eventually(t, 5*time.Second, "child tool approval", func() bool {
		rows, _ := r.store.ListApprovals(r.ctx, storage.ApprovalFilter{GraphID: "delegation", Status: domain.ApprovalPending})
		for _, a := range rows {
			if a.Kind == domain.ApprovalTool {
				approval = a
				return true
			}
		}
		return false
	})
	child, err := r.store.GetExecution(r.ctx, approval.ExecutionID)
	if err != nil || child.ParentExecutionID == "" {
		t.Fatalf("%+v %v", child, err)
	}
	if _, err := r.apr.Decide(r.ctx, approval.ID, false, "operator", "", ""); err != nil {
		t.Fatal(err)
	}
	parent := finishedParent(t, r)
	child, err = r.store.GetExecution(r.ctx, approval.ExecutionID)
	if err != nil || child.Status != domain.ExecFailed || child.ToolCalls != 1 || !strings.Contains(parent.Summary, "delegated tool call failed") {
		t.Fatalf("%+v %+v %v", child, parent, err)
	}
}

func TestDelegationRecursiveTreeSharesBudgetsAndDepth(t *testing.T) {
	for _, depth := range []int{1, 2} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			r, p := delegationRig(t, "recursive")
			p.profile = "recursive"
			manager := delegation.Manager{Store: r.store}
			policy := delegation.DefaultPolicy()
			policy.ApprovalMode = "automatic"
			policy.MaxDepth = depth
			if _, err := manager.SavePolicy(r.ctx, policy, 0, "admin"); err != nil {
				t.Fatal(err)
			}
			profile := delegation.DefaultProfile()
			profile.ID = "recursive"
			profile.Tools = []string{"delegation.request"}
			if _, err := manager.SaveProfile(r.ctx, profile, 0, "admin"); err != nil {
				t.Fatal(err)
			}
			deployDelegation(r, "[delegation.request]", "{turnTimeout: 10s, maxToolCalls: 8}")
			root := finishedParent(t, r)
			tree, err := (executioncontrol.Manager{Store: r.store}).Tree(r.ctx, root.ID)
			if err != nil || len(tree.Nodes) != depth+1 {
				t.Fatalf("%+v %v", tree, err)
			}
			if depth == 2 {
				if !strings.Contains(root.Summary, "bounded child answer") {
					t.Fatal(root.Summary)
				}
				child, grandchild := tree.Nodes[1].Execution, tree.Nodes[2].Execution
				if child.Status != domain.ExecSucceeded || grandchild.Status != domain.ExecSucceeded || grandchild.ParentExecutionID != child.ID || child.Usage.InputTokens != 21 || root.Usage.InputTokens != 25 || root.Usage.OutputTokens != 11 {
					t.Fatalf("root=%+v child=%+v grandchild=%+v", root, child, grandchild)
				}
			} else if tree.Nodes[1].Execution.Status != domain.ExecFailed {
				t.Fatalf("depth bypass: %+v", tree.Nodes[1])
			}
			ledgers, err := r.store.ListSettings(r.ctx, delegation.BudgetPrefix)
			if err != nil || len(ledgers) != 1 || ledgers[0].Key != delegation.BudgetPrefix+root.ID {
				t.Fatalf("split tree ledger: %v %v", ledgers, err)
			}
			budget, err := manager.Budget(r.ctx, root.ID)
			if err != nil || budget.Reserved != 0 || budget.Charged != root.Usage.InputTokens+root.Usage.OutputTokens {
				t.Fatalf("budget %+v usage %+v err %v", budget, root.Usage, err)
			}
			leases, err := r.store.ListLeases(r.ctx, "delegation/")
			if err != nil || len(leases) != 0 {
				t.Fatalf("leaked capacity: %v %v", leases, err)
			}
		})
	}
}

func TestDelegationRecursiveCancellationClosesGrandchildApproval(t *testing.T) {
	r, p := delegationRig(t, "recursive")
	p.profile = "recursive"
	manager := delegation.Manager{Store: r.store}
	policy := delegation.DefaultPolicy()
	policy.MaxDepth = 2
	if _, err := manager.SavePolicy(r.ctx, policy, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	profile := delegation.DefaultProfile()
	profile.ID = "recursive"
	profile.Tools = []string{"delegation.request"}
	if _, err := manager.SaveProfile(r.ctx, profile, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	deployDelegation(r, "[delegation.request]", "{turnTimeout: 10s}")
	first := delegationApproval(t, r)
	if _, err := r.apr.Decide(r.ctx, first.ID, true, "operator", "", ""); err != nil {
		t.Fatal(err)
	}
	second := delegationApproval(t, r)
	if second.ID == first.ID {
		t.Fatal("missing grandchild approval")
	}
	control := executioncontrol.Manager{Store: r.store}
	if _, err := control.Cancel(r.ctx, second.ExecutionID, "tree", "operator"); err != nil {
		t.Fatal(err)
	}
	root := finishedParent(t, r)
	tree, err := control.Tree(r.ctx, root.ID)
	if err != nil || len(tree.Nodes) != 3 {
		t.Fatalf("%+v %v", tree, err)
	}
	for _, node := range tree.Nodes {
		if node.Execution.Status != domain.ExecCancelled {
			t.Fatalf("uncancelled descendant %+v", node.Execution)
		}
	}
	approval, err := r.store.GetApproval(r.ctx, second.ID)
	if err != nil || approval.Status != domain.ApprovalExpired {
		t.Fatalf("%+v %v", approval, err)
	}
	budget, err := manager.Budget(r.ctx, root.ID)
	if err != nil || budget.Reserved != 0 || budget.Charged != 13 {
		t.Fatalf("%+v %v", budget, err)
	}
}
