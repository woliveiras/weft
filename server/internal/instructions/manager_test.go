package instructions

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestValidation(t *testing.T) {
	good := Input{ID: "review", Title: "Review", Body: "Check code"}
	for _, modify := range []func(*Input){func(i *Input) { i.ID = "../escape" }, func(i *Input) { i.Title = "" }, func(i *Input) { i.Body = "\x00" }, func(i *Input) { i.Body = string([]byte{255}) }, func(i *Input) { i.Body = strings.Repeat("a", MaxBytes+1) }} {
		in := good
		modify(&in)
		if Validate(in) == nil {
			t.Fatalf("accepted invalid input: %q", in.ID)
		}
	}
	if err := Validate(good); err != nil {
		t.Fatal(err)
	}
}
func TestBudgetAndRendering(t *testing.T) {
	s := Snapshot{Budget: 4 + len("graph") + len("a") + 6}
	add(&s, Entry{Scope: "graph", Source: "a", Body: "four"})
	add(&s, Entry{Scope: "project", Source: "b", Body: "five"})
	if !s.Entries[0].Included || s.Entries[1].Reason != "token_budget" || s.Entries[1].Body != "" || s.Budget != 0 {
		t.Fatal(s)
	}
	if !strings.Contains(Context(s), "four") || strings.Contains(Context(s), "five") {
		t.Fatal(Context(s))
	}
}

type brokenStore struct {
	storage.Store
	write bool
}

func (b brokenStore) GetSetting(context.Context, string) (*storage.Setting, error) {
	if b.write {
		return nil, storage.ErrNotFound
	}
	return nil, errors.New("dependency unavailable")
}
func (b brokenStore) PutSetting(context.Context, string, []byte, int64, string) (int64, error) {
	return 0, errors.New("write unavailable")
}
func TestDependencyFailure(t *testing.T) {
	for _, write := range []bool{false, true} {
		m := Manager{Store: brokenStore{write: write}}
		if _, err := m.Save(t.Context(), Input{ID: "a", Title: "A", Body: "a"}, 0, "actor", false); err == nil {
			t.Fatal("failure ignored")
		}
	}
}

func TestRequiredContentReservedAndLegacyUntouched(t *testing.T) {
	m := manager(t)
	_, err := m.Save(t.Context(), Input{ID: "large", Title: "Large", Body: strings.Repeat("x", MaxTokens), Enabled: true}, 0, "a", false)
	if err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Spec: domain.GraphSpec{Instructions: domain.InstructionRefs{SkillIDs: []string{"large"}}}}
	n := &domain.Node{ID: "n"}
	s, err := m.Resolve(t.Context(), g, n, "", "task", []string{"operator request"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Entries[0].Reason != "token_budget" || !s.Entries[1].Included || !s.Entries[2].Included {
		t.Fatal(s)
	}
	s, err = m.Resolve(t.Context(), g, n, "", strings.Repeat("a", MaxTokens+1), nil)
	if err == nil || s.Entries[0].Reason != "required_content_exceeds_budget" {
		t.Fatal(s, err)
	}
	g.Spec.Instructions = domain.InstructionRefs{}
	prompt := strings.Repeat("a", MaxTokens+1)
	s, err = m.Resolve(t.Context(), g, n, "", prompt, []string{"operator"})
	if err != nil || s.Entries[0].Body != prompt || s.Entries[1].Body != "operator" {
		t.Fatal("legacy changed", err)
	}
}
func TestRenderedBudgetIncludesFraming(t *testing.T) {
	m := manager(t)
	body := strings.Repeat("x", 2000)
	for _, id := range []string{"a", "b", "c", "d"} {
		if _, err := m.Save(t.Context(), Input{ID: id, Title: id, Body: body, Enabled: true}, 0, "a", false); err != nil {
			t.Fatal(err)
		}
	}
	g := &domain.Graph{Spec: domain.GraphSpec{Instructions: domain.InstructionRefs{SkillIDs: []string{"a", "b", "c", "d"}}}}
	s, err := m.Resolve(t.Context(), g, &domain.Node{ID: "n"}, "", "agent", []string{"operator"})
	if err != nil {
		t.Fatal(err)
	}
	if len(Context(*s))+1+len("agent")+len("operator") > MaxTokens {
		t.Fatal("framing exceeded budget")
	}
}
