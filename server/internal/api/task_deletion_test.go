package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func deleteDraftRequest(t *testing.T, base, id, token, revision, body string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, base+"/api/v1/tasks/"+id, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if revision != "" {
		req.Header.Set("If-Match", revision)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
func TestTaskDraftDeletionAPI(t *testing.T) {
	srv, a := setupApp(t, nil)
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); c != 201 {
		t.Fatal(c)
	}
	var d taskconfig.Draft
	if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", taskBody, &d); c != 201 {
		t.Fatal(c)
	}
	for _, tc := range []struct {
		name, token, revision, body string
		status                      int
	}{
		{"anonymous", "", "1", `{"confirm":true}`, 401},
		{"viewer", viewerTok, "1", `{"confirm":true}`, 403},
		{"missing revision", operatorTok, "", `{"confirm":true}`, 400},
		{"malformed revision", operatorTok, "oops", `{"confirm":true}`, 400},
		{"stale", operatorTok, "2", `{"confirm":true}`, 409},
		{"no confirmation", operatorTok, "1", `{}`, 400},
		{"false confirmation", operatorTok, "1", `{"confirm":false}`, 400},
		{"unknown fields", operatorTok, "1", `{"confirm":true,"force":true}`, 400},
		{"trailing body", operatorTok, "1", `{"confirm":true} {}`, 400},
		{"malformed", operatorTok, "1", `{`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := deleteDraftRequest(t, srv.URL, d.ID, tc.token, tc.revision, tc.body); got != tc.status {
				t.Fatalf("got %d want %d", got, tc.status)
			}
			if _, err := (taskconfig.Manager{Store: a.Store}).Get(t.Context(), d.ID); err != nil {
				t.Fatal("invalid request changed task", err)
			}
		})
	}
	if c := deleteDraftRequest(t, srv.URL, d.ID, operatorTok, `"1"`, `{"confirm":true}`); c != 204 {
		t.Fatal(c)
	}
	for _, suffix := range []string{"", "/review", "/progress", "/authorization"} {
		if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+suffix, viewerTok, "", "", nil); c != 404 {
			t.Fatal(suffix, c)
		}
	}
	if c := taskStart(t, srv.URL, d.ID, adminTok, d.Revision); c != 404 {
		t.Fatal("deleted draft started", c)
	}
	if c := deleteDraftRequest(t, srv.URL, d.ID, adminTok, "1", `{"confirm":true}`); c != 404 {
		t.Fatal("repeated delete", c)
	}
	for _, order := range []string{"", "?order=updated_desc"} {
		var page taskconfig.Page
		if c := call(t, srv, "GET", "/api/v1/tasks"+order, viewerTok, "", "", &page); c != 200 || len(page.Items) != 0 {
			t.Fatal(c, page)
		}
	}
	gs, err := a.Store.ListGraphs(t.Context())
	if err != nil || len(gs) != 1 {
		t.Fatal("deletion changed workflows", gs, err)
	}
	xs, err := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{})
	if err != nil || len(xs) != 0 {
		t.Fatal("deletion executed task", xs, err)
	}
	events, err := a.Store.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{"task.transition_requested"}})
	if err != nil || len(events) != 1 {
		t.Fatal("missing deletion audit", events, err)
	}
}
func TestTaskDeletionRejectsEveryPreparedOrExecutedState(t *testing.T) {
	srv, a := setupApp(t, nil)
	call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil)
	manager := taskconfig.Manager{Store: a.Store}
	for _, status := range []string{"preparing", "environment_pending", "environment_build_requested", "environment_cancel_requested", "starting", "running", "blocked", "failed", "succeeded", "cancel_requested", "cancelled", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			var d taskconfig.Draft
			if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", taskBody, &d); c != 201 {
				t.Fatal(c)
			}
			transitioned, err := manager.Transition(t.Context(), &d, status, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			if c := deleteDraftRequest(t, srv.URL, d.ID, adminTok, fmt.Sprint(transitioned.Revision), `{"confirm":true}`); c != 409 {
				t.Fatalf("state %s deletable: %d", status, c)
			}
			kept, err := manager.Get(t.Context(), d.ID)
			if err != nil || kept.Status != status {
				t.Fatal("task changed", kept, err)
			}
		})
	}
	// A genuinely completed task, including its execution copy and evidence, stays intact.
	var executed taskconfig.Draft
	call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", taskBody, &executed)
	if c := taskStart(t, srv.URL, executed.ID, adminTok, executed.Revision); c != 202 {
		t.Fatal(c)
	}
	current, err := manager.Get(t.Context(), executed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c := deleteDraftRequest(t, srv.URL, current.ID, adminTok, fmt.Sprint(current.Revision), `{"confirm":true}`); c != 409 {
		t.Fatal("executed task deleted", c)
	}
	if _, err := a.Store.GetGraph(t.Context(), current.ExecutionGraphID, 0); err != nil {
		t.Fatal("execution copy lost", err)
	}
}
