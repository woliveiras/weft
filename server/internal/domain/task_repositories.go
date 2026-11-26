package domain

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// TaskRepository is a reviewed, task-local checkout. Alias is a single
// portable directory component shared by the console and runtime tools.
type TaskRepository struct {
	Alias              string   `json:"alias" yaml:"alias"`
	ProjectID          string   `json:"projectId" yaml:"projectId"`
	BaseCommit         string   `json:"baseCommit" yaml:"baseCommit"`
	SourceRef          string   `json:"sourceRef,omitempty" yaml:"sourceRef,omitempty"`
	ConnectionRevision string   `json:"connectionRevision,omitempty" yaml:"connectionRevision,omitempty"`
	SCMConnection      string   `json:"scmConnection,omitempty" yaml:"scmConnection,omitempty"`
	Repository         string   `json:"repository,omitempty" yaml:"repository,omitempty"`
	TargetBranch       string   `json:"targetBranch,omitempty" yaml:"targetBranch,omitempty"`
	Checks             []Check  `json:"checks,omitempty" yaml:"checks,omitempty"`
	RequiredChecks     []string `json:"requiredChecks,omitempty" yaml:"requiredChecks,omitempty"`
}

var aliasPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)
var projectPattern = regexp.MustCompile(`^prj_[a-z0-9]{1,80}$`)
var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func ValidRepositoryAlias(name string) bool {
	if !aliasPattern.MatchString(name) || !filepath.IsLocal(name) {
		return false
	}
	switch name {
	case "aux", "con", "prn", "nul":
		return false
	}
	return !(len(name) == 4 && (strings.HasPrefix(name, "com") || strings.HasPrefix(name, "lpt")) && name[3] >= '1' && name[3] <= '9')
}

func ValidateTaskRepositories(repos []TaskRepository) error {
	aliases, projects, destinations := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i, r := range repos {
		field := fmt.Sprintf("repositories[%d] (%s)", i, r.Alias)
		if !ValidRepositoryAlias(r.Alias) || aliases[r.Alias] {
			return fmt.Errorf("%s: choose a unique lowercase directory name", field)
		}
		if !projectPattern.MatchString(r.ProjectID) || projects[r.ProjectID] {
			return fmt.Errorf("%s: choose a distinct registered project", field)
		}
		if !fullCommit.MatchString(r.BaseCommit) {
			return fmt.Errorf("%s: select a full base commit", field)
		}
		if len(r.SourceRef) > 256 || strings.ContainsAny(r.SourceRef, "\r\n\x00") {
			return fmt.Errorf("%s: invalid source ref", field)
		}
		if r.SCMConnection != "" || r.Repository != "" || r.TargetBranch != "" {
			if r.SCMConnection == "" || len(r.SCMConnection) > 64 || r.Repository == "" || len(r.Repository) > 256 || r.TargetBranch == "" || len(r.TargetBranch) > 256 || strings.ContainsAny(r.SCMConnection+r.Repository+r.TargetBranch, " \t\r\n\x00") || destinations[r.SCMConnection] {
				return fmt.Errorf("%s: select a distinct delivery connection, repository and base branch", field)
			}
			destinations[r.SCMConnection] = true
		}
		if err := ValidateChecks(r.Checks); err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
		checks := map[string]bool{}
		for _, c := range r.Checks {
			if c.Cwd != "" && !filepath.IsLocal(c.Cwd) {
				return fmt.Errorf("%s: check %s must run inside its project", field, c.Name)
			}
			checks[c.Name] = true
		}
		for _, name := range r.RequiredChecks {
			if !checks[name] {
				return fmt.Errorf("%s: unknown or duplicate required check %q", field, name)
			}
			delete(checks, name)
		}
		aliases[r.Alias], projects[r.ProjectID] = true, true
	}
	if len(destinations) > 0 && len(destinations) != len(repos) {
		return fmt.Errorf("configure a PR destination for every selected repository or use local delivery")
	}
	return nil
}

func RepositoryProjectIDs(repos []TaskRepository) []string {
	var ids []string
	for _, r := range repos {
		ids = append(ids, r.ProjectID)
	}
	return ids
}

// RepositoryChecks gives each declared check an unambiguous name while
// leaving the project and workflow check formats unchanged.
func RepositoryChecks(repos []TaskRepository) []Check {
	var checks []Check
	for _, r := range repos {
		for _, c := range r.Checks {
			c.Name = r.Alias + "." + c.Name
			c.Cwd = filepath.ToSlash(filepath.Join(r.Alias, c.Cwd))
			checks = append(checks, c)
		}
	}
	return checks
}
