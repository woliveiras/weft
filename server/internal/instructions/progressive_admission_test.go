package instructions

import (
	"fmt"
	"slices"
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

func TestOversizedConsoleBodyWithoutFallbackKeepsCatalogueSlot(t *testing.T) {
	ctx := t.Context()
	m := manager(t)
	if _, err := m.Save(ctx, Input{ID: "standalone", Title: "Standalone", Body: strings.Repeat("x", MaxTokens), Enabled: true}, 0, "test", false); err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Spec: domain.GraphSpec{Instructions: domain.InstructionRefs{
		RepositoryGuidance: &domain.RepositoryGuidancePolicy{}, SkillIDs: []string{"standalone"},
	}}}
	root := t.TempDir()
	snap, session, err := m.Prepare(ctx, g, &domain.Node{ID: "worker"}, root, root, "task", nil)
	if err != nil {
		t.Fatal(err)
	}
	snap.ExecutionID = "standalone-budget"
	before, budget := session.Prompt(), snap.Budget
	for range 2 {
		status, err := session.Load(ctx, "console:standalone", "")
		if err != nil || status != "omitted: token_budget" || snap.Budget != budget || session.Prompt() != before {
			t.Fatalf("oversized body displaced its catalogue: %q, %v", status, err)
		}
	}
}

func TestSelectedGlobalSkillUsesGraphBudgetPriority(t *testing.T) {
	m := manager(t)
	for _, id := range []string{"a-default", "z-selected"} {
		if _, err := m.Save(t.Context(), Input{ID: id, Title: id, Description: strings.Repeat("d", 120), Body: id, Enabled: true, Global: true}, 0, "test", false); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	g := &domain.Graph{Spec: domain.GraphSpec{Instructions: domain.InstructionRefs{
		RepositoryGuidance: &domain.RepositoryGuidancePolicy{}, InstallationDefaults: true, SkillIDs: []string{"z-selected"},
	}}}
	// Either catalogue entry fits on its own. The selected one gets priority.
	prompt := strings.Repeat("p", MaxTokens-len(progressivePreamble)-1-250)
	snap, session, err := m.Prepare(t.Context(), g, &domain.Node{ID: "worker"}, root, root, prompt, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := session.selected["console:z-selected"]
	if !ok || entry.entry.Scope != "graph" {
		t.Fatalf("selected skill did not retain its graph rank: %+v", snap.Entries)
	}
	findOmission(t, snap, "a-default", "token_budget")
	if !slices.ContainsFunc(snap.Entries, func(e Entry) bool {
		return e.Source == "z-selected" && e.Scope == "installation" && e.Reason == "duplicate" && !e.Included
	}) {
		t.Fatal("duplicate installation entry was not recorded")
	}
}
