package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func checkpointRegistry(restored *[]*Invocation) *Registry {
	reg := Builtin(BuiltinConfig{})
	ok := func(_ context.Context, inv *Invocation) (*Result, error) {
		*restored = append(*restored, inv)
		return textResult("done"), nil
	}
	reg.MustRegister(CheckpointTools(ok, ok)...)
	return reg
}

func TestCheckpointToolsNeedAGrantNamingThem(t *testing.T) {
	var calls []*Invocation
	reg := checkpointRegistry(&calls)
	restore, _ := reg.Get("checkpoint.restore")
	list, _ := reg.Get("checkpoint.list")
	for _, wildcard := range []string{"*", "checkpoint.*", "check*"} {
		p := principal("/w", domain.ToolGrant{Name: wildcard})
		if len(MatchingGrants(p, restore)) != 0 || len(MatchingGrants(p, list)) != 0 {
			t.Fatalf("wildcard %q granted a checkpoint tool", wildcard)
		}
	}
	if len(MatchingGrants(principal("/w", domain.ToolGrant{Name: "checkpoint.restore"}), restore)) != 1 {
		t.Fatal("explicit grant did not match")
	}
	for _, s := range reg.Schemas(principal("/w", domain.ToolGrant{Name: "*"})) {
		if s.Name == "checkpoint.restore" || s.Name == "checkpoint.list" {
			t.Fatalf("%s offered under a wildcard grant", s.Name)
		}
	}

	gw := NewGateway(reg, nil, &captureAuditor{})
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := gw.Invoke(ctx, principal(dir, domain.ToolGrant{Name: "*"}), "checkpoint.restore", nil); !errors.Is(err, ErrDenied) {
		t.Fatalf("wildcard restore: %v", err)
	}
	ro := principal(dir, domain.ToolGrant{Name: "checkpoint.restore"})
	ro.WorkspaceReadOnly = true
	if _, err := gw.Invoke(ctx, ro, "checkpoint.restore", nil); !errors.Is(err, ErrDenied) {
		t.Fatalf("read-only workspace restore: %v", err)
	}
	if len(calls) != 0 {
		t.Fatal("a refused call reached the tool")
	}
	res, err := gw.Invoke(ctx, principal(dir, domain.ToolGrant{Name: "checkpoint.restore"}), "checkpoint.restore", json.RawMessage(`{"checkpoint":"dev/r1/2"}`))
	if err != nil || res.IsError || len(calls) != 1 || string(calls[0].Args) != `{"checkpoint":"dev/r1/2"}` {
		t.Fatalf("granted restore: %+v %v", res, err)
	}
	// The scope comes from the principal: without one the tool refuses.
	anon := &Principal{Workdir: dir, Grants: []domain.ToolGrant{{Name: "checkpoint.list"}}}
	if _, err := gw.Invoke(ctx, anon, "checkpoint.list", nil); !errors.Is(err, ErrDenied) {
		t.Fatalf("anonymous list: %v", err)
	}
}

func TestBeforeMutationRunsOnlyBeforeAuthorizedWorkspaceChanges(t *testing.T) {
	var calls []*Invocation
	gw := NewGateway(checkpointRegistry(&calls), nil, &captureAuditor{})
	ctx := context.Background()
	dir := t.TempDir()
	var seen []string
	p := principal(dir, domain.ToolGrant{Name: "filesystem.*"}, domain.ToolGrant{Name: "checkpoint.restore"})
	p.BeforeMutation = func(_ context.Context, tool string) { seen = append(seen, tool) }

	mustInvoke := func(name, args string) {
		t.Helper()
		if res, err := gw.Invoke(ctx, p, name, json.RawMessage(args)); err != nil || res.IsError {
			t.Fatalf("%s: %+v %v", name, res, err)
		}
	}
	mustInvoke("filesystem.list", `{"path":"."}`)
	mustInvoke("filesystem.write", `{"path":"a.txt","content":"x"}`)
	mustInvoke("filesystem.delete", `{"path":"a.txt"}`)
	mustInvoke("checkpoint.restore", `{}`)
	if _, err := gw.Invoke(ctx, p, "shell.exec", json.RawMessage(`{"argv":["true"]}`)); !errors.Is(err, ErrDenied) {
		t.Fatalf("ungranted shell: %v", err)
	}
	if len(seen) != 2 || seen[0] != "filesystem.write" || seen[1] != "filesystem.delete" {
		t.Fatalf("BeforeMutation calls %v", seen)
	}

	// A rejected approval never reaches the hook.
	approval := principal(dir, domain.ToolGrant{Name: "filesystem.write", RequiresApproval: true})
	approval.BeforeMutation = func(_ context.Context, tool string) { seen = append(seen, "approval:"+tool) }
	rejecting := NewGateway(checkpointRegistry(&calls), &fakeApprover{status: domain.ApprovalRejected}, &captureAuditor{})
	if _, err := rejecting.Invoke(ctx, approval, "filesystem.write", json.RawMessage(`{"path":"b","content":"y"}`)); !errors.Is(err, ErrRejected) {
		t.Fatalf("rejected write: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("hook ran for a rejected call: %v", seen)
	}
}
