package taskconfig

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

func TestPromptProposalValidation(t *testing.T) {
	for _, raw := range []string{`{}`, `{"outcome":"admin"}`, `{"outcome":"review","tools":["shell.exec"]}`, `{"outcome":"review"} {}`, `{"outcome":"review","repository":"https://invented.example/repo"}`, `{"outcome":"review","repository":"file:///tmp/repo"}`, `{"outcome":"review","repository":"https://user:secret@example.com/repo"}`, strings.Repeat("x", 4097)} {
		if _, err := parseProposal(raw, "Review https://example.com/repo"); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	if _, err := parseProposal(`{"outcome":"review","workflowId":"factory","repository":"https://example.com/repo","clarification":false}`, "Review https://example.com/repo"); err != nil {
		t.Fatal(err)
	}
}
func TestPromptBindingDoesNotGuessOrMutate(t *testing.T) {
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "factory", Version: 4}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "entry"}, {ID: "result"}}, Channels: []domain.Channel{{ID: "in", To: []string{"entry"}, Types: []string{"request"}}, {ID: "out", From: "entry", To: []string{"result"}}}}}
	d := &Draft{Input: Input{Title: "Task", Request: "Original request"}}
	bindPrompt(d, g)
	if d.NodeID != "entry" || d.ResultNodeID != "result" || d.MessageType != "request" || !strings.Contains(d.PayloadJSON, d.Request) || g.Spec.Nodes[0].Prompt != "" {
		t.Fatal(d)
	}
	g.Spec.Channels = append(g.Spec.Channels, domain.Channel{ID: "second", To: []string{"entry"}})
	d = &Draft{}
	bindPrompt(d, g)
	if d.NodeID != "" {
		t.Fatal("guessed entry")
	}
	g.Spec.Channels = nil
	d = &Draft{}
	bindPrompt(d, g)
	if d.NodeID != "" || d.ResultNodeID != "" {
		t.Fatal("guessed among nodes")
	}
}

type promptModel struct {
	raw    string
	err    error
	t      *testing.T
	before func()
}

func (p promptModel) Name() string                                    { return "fixture" }
func (p promptModel) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (p promptModel) Chat(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	if len(r.Tools) != 0 || r.MaxTokens != 512 || !r.JSON {
		p.t.Fatal("unbounded or tool-enabled interpreter")
	}
	if p.before != nil {
		p.before()
	}
	if p.err != nil {
		return nil, p.err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Content: p.raw}}, nil
}
func TestPromptPersistenceResolutionAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.db")
	s, err := sqlstore.OpenSQLite(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	m := Manager{Store: s}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "software-factory"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reader"}}}}
	if _, err = s.SaveGraph(t.Context(), g, 0); err != nil {
		t.Fatal(err)
	}
	d, err := m.SavePrompt(t.Context(), PromptInput{Request: "Explain this project", Settings: &PromptSettings{Title: "Custom title", ExecutionMode: "sandbox"}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Custom title" || d.ExecutionMode != "sandbox" {
		t.Fatal("explicit settings lost", d)
	}
	id := d.ID
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlstore.OpenSQLite(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m.Store = s
	d, err = m.Get(t.Context(), id)
	if err != nil || d.Resolution.State != "pending" || d.Request != "Explain this project" {
		t.Fatal(d, err)
	}
	d, err = m.ResolvePrompt(t.Context(), d, nil, "operator")
	if err != nil || d.Resolution.State != "needs_model" {
		t.Fatal(d, err)
	}
	r, err := llm.NewRegistry(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	r.Add(llm.ProviderConfig{Name: "fixture", DefaultModel: "fixture"}, promptModel{t: t, raw: `{"outcome":"understand","workflowId":"","repository":"","clarification":false}`})
	d, err = m.ResolvePrompt(t.Context(), d, r, "operator")
	if err != nil || d.Resolution.State != "resolved" || d.GraphID != "software-factory" || d.NodeID != "reader" || d.ResultNodeID != "reader" {
		t.Fatal(d, err)
	}
	oldRevision := d.Revision
	for _, tc := range []struct {
		raw   string
		err   error
		state string
	}{
		{err: errors.New("sensitive provider payload"), state: "model_error"},
		{raw: `{"outcome":"review","tools":[]}`, state: "invalid_proposal"},
		{raw: `{"outcome":"review","clarification":true}`, state: "needs_input"},
		{raw: `{"outcome":"review","workflowId":"unknown"}`, state: "needs_input"},
	} {
		d, err = m.SavePrompt(t.Context(), PromptInput{ID: d.ID, Revision: d.Revision, Request: d.Request}, "operator")
		if err != nil {
			t.Fatal(err)
		}
		r.Add(llm.ProviderConfig{Name: "fixture", DefaultModel: "fixture"}, promptModel{t: t, raw: tc.raw, err: tc.err})
		d, err = m.ResolvePrompt(t.Context(), d, r, "operator")
		if err != nil || d.Resolution.State != tc.state || strings.Contains(d.Resolution.Message, "sensitive") {
			t.Fatal(d, err)
		}
	}
	if _, err = m.SavePrompt(t.Context(), PromptInput{ID: id, Revision: oldRevision, Request: "stale"}, "operator"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal(err)
	}
	current, _ := m.Get(t.Context(), id)
	r.Add(llm.ProviderConfig{Name: "fixture", DefaultModel: "fixture"}, promptModel{t: t, raw: `{"outcome":"review"}`, before: func() {
		_, err = m.SavePrompt(t.Context(), PromptInput{ID: id, Revision: current.Revision, Request: "Newer request"}, "operator")
		if err != nil {
			t.Fatal(err)
		}
	}})
	if _, err = m.ResolvePrompt(t.Context(), current, r, "operator"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("concurrent change overwritten", err)
	}
	gs, _ := s.ListGraphs(t.Context())
	if len(gs) != 1 || gs[0].Metadata.Version != 1 {
		t.Fatal("workflow mutated", gs)
	}
}

func TestPromptSelectionRepositoryAndManualRepair(t *testing.T) {
	s, err := sqlstore.OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "selection.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"software-factory", "chosen"} {
		_, err = s.SaveGraph(t.Context(), &domain.Graph{Metadata: domain.GraphMetadata{ID: id, Name: id}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reader"}}}}, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	m := Manager{Store: s}
	r, err := llm.NewRegistry(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ request, explicit, raw, state, graph string }{
		{"Review with chosen", "", `{"outcome":"review","workflowId":"chosen"}`, "resolved", "chosen"},
		{"Review this", "chosen", `{"outcome":"review","workflowId":"software-factory"}`, "resolved", "chosen"},
		{"Review this", "", `{"outcome":"review","workflowId":"chosen"}`, "invalid_proposal", ""},
		{"Review https://example.com/repo", "", `{"outcome":"review","repository":"https://example.com/repo"}`, "needs_repository", "software-factory"},
	}
	for _, tc := range cases {
		d, err := m.SavePrompt(t.Context(), PromptInput{Request: tc.request, GraphID: tc.explicit}, "operator")
		if err != nil {
			t.Fatal(err)
		}
		r.Add(llm.ProviderConfig{Name: "fixture", DefaultModel: "fixture"}, promptModel{t: t, raw: tc.raw})
		d, err = m.ResolvePrompt(t.Context(), d, r, "operator")
		if err != nil || d.Resolution.State != tc.state || d.GraphID != tc.graph {
			t.Fatal(tc, d, err)
		}
		if tc.state == "needs_repository" {
			if _, err = m.Save(t.Context(), d.ID, d.Input, d.Revision, "operator"); !errors.Is(err, ErrInvalid) {
				t.Fatal("repository prerequisite bypassed", err)
			}
			in := d.Input
			in.Request = "Review supplied material instead"
			repaired, err := m.Save(t.Context(), d.ID, in, d.Revision, "operator")
			if err != nil || repaired.Resolution != nil {
				t.Fatal(repaired, err)
			}
		}
	}
}

func TestPromptSaveValidationAndDependencyFailures(t *testing.T) {
	for _, request := range []string{"", "\x00", strings.Repeat("a", 16385), string([]byte{255})} {
		if _, err := (Manager{}).SavePrompt(t.Context(), PromptInput{Request: request}, "operator"); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := (Manager{}).SavePrompt(t.Context(), PromptInput{Request: "Explain", Settings: &PromptSettings{ExecutionMode: "unrestricted"}}, "operator"); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid settings accepted", err)
	}
	for _, failure := range []string{"audit", "write"} {
		b := &broken{fail: failure}
		if _, err := (Manager{Store: b}).SavePrompt(t.Context(), PromptInput{Request: "Explain"}, "operator"); err == nil {
			t.Fatal("ignored failure")
		}
		if failure == "audit" && b.writes != 0 {
			t.Fatal("mutation after audit failure")
		}
	}
}
