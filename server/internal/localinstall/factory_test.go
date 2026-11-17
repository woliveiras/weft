package localinstall

import (
	"context"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestNativeFactoryContract(t *testing.T) {
	g, err := factoryGraph()
	if err != nil {
		t.Fatal(err)
	}
	if g.Metadata.ID != "software-factory" || len(g.Spec.Nodes) != 6 {
		t.Fatal("factory identity")
	}
	for _, n := range g.Spec.Nodes {
		if n.Runtime.Kind != domain.RuntimeNative || n.Runtime.Endpoint != "" || n.Prompt == "" {
			t.Fatalf("invalid native agent %s", n.ID)
		}
		for _, grant := range n.Tools {
			if grant.Name == "git.write" || grant.Name == "shell.exec" {
				t.Fatal("unexpected mutation authority", n.ID)
			}
		}
	}
	if c := g.Channel("release"); c == nil || !c.Approval {
		t.Fatal("missing handoff approval")
	}
	if c := g.Channel("requests"); c == nil || len(c.Types) != 1 || c.Types[0] != "feature.request" {
		t.Fatal("input contract")
	}
}

func TestNativeFactoryV2Contract(t *testing.T) {
	g, err := FactoryV2Graph()
	if err != nil {
		t.Fatal(err)
	}
	if g.Metadata.ID != "software-factory-v2" || len(g.Spec.Nodes) != 5 {
		t.Fatal("factory identity")
	}
	writers := map[string]bool{}
	shells := map[string]bool{}
	for _, n := range g.Spec.Nodes {
		if n.Runtime.Kind != domain.RuntimeNative || n.Prompt == "" {
			t.Fatalf("invalid native agent %s", n.ID)
		}
		for _, grant := range n.Tools {
			switch grant.Name {
			case "git.write", "git.push", "scm.*":
				t.Fatalf("%s may not commit or push outside the hand-off", n.ID)
			case "filesystem.write", "filesystem.delete":
				writers[n.ID] = true
			case "shell.exec":
				shells[n.ID] = true
			}
		}
	}
	// Only the implementer edits; checks run where verification happens.
	if len(writers) != 1 || !writers["implementer"] || len(shells) != 2 || !shells["implementer"] || !shells["verifier"] {
		t.Fatalf("writers %v shells %v", writers, shells)
	}
	for id, skill := range map[string]string{"planner": "weft-planner", "implementer": "weft-implementer", "reviewer": "weft-reviewer", "verifier": "weft-verifier"} {
		n := g.Node(id)
		if n == nil || len(n.SkillIDs) != 1 || n.SkillIDs[0] != skill {
			t.Fatalf("%s must use the %s role skill: %+v", id, skill, n)
		}
	}
	if c := g.Channel("release"); c == nil || !c.Approval || c.From != "verifier" || c.Types[0] != "verify.passed" {
		t.Fatal("the hand-off must follow verification behind approval")
	}
	for _, loop := range []string{"rework", "verify-failed"} {
		if c := g.Channel(loop); c == nil || c.When == "" {
			t.Fatalf("%s loop must be bounded", loop)
		}
	}
}

type failedSeedStore struct {
	storage.Store
	failRead, failWrite bool
}

func (s failedSeedStore) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	if s.failRead {
		return nil, errors.New("fixture read failure")
	}
	return s.Store.GetSetting(ctx, key)
}
func (s failedSeedStore) PutSetting(ctx context.Context, key string, value []byte, revision int64, actor string) (int64, error) {
	if s.failWrite {
		return 0, errors.New("fixture write failure")
	}
	return s.Store.PutSetting(ctx, key, value, revision, actor)
}

func TestNativeFactorySeedPreservesEditsAndDeletion(t *testing.T) {
	root, web := fixture(t)
	if err := Init(root, web, 8787); err != nil {
		t.Fatal(err)
	}
	cfg, release, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	a, err := app.New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close(context.Background())
	original := a.Store
	a.Store = failedSeedStore{Store: original, failRead: true}
	if err := seedFactory(t.Context(), a); err == nil {
		t.Fatal("ignored read failure")
	}
	if graphs, err := original.ListGraphs(t.Context()); err != nil || len(graphs) != 0 {
		t.Fatal("write after failed read", err)
	}
	a.Store = failedSeedStore{Store: original, failWrite: true}
	if err := seedFactory(t.Context(), a); err == nil {
		t.Fatal("ignored marker failure")
	}
	g, err := original.GetGraph(t.Context(), "software-factory", 0)
	if err != nil {
		t.Fatal(err)
	}
	g.Metadata.Name = "My factory"
	if _, _, err := a.Engine.SaveGraph(t.Context(), g, g.Metadata.Version, "fixture"); err != nil {
		t.Fatal(err)
	}
	a.Store = original
	if err := seedFactory(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	g, err = original.GetGraph(t.Context(), "software-factory", 0)
	if err != nil || g.Metadata.Name != "My factory" || g.Metadata.Version != 2 {
		t.Fatal("customization lost", err)
	}
	if err := seedFactory(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if err := original.DeleteGraph(t.Context(), g.Metadata.ID); err != nil {
		t.Fatal(err)
	}
	if err := seedFactory(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := original.GetGraph(t.Context(), g.Metadata.ID, 0); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("deleted workflow recreated", err)
	}
}
