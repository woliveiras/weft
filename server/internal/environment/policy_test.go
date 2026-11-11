//go:build !windows

package environment

import (
	"context"
	"errors"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/llm"
	"strings"
	"testing"
)

func TestPolicyRejectsManualBuildEscapes(t *testing.T) {
	h := setup(t)
	for _, source := range []string{"FROM alpine\nRUN --network=default echo escape", "# syntax=custom/frontend\nFROM alpine", "FROM alpine\nADD https://example.test/source /tmp/source"} {
		if err := h.m.ValidatePolicy(context.Background(), &domain.EnvironmentSpec{Dockerfile: source}); !errors.Is(err, ErrInvalidDefinition) {
			t.Fatalf("accepted escape: %v", err)
		}
	}
}

// Real storage and lifecycle, with only the Docker process substituted.
func TestInstallationPolicyRevokesCachedEnvironment(t *testing.T) {
	h := setup(t)
	ctx := context.Background()
	g := graphWith(goNodeEnv())
	if _, err := h.m.Build(ctx, g, false, "test"); err != nil {
		t.Fatal(err)
	}
	if v := h.wait(t, g); !v.UpToDate {
		t.Fatal(v)
	}
	// A restarted controller must enforce current policy against persisted images.
	opt := h.m.opt
	opt.Policy = environmentpolicy.Policy{RunNetworks: []string{"none"}}
	m, err := New(opt)
	if err != nil {
		t.Fatal(err)
	}
	before := len(h.docker.calls(t))
	if _, err = m.Build(ctx, g, false, "test"); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("cached build: %v", err)
	}
	if _, err = m.SandboxFor(ctx, g); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("cached runtime: %v", err)
	}
	if view, err := m.Get(ctx, g); err != nil || view.UpToDate {
		t.Fatalf("policy status: %+v %v", view, err)
	}
	if len(h.docker.calls(t)) != before {
		t.Fatal("denial invoked Docker")
	}
	// Restoring an allowed run network uses the same image; policy does not delete data.
	g.Spec.Environment.Network.Run = "none"
	if _, err = m.SandboxFor(ctx, g); err != nil {
		t.Fatal(err)
	}
}

func TestBuildNetworkPolicyArguments(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(map[bool]string{true: "bounded", false: "legacy"}[bounded], func(t *testing.T) {
			h := setup(t)
			if bounded {
				h = capacityHarness(t)
			}
			h.m.opt.Policy = environmentpolicy.Policy{SetupNetworks: []string{"none"}}
			g := graphWith(&domain.EnvironmentSpec{Dockerfile: "FROM alpine\nRUN echo ok", Network: domain.EnvironmentNetwork{Setup: "none"}})
			if _, err := h.m.Build(context.Background(), g, false, "test"); err != nil {
				t.Fatal(err)
			}
			if v := h.wait(t, g); v.State != StateReady {
				t.Fatal(v)
			}
			found := false
			for _, call := range h.docker.calls(t) {
				if strings.Contains(call, "build --") && strings.Contains(call, "--network none") {
					found = true
				}
			}
			if !found {
				t.Fatal("build network restriction absent")
			}
		})
	}
}

func TestApprovedProposalCannotOverrideNewInstallationPolicy(t *testing.T) {
	docker := newFakeDocker(t)
	p, g, _ := proposalFixture(t, docker.bin)
	ctx := context.Background()
	v, err := p.Request(ctx, g, domain.ModelRef{}, false, "developer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Approvals.Decide(ctx, v.ID, true, "developer", "", ""); err != nil {
		t.Fatal(err)
	}
	opt := p.Manager.opt
	opt.Policy.DisableAgentProposals = true
	p.Manager, err = New(opt)
	if err != nil {
		t.Fatal(err)
	}
	p.Manager.SetProposalValidator(p.ValidateAccepted)
	approval, err := p.Store.GetApproval(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateDecision(ctx, approval); err == nil {
		t.Fatal("approval eligibility ignored new policy")
	}
	if err := p.Apply(ctx, g.Metadata.ID); err == nil {
		t.Fatal("old approval bypassed current policy")
	}
	saved, err := p.Store.GetGraph(ctx, g.Metadata.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Spec.Environment != nil {
		t.Fatal("denied environment saved")
	}
	if len(docker.calls(t)) != 0 {
		t.Fatal("denied approval invoked Docker")
	}
}

func TestAnalysisReceivesInstallationRestrictions(t *testing.T) {
	called := false
	model := analysisModelFunc(func(_ context.Context, _ string, request *llm.ChatRequest) (*llm.ChatResponse, error) {
		called = true
		if !strings.Contains(request.System, `"setupNetworks":["none"]`) || !strings.Contains(request.System, `"disableDockerfiles":true`) {
			t.Fatal("installation policy missing from analysis prompt")
		}
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: `{"environment":{"image":"alpine","network":{"setup":"none"}},"rationale":"offline toolchain"}`}}, nil
	})
	policy := environmentpolicy.Policy{SetupNetworks: []string{"none"}, DisableDockerfiles: true}
	if _, err := analyzeWithPolicy(context.Background(), model, domain.ModelRef{}, &Evidence{}, policy); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("model not called")
	}
	called = false
	policy.DisableAgentProposals = true
	if _, err := analyzeWithPolicy(context.Background(), model, domain.ModelRef{}, &Evidence{}, policy); err == nil || called {
		t.Fatal("disabled analysis invoked model")
	}
}
