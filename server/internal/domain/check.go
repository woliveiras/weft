package domain

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Check is a declared verification command (test, lint, build or a named
// custom one). Projects and workflows declare checks; the check.run tool
// runs one by name in the agent's sandbox and worktree under the agent's
// shell.exec grant, so a check is a named shell call, not a privilege.
type Check struct {
	// Name is how agents and people refer to the check: "test", "lint",
	// "build" or a custom lowercase name such as "web-test".
	Name string `json:"name" yaml:"name"`
	// Command is the shell command line, e.g. "go test ./...".
	Command string `json:"command" yaml:"command"`
	// Cwd is an optional directory relative to the workspace.
	Cwd string `json:"cwd,omitempty" yaml:"cwd,omitempty"`
	// TimeoutSec bounds one run (default 600, at most 1800).
	TimeoutSec int `json:"timeoutSec,omitempty" yaml:"timeoutSec,omitempty"`
}

const (
	// MaxChecks bounds how many checks one project or workflow declares.
	MaxChecks       = 20
	MaxCheckTimeout = 1800
	maxCheckCommand = 1024
)

var checkName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)

// ValidateChecks reports the first problem in a list of declared checks.
// The error names the offending entry by index.
func ValidateChecks(checks []Check) error {
	if len(checks) > MaxChecks {
		return fmt.Errorf("at most %d checks may be declared", MaxChecks)
	}
	seen := map[string]bool{}
	for i, c := range checks {
		switch {
		case !checkName.MatchString(c.Name):
			return fmt.Errorf("checks[%d].name: %q must be lowercase letters, digits, '-' or '_' (at most 40)", i, c.Name)
		case seen[c.Name]:
			return fmt.Errorf("checks[%d].name: duplicate check %q", i, c.Name)
		case strings.TrimSpace(c.Command) == "" || len(c.Command) > maxCheckCommand || !utf8.ValidString(c.Command) || strings.ContainsAny(c.Command, "\x00\n\r"):
			return fmt.Errorf("checks[%d].command: a single-line command of at most %d bytes is required", i, maxCheckCommand)
		case len(c.Cwd) > 512 || strings.ContainsAny(c.Cwd, "\x00\n\r"):
			return fmt.Errorf("checks[%d].cwd: invalid directory", i)
		case c.TimeoutSec < 0 || c.TimeoutSec > MaxCheckTimeout:
			return fmt.Errorf("checks[%d].timeoutSec: must be between 0 and %d", i, MaxCheckTimeout)
		}
		seen[c.Name] = true
	}
	return nil
}

// MergeChecks combines project checks with workflow checks. A workflow
// check replaces the project check of the same name; order is project
// checks first, then workflow-only checks.
func MergeChecks(project, workflow []Check) []Check {
	if len(project) == 0 {
		return workflow
	}
	byName := map[string]Check{}
	for _, c := range workflow {
		byName[c.Name] = c
	}
	out := make([]Check, 0, len(project)+len(workflow))
	used := map[string]bool{}
	for _, c := range project {
		if w, ok := byName[c.Name]; ok {
			c = w
		}
		out = append(out, c)
		used[c.Name] = true
	}
	for _, c := range workflow {
		if !used[c.Name] {
			out = append(out, c)
		}
	}
	return out
}
