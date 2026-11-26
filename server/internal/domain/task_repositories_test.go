package domain

import (
	"strings"
	"testing"
)

func TestReviewedRepositoryBindings(t *testing.T) {
	a := TaskRepository{Alias: "infra", ProjectID: "prj_one", BaseCommit: strings.Repeat("a", 40), Checks: []Check{{Name: "test", Command: "sh check.sh"}}, RequiredChecks: []string{"test"}}
	b := a
	b.Alias = "backend"
	b.ProjectID = "prj_two"
	if err := ValidateTaskRepositories([]TaskRepository{a, b}); err != nil {
		t.Fatal(err)
	}
	checks := RepositoryChecks([]TaskRepository{b, a})
	if checks[0].Name != "backend.test" || checks[1].Cwd != "infra" || a.Checks[0].Name != "test" {
		t.Fatal(checks)
	}
	for name, change := range map[string]func(*TaskRepository){
		"repeated project":    func(r *TaskRepository) { r.ProjectID = a.ProjectID },
		"repeated alias":      func(r *TaskRepository) { r.Alias = a.Alias },
		"reserved path":       func(r *TaskRepository) { r.Alias = ".git" },
		"relative escape":     func(r *TaskRepository) { r.Alias = "../api" },
		"abbreviated head":    func(r *TaskRepository) { r.BaseCommit = "abcd1234" },
		"unbound check":       func(r *TaskRepository) { r.RequiredChecks = []string{"frontend.test"} },
		"incomplete delivery": func(r *TaskRepository) { r.Repository = "acme/backend" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := b
			change(&bad)
			if err := ValidateTaskRepositories([]TaskRepository{a, bad}); err == nil {
				t.Fatal("accepted invalid scope")
			}
		})
	}
}
