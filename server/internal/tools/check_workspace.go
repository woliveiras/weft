package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// CheckWorkspace ties a check run to the code it ran on.
type CheckWorkspace struct {
	// Head is the commit checked out, empty outside a Git repository.
	Head string `json:"head,omitempty"`
	// State fingerprints uncommitted changes: the status, the diff against
	// HEAD and the contents of untracked files. Same Head and State means
	// the same tree (ignored files excepted).
	State string `json:"state,omitempty"`
	// Partial is set when the fingerprint could not cover everything
	// (not a repository, oversized diff, too many untracked files).
	Partial bool `json:"partial,omitempty"`
	// Base is the commit check-defining changes are compared against: the
	// task's base commit, or HEAD when unknown.
	Base string `json:"base,omitempty"`
	// CheckFilesChanged lists changed test and check-definition files
	// (Makefile, package.json, pyproject.toml, conftest.py, *_test.go, …),
	// so a pass is never read without seeing that its tests changed.
	CheckFilesChanged []string `json:"checkFilesChanged,omitempty"`
}

const (
	maxFingerprintDiff      = 8 << 20
	maxFingerprintUntracked = 500
	maxCheckFilesListed     = 20
)

// checkDefining matches files that define what a check verifies.
var checkDefining = regexp.MustCompile(`(?i)(^|/)(` +
	`makefile|gnumakefile|[^/]*\.mk|justfile|taskfile\.ya?ml|` +
	`package\.json|(vitest|jest|vite|playwright|karma)\.config\.[cm]?[jt]s|\.eslintrc[^/]*|eslint\.config\.[cm]?[jt]s|tsconfig[^/]*\.json|` +
	`pyproject\.toml|setup\.cfg|tox\.ini|pytest\.ini|noxfile\.py|conftest\.py|test_[^/]*\.py|[^/]*_test\.py|` +
	`go\.mod|[^/]*_test\.go|\.golangci\.ya?ml|` +
	`[^/]*\.(test|spec)\.[cm]?[jt]sx?|__tests__/[^/]+|` +
	`\.weft/[^/]+|\.github/workflows/[^/]+` +
	`)$`)

// IsCheckDefiningFile reports whether a workspace-relative path is a test
// or check-definition file.
func IsCheckDefiningFile(p string) bool {
	return checkDefining.MatchString(path.Clean(strings.ReplaceAll(p, "\\", "/")))
}

// gitProbe runs a read-only git command in the sandbox with repository
// hooks, fsmonitor, external diff and textconv drivers disabled, so the
// fingerprint never executes repository-controlled code.
func gitProbe(ctx context.Context, inv *Invocation, stdin string, args ...string) (*ExecResult, error) {
	argv := append([]string{"git", "-c", "core.pager=cat", "-c", "color.ui=false", "-c", "safe.directory=*",
		"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false"}, args...)
	return sandboxOf(inv).Exec(ctx, ExecRequest{Argv: argv, Stdin: stdin, Timeout: time.Minute, MaxOutput: maxFingerprintDiff,
		Env: map[string]string{"GIT_PAGER": "cat", "GIT_EDITOR": "true", "GIT_OPTIONAL_LOCKS": "0"}})
}

// fingerprintWorkspace describes the workspace (the repository at the
// workspace root) a check is about to run on.
// It never fails the check: what it cannot establish is marked Partial.
func fingerprintWorkspace(ctx context.Context, inv *Invocation) CheckWorkspace {
	var w CheckWorkspace
	head, err := gitProbe(ctx, inv, "", "rev-parse", "--verify", "-q", "HEAD")
	if err != nil || head.ExitCode != 0 {
		w.Partial = true
		return w
	}
	w.Head = strings.TrimSpace(head.Stdout)
	h := sha256.New()
	status, err := gitProbe(ctx, inv, "", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || status.ExitCode != 0 || status.Truncated {
		w.Partial = true
	} else {
		h.Write([]byte(status.Stdout))
	}
	diff, err := gitProbe(ctx, inv, "", "diff", "HEAD", "--binary", "--no-ext-diff", "--no-textconv", "--no-color")
	if err != nil || diff.ExitCode != 0 || diff.Truncated {
		w.Partial = true
	} else {
		h.Write([]byte{0})
		h.Write([]byte(diff.Stdout))
	}
	untracked := untrackedFromStatus(status)
	if len(untracked) > maxFingerprintUntracked {
		w.Partial = true
	} else if len(untracked) > 0 {
		obj, err := gitProbe(ctx, inv, strings.Join(untracked, "\n")+"\n", "hash-object", "--no-filters", "--stdin-paths")
		if err != nil || obj.ExitCode != 0 {
			w.Partial = true
		} else {
			h.Write([]byte{0})
			h.Write([]byte(obj.Stdout))
		}
	}
	w.State = hex.EncodeToString(h.Sum(nil))[:16]

	w.Base = w.Head
	if base := principalBase(inv); base != "" && safeRef("base", base) == nil {
		w.Base = base
	}
	changed := map[string]bool{}
	for _, p := range untracked {
		changed[p] = true
	}
	names, err := gitProbe(ctx, inv, "", "diff", "--name-only", "-z", "--no-renames", w.Base)
	if err != nil || names.ExitCode != 0 {
		w.Partial = true
	} else {
		for _, p := range strings.Split(names.Stdout, "\x00") {
			if p != "" {
				changed[p] = true
			}
		}
	}
	for p := range changed {
		if IsCheckDefiningFile(p) {
			w.CheckFilesChanged = append(w.CheckFilesChanged, p)
		}
	}
	sort.Strings(w.CheckFilesChanged)
	if len(w.CheckFilesChanged) > maxCheckFilesListed {
		w.CheckFilesChanged = append(w.CheckFilesChanged[:maxCheckFilesListed], "…")
	}
	return w
}

// untrackedFromStatus reads "?? path" entries of `git status -z`.
func untrackedFromStatus(status *ExecResult) []string {
	if status == nil || status.ExitCode != 0 {
		return nil
	}
	var out []string
	for _, e := range strings.Split(status.Stdout, "\x00") {
		if strings.HasPrefix(e, "?? ") && !strings.ContainsAny(e, "\n\r") {
			out = append(out, e[3:])
		}
	}
	return out
}

func principalBase(inv *Invocation) string {
	if inv.Principal == nil {
		return ""
	}
	return inv.Principal.BaseCommit
}
