package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestPortableFailedAdmissionRetryAPI(t *testing.T) {
	for _, kind := range []string{"jira", "jira-dc"} {
		t.Run(kind, func(t *testing.T) {
			srv, a := setupApp(t, nil)
			if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); code != 201 {
				t.Fatal(code)
			}
			manager := trigger.Manager{Store: a.Store}
			tr, err := manager.Create(t.Context(), trigger.Input{Name: "Retry fixture", Workflow: "task-fixture", NodeID: "reader", Source: trigger.SourceConfig{Kind: "github", ConnectionID: "gh", Repository: "acme/app"}}, "admin")
			if err != nil {
				t.Fatal(err)
			}
			source := trigger.SourceConfig{Kind: kind, ConnectionID: "jira-team"}
			origin := taskconfig.IssueOrigin{Kind: kind, ConnectionID: source.ConnectionID, ConnectionRevision: 2, Key: "APP-7"}
			if kind == "jira" {
				source.CloudID = "11111111-2222-3333-4444-555555555555"
				origin.Instance = "https://api.atlassian.com/ex/jira/" + source.CloudID
			} else {
				source.SiteURL = "https://jira.example/context"
				source.ProjectID = "10001"
				origin.Instance = source.SiteURL
				origin.ProjectID = source.ProjectID
			}
			tm := taskconfig.Manager{Store: a.Store}
			draft, err := tm.SaveTriggeredIssue(t.Context(), taskconfig.Input{Title: "Issue", Request: "Fix parser", Outcome: "implement", GraphID: "task-fixture", GraphVersion: 1, NodeID: "reader"}, "admin", origin)
			if err != nil {
				t.Fatal(err)
			}
			key := trigger.DedupKey(&trigger.Trigger{Workflow: tr.Workflow, Source: source}, trigger.Issue{Source: kind, Key: origin.Key})
			tr, err = manager.Update(t.Context(), tr.ID, "admin", func(current *trigger.Trigger) error {
				current.Source = source
				current.State = trigger.Paused
				current.Processed = map[string]string{key: "failed"}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/triggers/" + tr.ID + "/retry-failed"
			raw, _ := json.Marshal(map[string]any{"key": origin.Key, "taskId": draft.ID, "confirm": true})
			body := string(raw)
			for _, token := range []string{viewerTok, operatorTok} {
				if code := call(t, srv, "POST", path, token, "application/json", body, nil); code != 403 {
					t.Fatal("non-admin recovery", code)
				}
			}
			if code := call(t, srv, "POST", path, adminTok, "application/json", strings.Replace(body, "true", "false", 1), nil); code != 400 {
				t.Fatal("missing consent", code)
			}
			if code := call(t, srv, "POST", path, adminTok, "application/json", strings.Replace(body, "APP-7", "APP-8", 1), nil); code != 409 {
				t.Fatal("mismatched issue", code)
			}
			var out trigger.Trigger
			if code := call(t, srv, "POST", path, adminTok, "application/json", body, &out); code != 200 {
				t.Fatal("portable retry rejected", code)
			}
			if out.FailedRetries[key].TaskID != draft.ID || out.FailedRetries[key].Consumed || out.Processed[key] != "failed" {
				t.Fatal("retry replaced original admission", out)
			}
			if code := call(t, srv, "POST", path, adminTok, "application/json", body, nil); code != 409 {
				t.Fatal("replayed retry", code)
			}
			after, err := tm.Get(t.Context(), draft.ID)
			if err != nil || after.Status != "cancelled" || after.TriggerIssueOrigin == nil || *after.TriggerIssueOrigin != origin {
				t.Fatal("origin not preserved", after, err)
			}
			events, err := a.Store.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{"trigger.failed_retry_requested"}})
			if err != nil || len(events) != 1 || events[0].Actor != "admin" {
				t.Fatal("retry audit", events, err)
			}
		})
	}
}
