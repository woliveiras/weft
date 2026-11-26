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
	actual := hydrateChatProjectSettings(changes, snapshot)
	if actual[0].Spec["executionMode"] != "sandbox" || actual[0].Spec["path"] != "/fixture/repository" || !reflect.DeepEqual(actual[0].Spec["checks"], base.Spec["checks"]) {
		t.Fatalf("existing settings not hydrated: %+v", actual[0].Spec)
	}
	if base.Spec["executionMode"] != "workflow" || len(actual[1].Spec) != 2 || len(actual[2].Spec) != 1 {
		t.Fatalf("changed snapshot, explicit source or missing target: %+v %+v", base, actual)
	}
}
