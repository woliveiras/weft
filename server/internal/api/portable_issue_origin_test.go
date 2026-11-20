package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestJiraTriggerOriginSurvivesRealLaunchAndRequiresDelivery(t *testing.T) {
	srv, a := setupApp(t, nil)
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); code != 201 {
		t.Fatal(code)
	}
	tr := &trigger.Trigger{ID: "jira-fixture", Workflow: "task-fixture", NodeID: "reader", Source: trigger.SourceConfig{Kind: "jira", ConnectionID: "jira-team", CloudID: "11111111-2222-3333-4444-555555555555"}, Consent: trigger.Consent{Actor: "admin", WorkflowVersion: 1, TrackerRevision: 8, Authorization: taskauth.Ask}}
	issue := trigger.Issue{Source: "jira", Key: "APP-7", Title: "Explain parser", Body: "Expected result", URL: "https://team.atlassian.net/browse/APP-7"}
	id, err := a.Triggers.Tasks.Start(t.Context(), tr, issue)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := (taskconfig.Manager{Store: a.Store}).Get(t.Context(), id)
	if err != nil || draft.TriggerIssueOrigin == nil || draft.TriggerIssueOrigin.ConnectionID != "jira-team" || draft.TriggerIssueOrigin.ConnectionRevision != 8 || draft.TriggerIssueOrigin.Instance != "https://api.atlassian.com/ex/jira/"+tr.Source.CloudID || draft.TriggerIssueOrigin.Key != issue.Key || draft.Source != nil {
		t.Fatalf("origin %+v %v", draft, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		view, err := a.API.TaskRunner().View(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if view.State == "blocked" && len(view.Executions) > 0 {
			if !strings.Contains(strings.Join(view.Blockers, " "), "required pull request") {
				t.Fatalf("missing delivery context %+v", view.Blockers)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Jira task did not settle as blocked without PR delivery")
}
