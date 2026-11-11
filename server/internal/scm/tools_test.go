package scm_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

// gateway wires the SCM tools into a real tool gateway.
func gateway(t *testing.T, m scm.Manager) *tools.Gateway {
	t.Helper()
	reg := tools.NewRegistry()
	reg.MustRegister(scm.Tools(m)...)
	return tools.NewGateway(reg, nil, nil)
}

func principal(work string, grants ...domain.ToolGrant) *tools.Principal {
	return &tools.Principal{GraphID: "task-run-1", NodeID: "integrator", ExecutionID: "x1", Workdir: work, Grants: grants}
}

func TestGitPushThroughTheGateway(t *testing.T) {
	m, st := manager(t)
	m.Bus = events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	work, remote := repo(t)
	if _, err := m.Save(ctx, scm.Input{Name: "app", Kind: "fake", Repository: "acme/app", GitURL: "file://" + remote, Token: ptr(token)}, "admin", false); err != nil {
		t.Fatal(err)
	}
	gw := gateway(t, m)
	args := json.RawMessage(`{"connection":"app"}`)

	// Not granted at all, granted through a wildcard without repositories,
	// or granted for another repository: denied.
	for _, grants := range [][]domain.ToolGrant{nil, {{Name: "git.*"}}, {{Name: "git.push", Repositories: []string{"other"}}}} {
		if _, err := gw.Invoke(ctx, principal(work, grants...), "git.push", args); !errors.Is(err, tools.ErrDenied) {
			t.Fatalf("grants %+v: %v", grants, err)
		}
	}
	grant := domain.ToolGrant{Name: "git.push", Repositories: []string{"app"}}
	// On the base branch: refused.
	res, err := gw.Invoke(ctx, principal(work, grant), "git.push", args)
	if err != nil || !res.IsError || !strings.Contains(res.Content, `only branches starting with "weft/"`) {
		t.Fatalf("base branch: %+v %v", res, err)
	}
	git(t, work, "checkout", "-q", "-b", "weft/task-1")
	if err := os.WriteFile(filepath.Join(work, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", "b.txt")
	git(t, work, "commit", "-q", "-m", "change")
	res, err = gw.Invoke(ctx, principal(work, grant), "git.push", args)
	if err != nil || res.IsError || !strings.Contains(res.Content, "Pushed weft/task-1 to acme/app") {
		t.Fatalf("push: %+v %v", res, err)
	}
	if git(t, remote, "rev-parse", "refs/heads/weft/task-1") != git(t, work, "rev-parse", "HEAD") {
		t.Fatal("branch not on the remote")
	}
	// Naming the base branch explicitly is refused as well.
	res, _ = gw.Invoke(ctx, principal(work, grant), "git.push", json.RawMessage(`{"connection":"app","branch":"main"}`))
	if !res.IsError {
		t.Fatalf("explicit base branch: %+v", res)
	}
	evs, _ := st.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvBranchPushed}})
	if len(evs) != 1 || evs[0].GraphID != "task-run-1" || evs[0].ExecutionID != "x1" || strings.Contains(string(evs[0].Data), token) {
		t.Fatalf("push events = %+v", evs)
	}
	// A token without push access is reported before any Git command runs.
	readOnly = true
	defer func() { readOnly = false }()
	res, _ = gw.Invoke(ctx, principal(work, grant), "git.push", args)
	if !res.IsError || !strings.Contains(res.Content, "cannot push") {
		t.Fatalf("read-only token: %+v", res)
	}
	// A detached HEAD has no task branch to push.
	readOnly = false
	git(t, work, "checkout", "-q", "--detach")
	res, _ = gw.Invoke(ctx, principal(work, grant), "git.push", args)
	if !res.IsError || !strings.Contains(res.Content, "detached") {
		t.Fatalf("detached: %+v", res)
	}
	// Unknown connection.
	res, _ = gw.Invoke(ctx, principal(work, domain.ToolGrant{Name: "git.push", Repositories: []string{"gone"}}), "git.push", json.RawMessage(`{"connection":"gone"}`))
	if !res.IsError {
		t.Fatalf("unknown connection: %+v", res)
	}
}

type approver struct {
	approve bool
	asked   []*domain.Approval
}

func (a *approver) RequestApproval(_ context.Context, ap *domain.Approval) (*domain.Approval, error) {
	a.asked = append(a.asked, ap)
	out := *ap
	out.Status = domain.ApprovalRejected
	if a.approve {
		out.Status = domain.ApprovalApproved
	}
	return &out, nil
}

func TestPullRequestNeedsApprovalAndIsIdempotentPerBranch(t *testing.T) {
	openPRs = nil
	m, st := manager(t)
	m.Bus = events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	work, remote := repo(t)
	if _, err := m.Save(ctx, scm.Input{Name: "app", Kind: "fake", Repository: "acme/app", GitURL: "file://" + remote, Token: ptr(token)}, "admin", false); err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	reg.MustRegister(scm.Tools(m)...)
	ap := &approver{}
	gw := tools.NewGateway(reg, ap, nil)
	grant := domain.ToolGrant{Name: "scm.pull_request.create", Repositories: []string{"app"}}
	call := func(args string) (*tools.Result, error) {
		return gw.Invoke(ctx, principal(work, grant), "scm.pull_request.create", json.RawMessage(args))
	}
	// Repository grants apply as for push.
	if _, err := gw.Invoke(ctx, principal(work, domain.ToolGrant{Name: "scm.*"}), "scm.pull_request.create", json.RawMessage(`{"connection":"app","title":"x"}`)); !errors.Is(err, tools.ErrDenied) {
		t.Fatalf("no repositories: %v", err)
	}
	for args, want := range map[string]string{
		`{"connection":"app","title":"   "}`:                                                    "title",
		`{"connection":"app","title":"a\nb"}`:                                                   "title",
		`{"connection":"app","title":"` + strings.Repeat("x", 257) + `"}`:                       "title",
		`{"connection":"app","title":"ok","body":"` + strings.Repeat("x", scm.MaxBody+1) + `"}`: "body",
		`{"connection":"app","title":"ok"}`:                                                     "starting with",
		`{"connection":"app","title":"ok","branch":"main"}`:                                     "starting with",
	} {
		res, err := call(args)
		if err != nil || !res.IsError || !strings.Contains(res.Content, want) {
			t.Fatalf("%.60s: %+v %v", args, res, err)
		}
	}
	git(t, work, "checkout", "-q", "-b", "weft/task-1")
	// A rejected approval opens nothing.
	if res, err := call(`{"connection":"app","title":"Fix the parser"}`); err != nil || !res.IsError || !strings.Contains(res.Content, "not approved") || len(openPRs) != 0 {
		t.Fatalf("rejected: %+v %v %+v", res, err, last)
	}
	if len(ap.asked) != 1 || !strings.Contains(ap.asked[0].Title, "acme/app: Fix the parser") || ap.asked[0].ExecutionID != "x1" {
		t.Fatalf("approval request: %+v", ap.asked)
	}
	ap.approve = true
	res, err := call(`{"connection":"app","title":"Fix the parser","body":"Details"}`)
	if err != nil || res.IsError || !strings.Contains(res.Content, "Opened pull request #1") {
		t.Fatalf("open: %+v %v", res, err)
	}
	var pr scm.PullRequest
	if json.Unmarshal(res.Data, &pr) != nil || pr.Head != "weft/task-1" || pr.Base != "main" || !pr.Created {
		t.Fatalf("pr = %+v", pr)
	}
	// The same branch returns the existing pull request without asking again.
	res, err = call(`{"connection":"app","title":"Again"}`)
	if err != nil || !strings.Contains(res.Content, "already exists") || len(ap.asked) != 2 {
		t.Fatalf("idempotent: %+v %v asked %d", res, err, len(ap.asked))
	}
	evs, _ := st.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvPullRequestOpened}})
	if len(evs) != 2 || !strings.Contains(string(evs[0].Data), "https://scm.example/pr/1") {
		t.Fatalf("events = %+v", evs)
	}
	// Without an approver the call is denied, unless the connection
	// explicitly relaxes approval.
	noApprover := tools.NewGateway(reg, nil, nil)
	git(t, work, "checkout", "-q", "-b", "weft/task-2")
	if _, err := noApprover.Invoke(ctx, principal(work, grant), "scm.pull_request.create", json.RawMessage(`{"connection":"app","title":"Two"}`)); !errors.Is(err, tools.ErrDenied) {
		t.Fatalf("no approver: %v", err)
	}
	if _, err := m.Save(ctx, scm.Input{Name: "app", Kind: "fake", Repository: "acme/app", GitURL: "file://" + remote, RequirePullRequestApproval: ptr(false)}, "admin", true); err != nil {
		t.Fatal(err)
	}
	if res, err := noApprover.Invoke(ctx, principal(work, grant), "scm.pull_request.create", json.RawMessage(`{"connection":"app","title":"Two"}`)); err != nil || res.IsError {
		t.Fatalf("relaxed: %+v %v", res, err)
	}
}
