package scm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/tools"
)

// Capabilities of the SCM gateway tools.
const (
	CapPush        = "scm.push"
	CapPullRequest = "scm.pull_request"
)

// Tools returns the SCM gateway tools backed by m.
func Tools(m Manager) []tools.Tool {
	return []tools.Tool{pushTool{m: m}, pullRequestTool{m: m}}
}

type pushTool struct{ m Manager }

func (pushTool) Name() string       { return "git.push" }
func (pushTool) Capability() string { return CapPush }
func (pushTool) Description() string {
	return "Push the current task branch to a granted SCM connection. Only branches under the connection's prefix (default weft/) are pushed; never the base branch, tags or a forced update."
}
func (pushTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"connection":{"type":"string","description":"SCM connection name"},"branch":{"type":"string","description":"Branch to push (default: the current branch)"}},"required":["connection"],"additionalProperties":false}`)
}

// currentBranch reads the work tree's branch with Weft's own Git.
func currentBranch(ctx context.Context, workdir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir, cmd.Env = workdir, gitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("the workspace is not a Git work tree")
	}
	b := strings.TrimSpace(string(out))
	if b == "HEAD" {
		return "", errors.New("the workspace is on a detached HEAD; create a task branch first")
	}
	return b, nil
}

func (t pushTool) Invoke(ctx context.Context, inv *tools.Invocation) (*tools.Result, error) {
	var args struct{ Connection, Branch string }
	if err := json.Unmarshal(inv.Args, &args); err != nil {
		return nil, fmt.Errorf("git.push: %w", err)
	}
	if err := tools.CheckRepository(inv.Grant, args.Connection); err != nil {
		return nil, err
	}
	c, err := t.m.Get(ctx, args.Connection)
	if err != nil {
		return &tools.Result{Content: err.Error(), IsError: true}, nil
	}
	if inv.Workdir == "" {
		return &tools.Result{Content: "git.push needs a workspace", IsError: true}, nil
	}
	branch := args.Branch
	if branch == "" {
		if branch, err = currentBranch(ctx, inv.Workdir); err != nil {
			return &tools.Result{Content: err.Error(), IsError: true}, nil
		}
	}
	p, token, err := t.m.Open(c)
	if err != nil {
		return &tools.Result{Content: err.Error(), IsError: true}, nil
	}
	// The base branch comes from the provider, so a push can never target
	// it even when the agent names it.
	id, err := p.Identity(ctx)
	if err != nil {
		return &tools.Result{Content: "the SCM connection could not be checked: " + redact(err, token), IsError: true}, nil
	}
	if !id.CanPush {
		return &tools.Result{Content: fmt.Sprintf("the token of %q cannot push to %s", c.Name, c.Repository), IsError: true}, nil
	}
	out, err := Push(ctx, c, p, token, inv.Workdir, branch, id.DefaultBranch)
	if err != nil {
		return &tools.Result{Content: err.Error(), IsError: true}, nil
	}
	t.m.event(ctx, inv.Principal, domain.EvBranchPushed, map[string]any{"connection": c.Name, "repository": c.Repository, "branch": branch})
	data, _ := json.Marshal(map[string]string{"connection": c.Name, "repository": c.Repository, "branch": branch})
	return &tools.Result{Content: fmt.Sprintf("Pushed %s to %s (%s).\n%s", branch, c.Repository, c.Name, out), Data: data}, nil
}

// Bounds of a pull request an agent may open.
const (
	MaxTitle = 256
	MaxBody  = 16 << 10
)

type pullRequestTool struct{ m Manager }

func (pullRequestTool) Name() string       { return "scm.pull_request.create" }
func (pullRequestTool) Capability() string { return CapPullRequest }
func (pullRequestTool) Description() string {
	return "Open a pull request from the pushed task branch into the repository's base branch. Returns the existing open pull request for the branch instead of opening a second one. A person approves it first unless the connection relaxes that."
}
func (pullRequestTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"connection":{"type":"string"},"title":{"type":"string","maxLength":256},"body":{"type":"string","maxLength":16384},"branch":{"type":"string","description":"Head branch (default: the current branch)"}},"required":["connection","title"],"additionalProperties":false}`)
}

func (t pullRequestTool) Invoke(ctx context.Context, inv *tools.Invocation) (*tools.Result, error) {
	var args struct{ Connection, Title, Body, Branch string }
	if err := json.Unmarshal(inv.Args, &args); err != nil {
		return nil, fmt.Errorf("scm.pull_request.create: %w", err)
	}
	if err := tools.CheckRepository(inv.Grant, args.Connection); err != nil {
		return nil, err
	}
	args.Title = strings.TrimSpace(args.Title)
	if args.Title == "" || len(args.Title) > MaxTitle || strings.ContainsAny(args.Title, "\r\n") {
		return &tools.Result{Content: fmt.Sprintf("the title must be one line of 1-%d characters", MaxTitle), IsError: true}, nil
	}
	if len(args.Body) > MaxBody {
		return &tools.Result{Content: fmt.Sprintf("the body must be at most %d bytes", MaxBody), IsError: true}, nil
	}
	c, err := t.m.Get(ctx, args.Connection)
	if err != nil {
		return &tools.Result{Content: err.Error(), IsError: true}, nil
	}
	head := args.Branch
	if head == "" {
		if inv.Workdir == "" {
			return &tools.Result{Content: "name the branch: there is no workspace to read it from", IsError: true}, nil
		}
		if head, err = currentBranch(ctx, inv.Workdir); err != nil {
			return &tools.Result{Content: err.Error(), IsError: true}, nil
		}
	}
	if !validBranch(head) || !strings.HasPrefix(head, c.Prefix()) {
		return &tools.Result{Content: fmt.Sprintf("pull requests are opened only from branches starting with %q", c.Prefix()), IsError: true}, nil
	}
	p, token, err := t.m.Open(c)
	if err != nil {
		return &tools.Result{Content: err.Error(), IsError: true}, nil
	}
	id, err := p.Identity(ctx)
	if err != nil {
		return &tools.Result{Content: "the SCM connection could not be checked: " + redact(err, token), IsError: true}, nil
	}
	base := id.DefaultBranch
	if base == "" {
		return &tools.Result{Content: "the repository reports no default branch to open the pull request into", IsError: true}, nil
	}
	// Idempotent per branch: an open pull request is returned as is.
	if pr, err := p.FindPullRequest(ctx, head, base); err != nil {
		return &tools.Result{Content: "could not look for an existing pull request: " + redact(err, token), IsError: true}, nil
	} else if pr != nil {
		pr.Created = false
		return t.result(ctx, inv, c, pr), nil
	}
	if c.ApprovalRequired() {
		if err := approve(ctx, inv, c, head, base, args.Title); err != nil {
			return nil, err
		}
	}
	if c.Kind == "github" && strings.HasPrefix(inv.Principal.GraphID, domain.TaskRunGraphPrefix) {
		taskID := "tsk_" + strings.TrimPrefix(inv.Principal.GraphID, domain.TaskRunGraphPrefix)
		draft, err := (taskconfig.Manager{Store: t.m.Store}).Get(ctx, taskID)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return &tools.Result{Content: "the admitted issue identity could not be verified", IsError: true}, nil
		}
		if err == nil && draft.ExecutionGraphID == inv.Principal.GraphID && draft.TriggerGitHubIssue != "" {
			args.Body = appendClosingIssue(args.Body, c.Repository, draft.TriggerGitHubIssue)
			if len(args.Body) > MaxBody {
				return &tools.Result{Content: "shorten the pull request body to leave room for the trigger issue reference", IsError: true}, nil
			}
		}
	}
	pr, err := p.CreatePullRequest(ctx, PullRequestInput{Title: args.Title, Body: args.Body, Head: head, Base: base})
	if err != nil {
		return &tools.Result{Content: "the pull request could not be opened: " + redact(err, token), IsError: true}, nil
	}
	return t.result(ctx, inv, c, pr), nil
}

func appendClosingIssue(body, repository, issue string) string {
	issueRepository, number, _ := strings.Cut(issue, "#")
	reference := issue
	if strings.EqualFold(repository, issueRepository) {
		reference = "#" + number
	}
	line := "Closes " + reference
	if strings.TrimSpace(body) == "" {
		return line
	}
	return strings.TrimSpace(body) + "\n\n" + line
}

func (t pullRequestTool) result(ctx context.Context, inv *tools.Invocation, c Connection, pr *PullRequest) *tools.Result {
	t.m.event(ctx, inv.Principal, domain.EvPullRequestOpened, map[string]any{"connection": c.Name, "repository": c.Repository,
		"number": pr.Number, "url": pr.URL, "head": pr.Head, "base": pr.Base, "created": pr.Created})
	data, _ := json.Marshal(pr)
	verb := "Opened"
	if !pr.Created {
		verb = "An open pull request already exists:"
	}
	return &tools.Result{Content: fmt.Sprintf("%s pull request #%d %s (%s into %s).", verb, pr.Number, pr.URL, pr.Head, pr.Base), Data: data}
}

// approve asks a person before a pull request is opened, whatever the task
// authorization mode, unless the connection explicitly relaxes it.
func approve(ctx context.Context, inv *tools.Invocation, c Connection, head, base, title string) error {
	if inv.Approver == nil {
		return fmt.Errorf("%w: opening a pull request needs a person's approval and no approver is configured", tools.ErrDenied)
	}
	p := inv.Principal
	detail, _ := json.Marshal(map[string]string{"tool": "scm.pull_request.create", "connection": c.Name, "repository": c.Repository, "head": head, "base": base, "title": title})
	a := &domain.Approval{ID: domain.NewID("apr"), GraphID: p.GraphID, NodeID: p.NodeID, Replica: p.Replica, ExecutionID: p.ExecutionID,
		Kind: domain.ApprovalTool, Status: domain.ApprovalPending, Title: fmt.Sprintf("Open a pull request on %s: %s", c.Repository, title),
		Detail: detail, CreatedAt: time.Now().UTC()}
	decided, err := inv.Approver.RequestApproval(ctx, a)
	if err != nil {
		return fmt.Errorf("%w: approval failed: %v", tools.ErrRejected, err)
	}
	if decided == nil || decided.Status != domain.ApprovalApproved {
		return fmt.Errorf("%w: opening the pull request was not approved", tools.ErrRejected)
	}
	return nil
}
