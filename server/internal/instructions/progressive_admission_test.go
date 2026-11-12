package instructions

import (
	"strings"
	"testing"
)

func TestAdmitRepositorySkillWhenSelectedConsoleDoesNotFit(t *testing.T) {
	console := progressiveEntry{entry: Entry{Scope: "graph", Source: "sample", Selection: "console:sample", Stage: "catalogue"}, content: strings.Repeat("c", 300)}
	repository := progressiveEntry{
		entry:   Entry{Scope: "repository-skill", Source: ".agents/skills/sample/SKILL.md", Selection: "repository:sample", Stage: "catalogue"},
		content: "Sample",
		repo:    &repoCandidate{name: "sample"},
	}
	for _, tc := range []struct {
		name, reason string
		budget       int
	}{
		{"console admitted", "shadowed_by_console", 1024},
		{"console omitted", "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{
				snapshot: &Snapshot{Budget: tc.budget},
				items:    []progressiveEntry{repository, console},
				selected: map[string]progressiveEntry{},
			}
			s.admit(nil, []string{"sample"})
			if e := s.snapshot.Entries[0]; e.Reason != tc.reason || e.Included != (tc.reason == "") {
				t.Fatalf("unexpected repository admission: %+v", e)
			}
		})
	}
}
