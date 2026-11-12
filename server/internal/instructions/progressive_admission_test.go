package instructions

import (
	"fmt"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
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

func TestConsoleBodyBudgetOffersRepositoryFallback(t *testing.T) {
	for _, remaining := range []int{1024, 1} {
		t.Run(fmt.Sprintf("remaining-%d", remaining), func(t *testing.T) {
			root := t.TempDir()
			writeGuidance(t, root, ".agents/skills/sample/SKILL.md", "---\nname: sample\ndescription: Fallback catalogue\n---\nREPO BODY")
			m := manager(t)
			ctx := t.Context()
			if _, err := m.Save(ctx, Input{ID: "sample", Title: "Sample", Body: strings.Repeat("c", MaxTokens), Enabled: true}, 0, "test", false); err != nil {
				t.Fatal(err)
			}
			g := &domain.Graph{Spec: domain.GraphSpec{Instructions: domain.InstructionRefs{
				RepositoryGuidance: &domain.RepositoryGuidancePolicy{}, SkillIDs: []string{"sample"},
			}}}
			snap, session, err := m.Prepare(ctx, g, &domain.Node{ID: "worker"}, root, root, "task", nil)
			if err != nil {
				t.Fatal(err)
			}
			snap.ExecutionID = "body-budget"
			snap.Budget = remaining
			if err := m.SaveSnapshot(ctx, snap); err != nil {
				t.Fatal(err)
			}
			status, err := session.Load(ctx, "console:sample", "")
			if err != nil || !strings.Contains(status, "token_budget") || strings.Contains(session.Prompt(), "console:sample") {
				t.Fatalf("oversized console skill remained active: %q, %v", status, err)
			}
			_, selectable := session.selected["repository:sample"]
			history, err := m.Snapshot(ctx, snap.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			if remaining == 1 {
				if selectable || snap.Budget != remaining {
					t.Fatal("unaffordable fallback was admitted")
				}
				findOmission(t, history, ".agents/skills/sample/SKILL.md", "token_budget")
				return
			}
			if !selectable || !strings.Contains(session.Prompt(), "Fallback catalogue") || strings.Contains(session.Prompt(), "REPO BODY") {
				t.Fatal("expected an available fallback catalogue without its body")
			}
			fallback := session.selected["repository:sample"]
			if cost := len(renderEntry(fallback.entry, fallback.content)); snap.Budget != remaining-cost || history.Budget != snap.Budget {
				t.Fatal("fallback framing was not charged exactly once")
			}
			budget := snap.Budget
			if _, err := session.Load(ctx, "console:sample", ""); err != nil || snap.Budget != budget ||
				strings.Count(session.Prompt(), "Fallback catalogue") != 1 {
				t.Fatal("repeating the rejected selection duplicated the fallback", err)
			}
		})
	}
}
