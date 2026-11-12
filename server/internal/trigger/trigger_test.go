package trigger_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func triggerStore(t *testing.T) storage.Store {
	t.Helper()
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "factory", Name: "Factory"}, Spec: domain.GraphSpec{Nodes: []domain.Node{
		{ID: "planner", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Loop: domain.LoopSpec{Mode: domain.LoopOnce}},
		{ID: "integrator", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Loop: domain.LoopSpec{Mode: domain.LoopOnce}},
	}}}
	if _, err := st.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	return st
}

func input() trigger.Input {
	return trigger.Input{Name: "Ready issues", Workflow: "factory", NodeID: "planner", ResultNodeID: "integrator",
		Source: trigger.SourceConfig{Kind: "github", ConnectionID: "gh", Repository: "acme/app"}, Label: "weft", Owner: "weft-bot",
		Authorization: taskauth.Workspace}
}

func TestTriggerValidationAndConsent(t *testing.T) {
	st := triggerStore(t)
	m := trigger.Manager{Store: st}
	bad := input()
	bad.Name, bad.Workflow, bad.Label, bad.IntervalMinutes, bad.Authorization, bad.ExecutionMode = "", "task-run-x", "weft:review", 2, "root", "cloud"
	bad.Source.Repository = ""
	var fe *trigger.FieldError
	if _, err := m.Create(ctx, bad, "admin"); !errors.As(err, &fe) || len(fe.Errors) != 7 {
		t.Fatalf("validation: %v", err)
	}
	missing := input()
	missing.NodeID = "nobody"
	if _, err := m.Create(ctx, missing, "admin"); !errors.As(err, &fe) {
		t.Fatalf("unknown entry agent: %v", err)
	}
	tr, err := m.Create(ctx, input(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if tr.State != trigger.Active || tr.IntervalMinutes != 15 || tr.Consent.Actor != "alice" || tr.Consent.WorkflowVersion != 1 ||
		tr.Consent.Authorization != taskauth.Workspace || tr.Consent.PolicyVersion != taskauth.PolicyVersion {
		t.Fatalf("created: %+v", tr)
	}
	if p, err := m.ConsentProblem(ctx, tr); err != nil || p != "" {
		t.Fatalf("fresh consent: %q %v", p, err)
	}
	// Changing the workflow invalidates the consent.
	g, _ := st.GetGraph(ctx, "factory", 0)
	if _, err := st.SaveGraph(ctx, g, 1); err != nil {
		t.Fatal(err)
	}
	if p, _ := m.ConsentProblem(ctx, tr); p == "" {
		t.Fatal("a new workflow version must invalidate consent")
	}
	// Resume records new consent for the current workflow.
	tr, err = m.Resume(ctx, tr.ID, "bob")
	if err != nil || tr.Consent.Actor != "bob" || tr.Consent.WorkflowVersion != 2 {
		t.Fatalf("resume: %+v %v", tr, err)
	}
	// Editing what the consent covers pauses the trigger; cosmetic edits do not.
	cosmetic := input()
	cosmetic.Name, cosmetic.IntervalMinutes = "Renamed", 30
	if tr, err = m.Edit(ctx, tr.ID, cosmetic, "bob"); err != nil || tr.State != trigger.Active || tr.Name != "Renamed" {
		t.Fatalf("cosmetic edit: %+v %v", tr, err)
	}
	covered := cosmetic
	covered.Authorization = taskauth.Full
	if tr, err = m.Edit(ctx, tr.ID, covered, "bob"); err != nil || tr.State != trigger.Paused || tr.PausedReason == "" {
		t.Fatalf("covered edit: %+v %v", tr, err)
	}
	if p, _ := m.ConsentProblem(ctx, tr); p == "" {
		t.Fatal("authorization change must invalidate consent")
	}
	if tr, err = m.Resume(ctx, tr.ID, "carol"); err != nil || tr.State != trigger.Active || tr.Consent.Authorization != taskauth.Full {
		t.Fatalf("resume after edit: %+v %v", tr, err)
	}
	if tr, err = m.Pause(ctx, tr.ID, "manual", "carol"); err != nil || tr.State != trigger.Paused {
		t.Fatalf("pause: %+v %v", tr, err)
	}
	list, _ := m.List(ctx, "factory")
	other, _ := m.List(ctx, "other")
	if len(list) != 1 || len(other) != 0 {
		t.Fatalf("list: %d %d", len(list), len(other))
	}
	if err := m.Delete(ctx, tr.ID, "carol"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(ctx, tr.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
	// A deleted workflow is reported.
	tr2, _ := m.Create(ctx, input(), "alice")
	if err := st.DeleteGraph(ctx, "factory"); err != nil {
		t.Fatal(err)
	}
	if p, _ := m.ConsentProblem(ctx, tr2); p != "The workflow was deleted." {
		t.Fatalf("deleted workflow: %q", p)
	}
}

func TestEditRejectsMissingWorkflowOrAgent(t *testing.T) {
	for _, field := range []string{"workflow", "nodeId", "resultNodeId"} {
		t.Run(field, func(t *testing.T) {
			st := triggerStore(t)
			m := trigger.Manager{Store: st}
			original, err := m.Create(ctx, input(), "alice")
			if err != nil {
				t.Fatal(err)
			}
			if field == "workflow" {
				if err := st.DeleteGraph(ctx, "factory"); err != nil {
					t.Fatal(err)
				}
			} else {
				g, err := st.GetGraph(ctx, "factory", 0)
				if err != nil {
					t.Fatal(err)
				}
				if field == "nodeId" {
					g.Spec.Nodes = g.Spec.Nodes[1:]
				} else {
					g.Spec.Nodes = g.Spec.Nodes[:1]
				}
				if _, err := st.SaveGraph(ctx, g, g.Metadata.Version); err != nil {
					t.Fatal(err)
				}
			}
			edit := input()
			edit.Name = "Unsaved edit"
			var validation *trigger.FieldError
			if _, err := m.Edit(ctx, original.ID, edit, "bob"); !errors.As(err, &validation) ||
				len(validation.Errors) != 1 || validation.Errors[0].Path != field {
				t.Fatalf("expected field error for %s, got %v", field, err)
			}
			saved, err := m.Get(ctx, original.ID)
			if err != nil || saved.Name != original.Name || !reflect.DeepEqual(saved.Consent, original.Consent) ||
				saved.State != original.State || !saved.UpdatedAt.Equal(original.UpdatedAt) {
				t.Fatalf("failed edit changed persisted trigger: %+v, %v", saved, err)
			}
		})
	}
}
