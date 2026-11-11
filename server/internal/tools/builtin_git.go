package tools

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	capGitRead  = "git.read"
	capGitWrite = "git.write"
)

// Git tools run git through the principal's sandbox, so they behave the same
// under process and docker isolation. Every user-supplied ref or path is
// checked not to start with "-" (option injection) and paths follow "--".
func gitTools() []Tool {
	obj := func(props string, req ...string) []byte {
		r := ""
		if len(req) > 0 {
			r = `,"required":["` + strings.Join(req, `","`) + `"]`
		}
		return schema(`{"type":"object","properties":{` + props + `}` + r + `,"additionalProperties":false}`)
	}
	return []Tool{
		&funcTool{name: "git.status", capability: capGitRead,
			description: "Show the working tree status (branch, staged, unstaged and untracked files).",
			schema:      obj(``), fn: gitStatus},
		&funcTool{name: "git.diff", capability: capGitRead,
			description: "Show changes as a unified diff: unstaged by default, staged with staged=true, or against a ref.",
			schema:      obj(`"staged":{"type":"boolean"},"ref":{"type":"string","description":"Compare the working tree with this commit/branch"},"path":{"type":"string"}`),
			fn:          gitDiff},
		&funcTool{name: "git.log", capability: capGitRead,
			description: "Show recent commits (one line each).",
			schema:      obj(`"n":{"type":"integer","minimum":1,"maximum":500},"ref":{"type":"string"}`),
			fn:          gitLog},
		&funcTool{name: "git.show", capability: capGitRead,
			description: "Show a commit (message, stat and patch) or a file at a revision ('ref:path').",
			schema:      obj(`"ref":{"type":"string"}`, "ref"), fn: gitShow},
		&funcTool{name: "git.branches", capability: capGitRead,
			description: "List local and remote branches with their last commit.",
			schema:      obj(``), fn: gitBranches},
		&funcTool{name: "git.add", capability: capGitWrite,
			description: "Stage files for the next commit.",
			schema:      obj(`"paths":{"type":"array","items":{"type":"string"},"minItems":1}`, "paths"), fn: gitAdd},
		&funcTool{name: "git.commit", capability: capGitWrite,
			description: "Create a commit from the staged changes (or all tracked changes with all=true).",
			schema:      obj(`"message":{"type":"string"},"all":{"type":"boolean"},"author":{"type":"string","description":"'Name <email>'"}`, "message"),
			fn:          gitCommit},
		&funcTool{name: "git.branch", capability: capGitWrite,
			description: "Create a branch (from the current HEAD or 'from'), optionally switching to it.",
			schema:      obj(`"name":{"type":"string"},"from":{"type":"string"},"checkout":{"type":"boolean"}`, "name"), fn: gitBranch},
		&funcTool{name: "git.checkout", capability: capGitWrite,
			description: "Switch to a branch or commit.",
			schema:      obj(`"ref":{"type":"string"}`, "ref"), fn: gitCheckout},
		&funcTool{name: "git.merge", capability: capGitWrite,
			description: "Merge a branch into the current branch.",
			schema:      obj(`"branch":{"type":"string"},"noFF":{"type":"boolean"},"message":{"type":"string"}`, "branch"), fn: gitMerge},
	}
}

func gitRun(ctx context.Context, inv *Invocation, args ...string) (*ExecResult, error) {
	// Hooks and fsmonitor are disabled so repository content an agent wrote
	// can never be executed by a git command it was allowed to run.
	argv := append([]string{"git", "-c", "core.pager=cat", "-c", "color.ui=false", "-c", "safe.directory=*",
		"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}, args...)
	return sandboxOf(inv).Exec(ctx, ExecRequest{Argv: argv, Timeout: 2 * time.Minute, Env: map[string]string{"GIT_PAGER": "cat", "GIT_EDITOR": "true"}})
}

// gitResult turns a git exit status into a tool result.
func gitResult(res *ExecResult, err error) (*Result, error) {
	if err != nil {
		return nil, err
	}
	out := strings.TrimRight(res.Stdout, "\n")
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = out
		}
		return &Result{Content: fmt.Sprintf("git failed (exit %d): %s", res.ExitCode, msg), IsError: true}, nil
	}
	if out == "" {
		out = strings.TrimSpace(res.Stderr)
	}
	if out == "" {
		out = "ok"
	}
	return &Result{Content: out, Truncated: res.Truncated}, nil
}

func safeRef(name, v string) error {
	if v == "" {
		return nil
	}
	if strings.HasPrefix(v, "-") || strings.ContainsAny(v, " \t\n\x00") {
		return fmt.Errorf("invalid %s %q", name, v)
	}
	return nil
}

func gitPath(inv *Invocation, p string) (string, error) {
	_, rel, err := ResolvePath(workdirOf(inv), p)
	if err != nil {
		return "", err
	}
	if err := CheckPath(inv.Grant, rel); err != nil {
		return "", err
	}
	return rel, nil
}

func gitStatus(ctx context.Context, inv *Invocation) (*Result, error) {
	return gitResult(gitRun(ctx, inv, "status", "--short", "--branch"))
}

func gitDiff(ctx context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Staged bool   `json:"staged"`
		Ref    string `json:"ref"`
		Path   string `json:"path"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if err := safeRef("ref", a.Ref); err != nil {
		return nil, err
	}
	args := []string{"diff", "--no-color", "--no-ext-diff"}
	if a.Staged {
		args = append(args, "--cached")
	}
	if a.Ref != "" {
		args = append(args, a.Ref)
	}
	if a.Path != "" {
		rel, err := gitPath(inv, a.Path)
		if err != nil {
			return nil, err
		}
		args = append(args, "--", rel)
	}
	r, err := gitResult(gitRun(ctx, inv, args...))
	if err == nil && r.Content == "ok" {
		r.Content = "no differences"
	}
	return r, err
}

func gitLog(ctx context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		N   int    `json:"n"`
		Ref string `json:"ref"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if a.N <= 0 {
		a.N = 20
	}
	if err := safeRef("ref", a.Ref); err != nil {
		return nil, err
	}
	args := []string{"log", "-n", strconv.Itoa(a.N), "--date=short", "--pretty=format:%h %ad %an %s"}
	if a.Ref != "" {
		args = append(args, a.Ref, "--")
	}
	return gitResult(gitRun(ctx, inv, args...))
}

func gitShow(ctx context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Ref string `json:"ref"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if a.Ref == "" {
		return nil, errors.New("ref is required")
	}
	if err := safeRef("ref", a.Ref); err != nil {
		return nil, err
	}
	return gitResult(gitRun(ctx, inv, "show", "--stat", "--patch", "--no-color", a.Ref))
}

func gitBranches(ctx context.Context, inv *Invocation) (*Result, error) {
	return gitResult(gitRun(ctx, inv, "branch", "-a", "-vv", "--no-color"))
}

func gitAdd(ctx context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Paths []string `json:"paths"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if len(a.Paths) == 0 {
		return nil, errors.New("paths is required")
	}
	args := []string{"add", "--"}
	for _, p := range a.Paths {
		rel, err := gitPath(inv, p)
		if err != nil {
			return nil, err
		}
		args = append(args, rel)
	}
	return gitResult(gitRun(ctx, inv, args...))
}

func gitCommit(ctx context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Message string `json:"message"`
		All     bool   `json:"all"`
		Author  string `json:"author"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Message) == "" {
		return nil, errors.New("message is required")
	}
	var args []string
	// Fall back to a platform identity when the repository has none: the
	// scrubbed environment has no global git config.
	if r, err := gitRun(ctx, inv, "config", "user.email"); err == nil && strings.TrimSpace(r.Stdout) == "" {
		args = append(args, "-c", "user.name=Weft Agent", "-c", "user.email=agent@weft.local")
	}
	args = append(args, "commit", "-m", a.Message)
	if a.All {
		args = append(args, "-a")
	}
	if a.Author != "" {
		if !strings.Contains(a.Author, "<") || strings.HasPrefix(a.Author, "-") {
			return nil, errors.New("author must look like 'Name <email>'")
		}
		args = append(args, "--author="+a.Author)
	}
	return gitResult(gitRun(ctx, inv, args...))
}

func gitBranch(ctx context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Name     string `json:"name"`
		From     string `json:"from"`
		Checkout bool   `json:"checkout"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if a.Name == "" {
		return nil, errors.New("name is required")
	}
	if err := safeRef("branch name", a.Name); err != nil {
		return nil, err
	}
	if err := safeRef("from", a.From); err != nil {
		return nil, err
	}
	args := []string{"branch", a.Name}
	if a.Checkout {
		args = []string{"checkout", "-b", a.Name}
	}
	if a.From != "" {
		args = append(args, a.From)
	}
	return gitResult(gitRun(ctx, inv, args...))
}

func gitCheckout(ctx context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Ref string `json:"ref"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if a.Ref == "" {
		return nil, errors.New("ref is required")
	}
	if err := safeRef("ref", a.Ref); err != nil {
		return nil, err
	}
	return gitResult(gitRun(ctx, inv, "checkout", a.Ref))
}

func gitMerge(ctx context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Branch  string `json:"branch"`
		NoFF    bool   `json:"noFF"`
		Message string `json:"message"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if a.Branch == "" {
		return nil, errors.New("branch is required")
	}
	if err := safeRef("branch", a.Branch); err != nil {
		return nil, err
	}
	var args []string
	if r, err := gitRun(ctx, inv, "config", "user.email"); err == nil && strings.TrimSpace(r.Stdout) == "" {
		args = append(args, "-c", "user.name=Weft Agent", "-c", "user.email=agent@weft.local")
	}
	args = append(args, "merge", "--no-edit")
	if a.NoFF {
		args = append(args, "--no-ff")
	}
	if a.Message != "" {
		args = append(args, "-m", a.Message)
	}
	args = append(args, a.Branch)
	return gitResult(gitRun(ctx, inv, args...))
}
