package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

const taskGraph = `{"metadata":{"id":"task-fixture","name":"Task fixture"},"spec":{"nodes":[{"id":"reader","runtime":{"kind":"echo"},"loop":{"mode":"once"},"tools":[]}]}}`
const taskBody = `{"title":"Read code","request":"Explain architecture","outcome":"understand","graphId":"task-fixture","graphVersion":1,"nodeId":"reader"}`

func TestTaskDraftPersistenceAndReview(t *testing.T) {
	srv, a := setupApp(t, nil)
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); c != 201 {
		t.Fatal(c)
	}
	var d taskconfig.Draft
	if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", taskBody, &d); c != 201 || d.Revision != 1 || d.Status != "draft" {
		t.Fatal(c, d)
	}
	reloaded, err := (taskconfig.Manager{Store: a.Store}).Get(t.Context(), d.ID)
	if err != nil || reloaded.Request != "Explain architecture" {
		t.Fatal(reloaded, err)
	}
	var page taskconfig.Page
	if c := call(t, srv, "GET", "/api/v1/tasks?limit=1", viewerTok, "", "", &page); c != 200 || len(page.Items) != 1 {
		t.Fatal(c, page)
	}
	var review struct {
		CanStart bool     `json:"canStart"`
		Blockers []string `json:"blockers"`
	}
	if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/review", viewerTok, "", "", &review); c != 200 || review.CanStart || len(review.Blockers) == 0 {
		t.Fatal(c, review)
	}
	update := func(rev string) int {
		req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/tasks/"+d.ID, strings.NewReader(strings.Replace(taskBody, "Read code", "Updated", 1)))
		req.Header.Set("Authorization", "Bearer "+operatorTok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", rev)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if c := update(`"1"`); c != 200 {
		t.Fatal(c)
	}
	if c := update(`"1"`); c != 409 {
		t.Fatal("stale revision", c)
	}
	if c := call(t, srv, "PUT", "/api/v1/tasks/"+d.ID, operatorTok, "application/json", taskBody, nil); c != 400 {
		t.Fatal(c)
	}
	xs, err := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: "task-fixture"})
	if err != nil || len(xs) != 0 {
		t.Fatal("draft executed", xs, err)
	}

}

func TestTaskDraftValidationAndRoles(t *testing.T) {
	srv := setup(t)
	call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil)
	for _, tc := range []struct {
		body, token string
		status      int
	}{
		{taskBody, viewerTok, 403}, {taskBody, "", 401}, {`{`, operatorTok, 400}, {taskBody + ` {}`, operatorTok, 400},
		{strings.Replace(taskBody, `"title":`, `"tools":[],"title":`, 1), operatorTok, 400},
		{strings.Replace(taskBody, `"reader"`, `"missing"`, 1), operatorTok, 422},
		{strings.Replace(taskBody, `"graphVersion":1`, `"graphVersion":2`, 1), operatorTok, 409},
		{strings.Replace(taskBody, `"understand"`, `"unknown"`, 1), operatorTok, 422},
	} {
		if c := call(t, srv, "POST", "/api/v1/tasks", tc.token, "application/json", tc.body, nil); c != tc.status {
			t.Fatalf("got %d want %d", c, tc.status)
		}
	}
	for _, path := range []string{"/api/v1/tasks?limit=0", "/api/v1/tasks?limit=101", "/api/v1/tasks?after=bad"} {
		if c := call(t, srv, "GET", path, viewerTok, "", "", nil); c != 422 {
			t.Fatal(path, c)
		}
	}
	if c := call(t, srv, "GET", "/api/v1/tasks/unknown", viewerTok, "", "", nil); c != 404 {
		t.Fatal(c)
	}
	var a, b taskconfig.Draft
	call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", taskBody, &a)
	call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", taskBody, &b)
	var page taskconfig.Page
	call(t, srv, "GET", "/api/v1/tasks?limit=1", viewerTok, "", "", &page)
	if len(page.Items) != 1 || page.Next == "" {
		t.Fatal(page)
	}
	var next taskconfig.Page
	call(t, srv, "GET", "/api/v1/tasks?limit=1&after="+page.Next, viewerTok, "", "", &next)
	if len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID || next.Next != "" {
		t.Fatal(next)
	}
	// The wire representation never gains tool grants or generated runtime state.
	encoded, _ := json.Marshal(a)
	if strings.Contains(string(encoded), `"tools"`) {
		t.Fatal(string(encoded))
	}
}

func TestTaskIssueSourcePersistsWithoutAuthority(t *testing.T) {
	srv, a := setupApp(t, nil)
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); c != 201 {
		t.Fatal(c)
	}
	body := strings.Replace(taskBody, `"title":`, `"source":{"kind":"github","connectionId":"personal","reference":"octocat/Hello-World#42"},"resultNodeId":"reader","title":`, 1)
	var d taskconfig.Draft
	if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", body, &d); c != 201 {
		t.Fatal(c)
	}
	var review struct {
		CanStart bool     `json:"canStart"`
		Blockers []string `json:"blockers"`
	}
	if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/review", viewerTok, "", "", &review); c != 200 || review.CanStart || !strings.Contains(strings.Join(review.Blockers, " "), "mcp.personal.issue_read") {
		t.Fatal(c, review)
	}
	loaded, err := (taskconfig.Manager{Store: a.Store}).Get(t.Context(), d.ID)
	if err != nil || loaded.Source == nil || loaded.Source.Reference != "octocat/Hello-World#42" {
		t.Fatal(loaded, err)
	}
	if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", strings.Replace(body, "octocat/Hello-World#42", "https://user:secret@github.com/o/r/issues/1", 1), nil); c != 422 {
		t.Fatal(c)
	}
	xs, err := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: "task-fixture"})
	if err != nil || len(xs) != 0 {
		t.Fatal(xs, err)
	}
}

func TestPromptAPIResumesWithoutAuthority(t *testing.T) {
	srv, a := setupApp(t, nil)
	body := `{"request":"Explain this project"}`
	for _, tc := range []struct {
		token, body string
		status      int
	}{{viewerTok, body, 403}, {operatorTok, `{"request":"x","tools":[]}`, 400}, {operatorTok, `{"request":""}`, 422}, {operatorTok, body + ` {}`, 400}} {
		if c := call(t, srv, "POST", "/api/v1/tasks/prompt", tc.token, "application/json", tc.body, nil); c != tc.status {
			t.Fatal(c, tc)
		}
	}
	var d taskconfig.Draft
	if c := call(t, srv, "POST", "/api/v1/tasks/prompt", operatorTok, "application/json", body, &d); c != 200 || d.Resolution.State != "needs_model" {
		t.Fatal(c, d)
	}
	reloaded, err := (taskconfig.Manager{Store: a.Store}).Get(t.Context(), d.ID)
	if err != nil || reloaded.Request != "Explain this project" {
		t.Fatal(reloaded, err)
	}
	var review struct {
		CanStart bool
		Blockers []string
	}
	if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/review", viewerTok, "", "", &review); c != 200 || review.CanStart || len(review.Blockers) == 0 {
		t.Fatal(c, review)
	}
	req, _ := json.Marshal(map[string]any{"request": "Explain again", "id": d.ID, "revision": d.Revision})
	if c := call(t, srv, "POST", "/api/v1/tasks/prompt", operatorTok, "application/json", string(req), &d); c != 200 || d.Request != "Explain again" {
		t.Fatal(c, d)
	}
	if c := call(t, srv, "POST", "/api/v1/tasks/prompt", operatorTok, "application/json", string(req), nil); c != 409 {
		t.Fatal(c)
	}
}
