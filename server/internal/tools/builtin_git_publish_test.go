package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func TestGitPublishRequiresExactGrantAndActiveWritableReplica(t *testing.T) {
	reg := Builtin(BuiltinConfig{})
	tool, ok := reg.Get(GitPublish)
	if !ok {
		t.Fatal("missing publication tool")
	}
	gateway := NewGateway(reg, nil, nil)
	commit := strings.Repeat("c", 40)
	args := json.RawMessage(`{"commit":"` + commit + `","previous":""}`)
	p := principal(t.TempDir())
	calls := 0
	p.PublishBranch = func(_ context.Context, req workspace.BranchPublication) (workspace.PublishedBranch, error) {
		calls++
		if req.Commit != commit || req.Previous != "" {
			t.Fatalf("request %+v", req)
		}
		return workspace.PublishedBranch{Branch: "dev-r0", Commit: commit}, nil
	}
	offered := func() bool {
		for _, s := range reg.Schemas(p) {
			if s.Name == GitPublish {
				return true
			}
		}
		return false
	}
	for _, grant := range []string{"git.write", "git.read", "git.*", "*"} {
		p.Grants = []domain.ToolGrant{{Name: grant}}
		if offered() {
			t.Fatalf("offered under %q", grant)
		}
		if _, err := gateway.Invoke(t.Context(), p, GitPublish, args); !errors.Is(err, ErrDenied) {
			t.Fatalf("%q: %v", grant, err)
		}
	}
	p.Grants = []domain.ToolGrant{{Name: "*"}, {Name: GitPublish, RequiresApproval: true}}
	merged, err := Authorize(p, tool)
	if err != nil || !merged.RequiresApproval {
		t.Fatalf("lost mandatory approval: %+v %v", merged, err)
	}
	p.Grants = []domain.ToolGrant{{Name: GitPublish}}
	p.WorkspaceReadOnly = true
	if offered() {
		t.Fatal("offered to read-only clone")
	}
	if _, err := gateway.Invoke(t.Context(), p, GitPublish, args); !errors.Is(err, ErrDenied) {
		t.Fatalf("read-only: %v", err)
	}
	p.WorkspaceReadOnly = false
	if !offered() {
		t.Fatal("missing from granted replica schemas")
	}
	for _, extra := range []string{"branch", "remote", "path", "node", "graph", "replica", "repository"} {
		bad := json.RawMessage(strings.TrimSuffix(string(args), "}") + `,"` + extra + `":"other"}`)
		res, err := gateway.Invoke(t.Context(), p, GitPublish, bad)
		if err == nil && !res.IsError {
			t.Fatalf("accepted %s override", extra)
		}
	}
	if calls != 0 {
		t.Fatalf("%d unauthorized publications", calls)
	}
	res, err := gateway.Invoke(t.Context(), p, GitPublish, args)
	if err != nil || res.IsError || calls != 1 {
		t.Fatalf("granted: %+v %v", res, err)
	}
	p.PublishBranch = nil
	if offered() {
		t.Fatal("offered without publication service")
	}
	res, err = gateway.Invoke(t.Context(), p, GitPublish, args)
	if err == nil && !res.IsError {
		t.Fatal("published without active private replica")
	}
}
