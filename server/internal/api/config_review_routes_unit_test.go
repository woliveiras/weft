package api

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/reviewroute"
)

func TestReviewRouteDependencyRequiresSavedExplicitSourcesAndDestinations(t *testing.T) {
	in := reviewroute.Input{Name: "portable", Connection: "team", GraphID: "review", GraphVersion: 7, Enabled: true}
	for _, kind := range []string{"SCMConnection", "Graph"} {
		name := in.Connection
		if kind == "Graph" {
			name = in.GraphID
		}
		prepared := []configPrepared{{Doc: installation.New(kind, name, map[string]any{})}}
		for _, action := range []string{"create", "update", "error", "conflict"} {
			plan := installation.Plan{Objects: []installation.ObjectPlan{{Action: action}}}
			if err := validateReviewRouteDependencies(in, configObject{}, prepared, &plan); err == nil {
				t.Fatal("planned source/destination rewrite accepted", kind, action)
			}
		}
		plan := installation.Plan{Objects: []installation.ObjectPlan{{Action: "unchanged"}}}
		if err := validateReviewRouteDependencies(in, configObject{}, prepared, &plan); err != nil {
			t.Fatal(err)
		}
		prepared[0].Doc.Metadata["name"] = "unrelated"
		plan.Objects[0].Action = "update"
		if err := validateReviewRouteDependencies(in, configObject{}, prepared, &plan); err != nil {
			t.Fatal("unrelated configuration affected route", err)
		}
	}
}

func TestReviewRouteDependencyAllowsOnlyExactOfflineDisable(t *testing.T) {
	in := reviewroute.Input{Name: "portable", Connection: "team", GraphID: "review", GraphVersion: 7, Enabled: true}
	old := newConfigObject("PullRequestReviewRoute", in.Name, in, 3)
	delete(old.Doc.Spec, "name")
	prepared := []configPrepared{{Doc: installation.New("Graph", in.GraphID, map[string]any{})}}
	plan := installation.Plan{Objects: []installation.ObjectPlan{{Action: "update"}}}
	in.Enabled = false
	if err := validateReviewRouteDependencies(in, old, prepared, &plan); err != nil {
		t.Fatal(err)
	}
	in.GraphVersion++
	if err := validateReviewRouteDependencies(in, old, prepared, &plan); err == nil {
		t.Fatal("offline disabling adopted a different workflow version")
	}
}
