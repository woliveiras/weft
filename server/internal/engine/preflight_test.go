package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/queue/memory"
)

func preflightGraph() *domain.Graph {
	return &domain.Graph{
		Metadata: domain.GraphMetadata{ID: "pre"},
		Spec: domain.GraphSpec{
			Nodes: []domain.Node{
				{ID: "ok", Prompt: "p"}, // default provider
				{ID: "ghost", Prompt: "p", Model: domain.ModelRef{Provider: "nope"}}, // unknown
				{ID: "off", Prompt: "p", Model: domain.ModelRef{Provider: "nope"}, Disabled: true},
				{ID: "echo", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Model: domain.ModelRef{Provider: "nope"}},
			},
			Channels: []domain.Channel{{ID: "in", To: []string{"ok"}}},
		},
	}
}

func TestPreflightWarnsOnSaveAndBlocksStart(t *testing.T) {
	dir := t.TempDir()
	r := newRig(t, openStore(t, dir), memory.New(), dir, "i1")

	rep := r.eng.Validate(preflightGraph())
	if !rep.Valid || len(rep.Warnings) != 1 || rep.Warnings[0].Path != "spec.nodes[1].model.provider" || !strings.Contains(rep.Warnings[0].Message, `"nope"`) {
		t.Fatalf("validate must warn only about the enabled native node: %+v", rep)
	}
	if _, _, err := r.eng.SaveGraph(r.ctx, preflightGraph(), 0, "t"); err != nil {
		t.Fatalf("a provider problem must not block saving: %v", err)
	}

	err := r.eng.GraphAction(r.ctx, "pre", "start", "t")
	var ve *engine.ValidationError
	if !errors.As(err, &ve) || len(ve.Report.Errors) != 1 || ve.Report.Errors[0].Path != "spec.nodes[1].model.provider" {
		t.Fatalf("start must be refused with the node's field: %v", err)
	}
	if !errors.Is(err, engine.ErrInvalid) {
		t.Fatal("preflight failures are invalid requests")
	}
	_, rec, _ := r.eng.GetGraph(r.ctx, "pre", 0)
	if rec.DesiredState == domain.GraphRunning {
		t.Fatal("a refused start must not change the desired state")
	}

	// Starting a single healthy node only checks that node.
	if err := r.eng.NodeAction(r.ctx, "pre", "ok", "start", "t"); err != nil {
		t.Fatalf("healthy node start: %v", err)
	}
	if err := r.eng.NodeAction(r.ctx, "pre", "ghost", "start", "t"); !errors.As(err, &ve) {
		t.Fatalf("broken node start: %v", err)
	}
}

func TestAdmissionValidationSeparatesWarningsFromRequirements(t *testing.T) {
	dir := t.TempDir()
	r := newRig(t, openStore(t, dir), memory.New(), dir, "i1")
	report := r.eng.ValidateForStart(r.ctx, preflightGraph())
	if report.Valid || len(report.Errors) != 1 {
		t.Fatal(report)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "warning"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "echo", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}}}, Channels: []domain.Channel{{ID: "audit", From: "echo"}}}}
	report = r.eng.ValidateForStart(r.ctx, g)
	if !report.Valid || len(report.Errors) != 0 || len(report.Warnings) == 0 {
		t.Fatal(report)
	}
}

func TestReviewRevalidatesCurrentToolCatalogue(t *testing.T) {
	dir := t.TempDir()
	rejected := true
	r := newRig(t, openStore(t, dir), memory.New(), dir, "tool-review", func(o *engine.Options) {
		o.ValidateToolGrants = func(_ context.Context, _ *domain.Graph) error {
			if rejected {
				return errors.New("fixture catalogue unavailable")
			}
			return nil
		}
	})
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "review"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}}}}}
	report := r.eng.ValidateForStart(r.ctx, g)
	if report.Valid || len(report.Errors) != 1 || report.Errors[0].Path != "spec.nodes.tools" {
		t.Fatal(report)
	}
	rejected = false
	if report = r.eng.ValidateForStart(r.ctx, g); !report.Valid {
		t.Fatal(report)
	}
}
