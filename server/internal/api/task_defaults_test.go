package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestWorkflowTaskDefaultsRoundTripAndValidation(t *testing.T) {
	srv, app := setupApp(t, nil)
	const doc = `{"metadata":{"id":"task-defaults","name":"Task defaults"},"spec":{"task":{"nodeId":"worker","resultNodeId":"worker","channelId":"requests","messageType":"work.request","projectId":"project-a","scmConnection":"team-scm"},"nodes":[{"id":"worker","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],"channels":[{"id":"requests","to":["worker"],"types":["work.*"]}]}}`
	if got := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", doc, nil); got != http.StatusCreated {
		t.Fatalf("create task defaults: %d", got)
	}
	var response struct{ Graph domain.Graph }
	if got := call(t, srv, "GET", "/api/v1/graphs/task-defaults", viewerTok, "", "", &response); got != http.StatusOK {
		t.Fatalf("read task defaults: %d", got)
	}
	if response.Graph.Spec.Task == nil || response.Graph.Spec.Task.ProjectID != "project-a" || response.Graph.Spec.Task.SCMConnection != "team-scm" || response.Graph.Spec.Task.MessageType != "work.request" {
		t.Fatalf("lost saved suggestions: %+v", response.Graph.Spec.Task)
	}
	for _, invalid := range []string{
		strings.Replace(doc, `"nodeId":"worker"`, `"nodeId":"missing"`, 1),
		strings.Replace(doc, `"messageType":"work.request"`, `"messageType":"other.request"`, 1),
		strings.Replace(doc, `"loop":{"mode":"event"}`, `"disabled":true,"loop":{"mode":"event"}`, 1),
	} {
		if got := callIfMatch(t, srv, "PUT", "/api/v1/graphs/task-defaults", adminTok, invalid, 1, nil); got != http.StatusUnprocessableEntity {
			t.Fatalf("invalid default update: %d", got)
		}
	}
	stored, err := app.Store.GetGraph(t.Context(), "task-defaults", 0)
	if err != nil || stored.Metadata.Version != 1 {
		t.Fatalf("invalid update changed saved version: %+v %v", stored, err)
	}
	changed := strings.Replace(doc, `"projectId":"project-a"`, `"projectId":"project-b"`, 1)
	if got := callIfMatch(t, srv, "PUT", "/api/v1/graphs/task-defaults", adminTok, changed, 1, nil); got != http.StatusOK {
		t.Fatalf("update defaults: %d", got)
	}
	if got := callIfMatch(t, srv, "PUT", "/api/v1/graphs/task-defaults", adminTok, doc, 1, nil); got != http.StatusConflict {
		t.Fatalf("stale defaults save: %d", got)
	}
	if got := callIfMatch(t, srv, "PUT", "/api/v1/graphs/task-defaults", operatorTok, doc, 2, nil); got != http.StatusForbidden {
		t.Fatalf("operator changed defaults: %d", got)
	}
	status, err := app.Engine.Status(t.Context(), "task-defaults")
	if err != nil || status.State != "stopped" {
		t.Fatalf("saving defaults started workflow: %+v %v", status, err)
	}
}
