package engine

import (
	"encoding/json"
	"github.com/woliveiras/weft/server/internal/domain"
	"strings"
	"testing"
	"time"
)

func TestParseDelegation(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `{"template":"../other","objective":"work"}`, `{"template":"analysis","objective":" "}`,
		`{"template":"analysis","objective":"work","tools":["shell.exec"]}`, `{"template":"analysis","objective":"work","model":"other"}`,
		`{"template":"analysis","objective":"work"} {}`, `{"template":"analysis","objective":"` + strings.Repeat("a", 16385) + `"}`} {
		if _, _, err := parseDelegation(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, objective := range []string{"Review this data", strings.Repeat("a", 4096)} {
		raw, _ := json.Marshal(map[string]string{"template": "analysis", "objective": objective})
		got, _, err := parseDelegation(raw)
		if err != nil || got != objective {
			t.Fatalf("%q %v", got, err)
		}
	}
}

func TestDelegationDecisionSnapshot(t *testing.T) {
	at := time.Now().UTC()
	x := &domain.Execution{ID: "child", GraphID: "graph", ApprovalID: "approval", DelegationAudit: &domain.DelegationAudit{Decision: "pending"}}
	a := &domain.Approval{ID: "approval", ExecutionID: "child", GraphID: "graph", Kind: domain.ApprovalDelegation, Status: domain.ApprovalRejected, DecidedBy: "operator", DecidedAt: &at, Answer: "private", Comment: "private"}
	snapshotDelegationDecision(x, a)
	if x.DelegationAudit.Decision != "rejected" || x.DelegationAudit.DecidedBy != "operator" || !x.DelegationAudit.DecidedAt.Equal(at) {
		t.Fatalf("%+v", x.DelegationAudit)
	}
	a.Status = domain.ApprovalApproved
	a.ExecutionID = "other"
	snapshotDelegationDecision(x, a)
	if x.DelegationAudit.Decision != "rejected" {
		t.Fatal("accepted unrelated approval")
	}
	at = at.Add(time.Hour)
	if x.DelegationAudit.DecidedAt.Equal(at) {
		t.Fatal("snapshot aliases decision timestamp")
	}
	raw, err := json.Marshal(x.DelegationAudit)
	if err != nil || strings.Contains(string(raw), "private") {
		t.Fatalf("%s %v", raw, err)
	}
	snapshotDelegationDecision(x, nil)
	snapshotDelegationDecision(&domain.Execution{}, a)
}

func TestDelegationRuntimeAndTreeBudget(t *testing.T) {
	for _, kind := range []domain.RuntimeKind{domain.RuntimeNative, domain.RuntimeExternal, domain.RuntimeEcho, domain.RuntimeKind("unknown")} {
		supported := kind == domain.RuntimeNative || kind == domain.RuntimeExternal
		if delegationRuntime(kind) != supported {
			t.Fatal(kind)
		}
		env := &turnEnv{n: &domain.Node{Runtime: domain.RuntimeRef{Kind: kind}, Tools: []domain.ToolGrant{{Name: "delegation.request"}}}}
		if env.hasTreeBudget() != supported {
			t.Fatal("runtime bypassed shared admission", kind)
		}
		env.n.Tools = []domain.ToolGrant{{Name: "*"}}
		if env.hasTreeBudget() {
			t.Fatal("wildcard became delegation authority")
		}
	}
}
