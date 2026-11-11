//go:build !windows

package environment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/woliveiras/weft/server/internal/storage"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

const agentProposalArgs = `{"environment":{"dockerfile":"FROM alpine:3.22\nCOPY package.json /manifest\nRUN echo proposal-ok > /proof\n"},"rationale":"A shell and manifest for the task; setup may fetch Alpine.","tools":["sh"]}`

func exerciseAgentProposal(t *testing.T, bin, decision string) {
	t.Helper()
	p, g, dir := proposalFixture(t, bin)
	g.Spec.Nodes = []domain.Node{{ID: "worker", Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}}}
	if _, err := p.Store.SaveGraph(context.Background(), g, g.Metadata.Version); err != nil {
		t.Fatal(err)
	}
	if err := p.Store.PutGraphRecord(context.Background(), &domain.GraphRecord{ID: g.Metadata.ID, State: domain.GraphRunning, DesiredState: domain.GraphRunning}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	registry := tools.NewRegistry()
	registry.MustRegister(p.AgentTools()...)
	gateway := tools.NewGateway(registry, nil, nil)
	principal := &tools.Principal{GraphID: g.Metadata.ID, NodeID: "worker", ExecutionID: "execution", Actor: "agent:worker#0", Grants: []domain.ToolGrant{{Name: "environment.propose"}, {Name: "environment.status"}}}
	if status, err := gateway.Invoke(ctx, principal, "environment.status", json.RawMessage(`{}`)); err != nil || status.IsError {
		t.Fatal(status, err)
	}
	done := make(chan *tools.Result, 1)
	go func() {
		res, err := gateway.Invoke(ctx, principal, "environment.propose", json.RawMessage(agentProposalArgs))
		if err != nil {
			res = &tools.Result{IsError: true, Content: err.Error()}
		}
		done <- res
	}()
	var proposal *Proposal
	for proposal == nil || proposal.State != "awaiting_approval" {
		select {
		case result := <-done:
			t.Fatal("returned before approval", result)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
		var err error
		proposal, err = p.Get(ctx, g.Metadata.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	before, err := p.Manager.Get(ctx, g)
	if err != nil || before.State == StateBuilding || before.State == StateReady {
		t.Fatal("build before approval", before, err)
	}
	approval, err := p.Store.GetApproval(ctx, proposal.ID)
	for errors.Is(err, storage.ErrNotFound) && ctx.Err() == nil {
		time.Sleep(10 * time.Millisecond)
		approval, err = p.Store.GetApproval(ctx, proposal.ID)
	}
	if err != nil || approval.ExecutionID != "execution" || approval.NodeID != "worker" {
		t.Fatal(approval, err)
	}
	if err := p.Check(ctx, g); err == nil {
		t.Fatal("task gate missing")
	}
	switch decision {
	case "timeout":
		cancel()
	case "expired":
		past := time.Now().Add(-time.Second)
		approval.ExpiresAt = &past
		if _, err := p.Approvals.Expire(ctx, approval); err != nil {
			t.Fatal(err)
		}
	case "buildfail":
		if err := os.WriteFile(filepath.Join(filepath.Dir(bin), "fail-build"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Approvals.Decide(ctx, proposal.ID, true, "developer", "", ""); err != nil {
			t.Fatal(err)
		}
	case "stale":
		writeDiscoveryFile(t, dir, "package.json", `{"changed":true}`)
		if err := p.ValidateDecision(ctx, approval); err == nil {
			t.Fatal("stale proposal accepted")
		}
		cancel()
	default:
		if err := p.ValidateDecision(ctx, approval); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Approvals.Decide(ctx, proposal.ID, decision == "approve", "developer", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	for {
		if ctx.Err() == nil {
			p.Reconcile(ctx)
		}
		select {
		case result := <-done:
			if result == nil || result.IsError != (decision != "approve") {
				t.Fatal(result)
			}
			if decision == "buildfail" {
				if result == nil || !result.IsError {
					t.Fatal(result)
				}
				return
			}
			if decision != "approve" {
				saved, err := p.Store.GetGraph(context.Background(), g.Metadata.ID, 0)
				if err != nil || saved.Spec.Environment != nil {
					t.Fatal("nonapproved definition applied", saved, err)
				}
				return
			}
			saved, err := p.Store.GetGraph(ctx, g.Metadata.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Check(ctx, saved); err != nil {
				t.Fatal(err)
			}
			view, err := p.Manager.Get(ctx, saved)
			if err != nil || !view.UpToDate {
				t.Fatal(view, err)
			}
			if os.Getenv("WEFT_TEST_ENV_DOCKER_BIN") == bin {
				out, err := tools.DockerCommand(ctx, bin, "", "run", "--rm", "--network", "none", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--entrypoint", "sh", view.Image, "-ec", `test "$(id -u)" = 10001; test "$(cat /proof)" = proposal-ok; test -s /manifest`).CombinedOutput()
				if err != nil {
					t.Fatal(string(out), err)
				}
				if err := p.Manager.Reset(ctx, g.Metadata.ID, "test-cleanup"); err != nil {
					t.Fatal(err)
				}
			}
			return
		case <-time.After(20 * time.Millisecond):
			if ctx.Err() != nil && decision == "approve" {
				t.Fatal(ctx.Err())
			}
		}
	}
}
func TestAgentProposalIntegration(t *testing.T) {
	for _, decision := range []string{"approve", "reject", "timeout", "stale", "expired", "buildfail"} {
		t.Run(decision, func(t *testing.T) { exerciseAgentProposal(t, newFakeDocker(t).bin, decision) })
	}
}
func TestAgentProposalRealSandbox(t *testing.T) {
	bin := os.Getenv("WEFT_TEST_ENV_DOCKER_BIN")
	if bin == "" {
		t.Skip("run make test-environment-context")
	}
	exerciseAgentProposal(t, bin, "approve")
}
func TestAgentToolInvalidArguments(t *testing.T) {
	for _, args := range []string{`{"graphId":"other"}`, `{} {}`, `{"environment":null}`, `{"environment":{"image":"alpine","cpu":"10"},"rationale":"why","tools":[]}`} {
		p, g, _ := proposalFixture(t, newFakeDocker(t).bin)
		g.Spec.Nodes = []domain.Node{{ID: "worker", Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}}}
		if _, err := p.Store.SaveGraph(context.Background(), g, g.Metadata.Version); err != nil {
			t.Fatal(err)
		}
		_, err := p.agentPropose(context.Background(), &tools.Invocation{Principal: &tools.Principal{GraphID: g.Metadata.ID, NodeID: "worker"}, Args: json.RawMessage(args)})
		if err == nil {
			t.Fatal(args)
		}
		v, err := p.Get(context.Background(), g.Metadata.ID)
		if err != nil || v != nil {
			t.Fatal("invalid input created proposal", v, err)
		}
	}
}

func TestAgentProposalScopeAndPolicy(t *testing.T) {
	p, g, dir := proposalFixture(t, newFakeDocker(t).bin)
	g.Spec.Nodes = []domain.Node{{ID: "worker", Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}}}
	principal := &tools.Principal{GraphID: g.Metadata.ID, NodeID: "worker"}
	makeAnalysis := func() *Analysis {
		var a Analysis
		if err := json.Unmarshal([]byte(agentProposalArgs), &a); err != nil {
			t.Fatal(err)
		}
		return &a
	}
	bad := *principal
	bad.GraphID = "other"
	if _, err := p.RequestDefinition(context.Background(), g, makeAnalysis(), &bad); !errors.Is(err, tools.ErrDenied) {
		t.Fatal(err)
	}
	p.Manager.opt.Policy.DisableAgentProposals = true
	if _, err := p.RequestDefinition(context.Background(), g, makeAnalysis(), principal); err == nil {
		t.Fatal("installation policy bypass")
	}
	p.Manager.opt.Policy.DisableAgentProposals = false
	writeDiscoveryFile(t, dir, "Dockerfile", "FROM alpine:3.22\n")
	if _, err := p.RequestDefinition(context.Background(), g, makeAnalysis(), principal); err == nil {
		t.Fatal("repository discovery priority bypass")
	}
}
