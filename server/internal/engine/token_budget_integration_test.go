package engine_test

import (
	"github.com/woliveiras/weft/server/internal/delegation"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/executioncontrol"
	"testing"
	"time"
)

func TestTreeBudgetSuccessCountsCallsOnce(t *testing.T) {
	r, _ := delegationRig(t, "")
	m := delegation.Manager{Store: r.store}
	policy := delegation.DefaultPolicy()
	policy.ApprovalMode = "automatic"
	if _, e := m.SavePolicy(r.ctx, policy, 0, "admin"); e != nil {
		t.Fatal(e)
	}
	deployDelegation(r, "[delegation.request]", "{turnTimeout: 10s}")
	parent := finishedParent(t, r)
	v, e := m.Budget(r.ctx, parent.ID)
	if e != nil || v == nil || v.Reserved != 0 || v.Estimated != 0 || v.Charged != 16 || v.Charged != parent.Usage.InputTokens+parent.Usage.OutputTokens {
		t.Fatal(v, e, parent)
	}
}
func TestTreeBudgetReleasesRejectedAndCancelledReservations(t *testing.T) {
	for _, mode := range []string{"reject", "child", "tree", "inference"} {
		t.Run(mode, func(t *testing.T) {
			r, p := delegationRig(t, "timeout")
			m := delegation.Manager{Store: r.store}
			deployDelegation(r, "[delegation.request]", "{turnTimeout: 10s}")
			a := delegationApproval(t, r)
			child, e := r.store.GetExecution(r.ctx, a.ExecutionID)
			if e != nil {
				t.Fatal(e)
			}
			before, e := m.Budget(r.ctx, child.ParentExecutionID)
			if e != nil || before.Reserved <= 0 || before.Charged != 3 {
				t.Fatal(before, e)
			}
			if mode == "reject" {
				if _, e = r.apr.Decide(r.ctx, a.ID, false, "operator", "", ""); e != nil {
					t.Fatal(e)
				}
			} else {
				if mode == "inference" {
					if _, e = r.apr.Decide(r.ctx, a.ID, true, "operator", "", ""); e != nil {
						t.Fatal(e)
					}
					eventually(t, time.Second, "inference started", func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.children == 1 })
				}
				scope := "child"
				if mode == "tree" {
					scope = "tree"
				}
				if _, e = (executioncontrol.Manager{Store: r.store}).Cancel(r.ctx, a.ExecutionID, scope, "operator"); e != nil {
					t.Fatal(e)
				}
			}
			parent := finishedParent(t, r)
			v, e := m.Budget(r.ctx, parent.ID)
			if e != nil || v.Reserved != 0 {
				t.Fatal(v, e)
			}
			if mode == "inference" {
				if v.Estimated != before.Reserved || v.Charged != before.Reserved+6 {
					t.Fatal(v, before)
				}
			} else if v.Estimated != 0 || (mode == "tree" && v.Charged != 3) || (mode != "tree" && v.Charged != 6) {
				t.Fatal(v)
			}
		})
	}
}
func TestTreeBudgetBlocksBeforeModelAndAccountsFailure(t *testing.T) {
	for _, mode := range []string{"exhausted", "failure"} {
		t.Run(mode, func(t *testing.T) {
			r, p := delegationRig(t, "failure")
			m := delegation.Manager{Store: r.store}
			policy := delegation.DefaultPolicy()
			policy.ApprovalMode = "automatic"
			if mode == "exhausted" {
				policy.MaxTreeTokens = 1
			}
			if _, e := m.SavePolicy(r.ctx, policy, 0, "admin"); e != nil {
				t.Fatal(e)
			}
			deployDelegation(r, "[delegation.request]", "{turnTimeout: 10s}")
			parent := finishedParent(t, r)
			if mode == "exhausted" {
				if parent.Status != domain.ExecFailed {
					t.Fatal(parent)
				}
				p.mu.Lock()
				defer p.mu.Unlock()
				if p.children != 0 {
					t.Fatal("child ran")
				}
				return
			}
			v, e := m.Budget(r.ctx, parent.ID)
			if e != nil || v.Reserved != 0 || v.Estimated <= 0 || v.Charged != v.Estimated+6 {
				t.Fatal(v, e)
			}
		})
	}
}
