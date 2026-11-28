package api

import (
	"reflect"
	"testing"

	"github.com/woliveiras/weft/server/internal/installation"
)

func TestChatProjectSettingsHydrateOnlyExplicitPartialUpdates(t *testing.T) {
	base := installation.New("Project", "prj_fixture", map[string]any{"path": "/fixture/repository", "executionMode": "workflow", "checks": []any{"existing"}})
	snapshot := map[string]configObject{base.Key(): {Doc: base}}
	changes := []installation.Document{
		installation.New("Project", "prj_fixture", map[string]any{"executionMode": "sandbox"}),
		installation.New("Project", "prj_fixture", map[string]any{"path": "/explicit/source", "executionMode": "sandbox"}),
		installation.New("Project", "prj_unknown", map[string]any{"executionMode": "sandbox"}),
	}
	actual := hydrateChatSettings(changes, snapshot)
	if actual[0].Spec["executionMode"] != "sandbox" || actual[0].Spec["path"] != "/fixture/repository" || !reflect.DeepEqual(actual[0].Spec["checks"], base.Spec["checks"]) {
		t.Fatalf("existing settings not hydrated: %+v", actual[0].Spec)
	}
	if base.Spec["executionMode"] != "workflow" || len(actual[1].Spec) != 2 || len(actual[2].Spec) != 1 {
		t.Fatalf("changed snapshot, explicit source or missing target: %+v %+v", base, actual)
	}
}

func TestWorkflowProfileHydrationPreservesMetadataAndTaskDefaults(t *testing.T) {
	old := installation.Document{Kind: "Graph", Metadata: map[string]any{"id": "release", "name": "Release", "revision": "original"}, Spec: map[string]any{"task": map[string]any{"resultNodeId": "qa"}, "nodes": []string{"qa"}}, Extensions: map[string]any{"x-owner": "team"}}
	patch := installation.Document{Kind: "Graph", Metadata: map[string]any{"id": "release", "revision": "stale"}, Spec: map[string]any{"task": map[string]any{"authorizationProfile": "manual"}}}
	updated := hydrateChatSettings([]installation.Document{patch}, map[string]configObject{old.Key(): {Doc: old}})[0]
	if updated.Metadata["name"] != "Release" || updated.Revision() != "stale" || updated.Extensions["x-owner"] != "team" || !reflect.DeepEqual(updated.Spec["task"], map[string]any{"resultNodeId": "qa", "authorizationProfile": "manual"}) {
		t.Fatalf("partial update: %+v", updated)
	}
	if old.Revision() != "original" || len(old.Spec["task"].(map[string]any)) != 1 {
		t.Fatal("mutated stored snapshot")
	}
	patch.Spec["nodes"] = []string{"qa"}
	if partialWorkflowProfile(patch) {
		t.Fatal("treated a full graph edit as an approval profile patch")
	}
}
