package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
)

func retryTaskRequest(t *testing.T, base, id, token, revision, body string) (int, http.Header, *taskconfig.Draft) {
	t.Helper()
	req, err := http.NewRequest("POST", base+"/api/v1/tasks/"+id+"/retry", strings.NewReader(body))
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
	var d taskconfig.Draft
	if resp.StatusCode == 200 || resp.StatusCode == 201 {
		if err = json.NewDecoder(resp.Body).Decode(&d); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, resp.Header, &d
}

func TestFailedTaskRetryAPI(t *testing.T) {
	srv, a := setupApp(t, nil)
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); c != 201 {
		t.Fatal(c)
	}
	var source taskconfig.Draft
	call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", taskBody, &source)
	authorization := taskauth.Manager{Store: a.Store, Tools: a.Tools}
	scope, err := authorization.Review(t.Context(), source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = authorization.Save(t.Context(), source.ID, taskauth.Full, scope.Scope, 0, "admin", true); err != nil {
		t.Fatal(err)
	}
	m := taskconfig.Manager{Store: a.Store}
	failed, err := m.Transition(t.Context(), &source, "failed", "system:task")
	if err != nil {
		t.Fatal(err)
	}
	source = *failed
	revision := fmt.Sprint(source.Revision)
	for _, tc := range []struct {
		name, token, revision, body string
		want                        int
	}{
		{"anonymous", "", revision, `{"confirm":true}`, 401}, {"reader", viewerTok, revision, `{"confirm":true}`, 403},
		{"missing revision", operatorTok, "", `{"confirm":true}`, 400}, {"bad revision", operatorTok, "invalid", `{"confirm":true}`, 400},
		{"stale revision", operatorTok, "1", `{"confirm":true}`, 409}, {"no confirmation", operatorTok, revision, `{}`, 400},
		{"false confirmation", operatorTok, revision, `{"confirm":false}`, 400}, {"unknown field", operatorTok, revision, `{"confirm":true,"force":true}`, 400},
		{"trailing JSON", operatorTok, revision, `{"confirm":true} {}`, 400}, {"malformed JSON", operatorTok, revision, `{`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := retryTaskRequest(t, srv.URL, source.ID, tc.token, tc.revision, tc.body)
			if code != tc.want {
				t.Fatal(code, tc.want)
			}
		})
	}
	page, err := m.List(t.Context(), "", 100)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("invalid retry created task", page, err)
	}
	code, headers, child := retryTaskRequest(t, srv.URL, source.ID, operatorTok, revision, `{"confirm":true}`)
	if code != 201 || child.Status != "draft" || child.RetryOfTaskID != source.ID || headers.Get("ETag") != `"1"` || headers.Get("Location") != "/api/v1/tasks/"+child.ID {
		t.Fatal(code, headers, child)
	}
	if !reflect.DeepEqual(child.Input, source.Input) || child.ExecutionGraphID != "" {
		t.Fatal("input reset or execution copied", child)
	}
	code, _, replay := retryTaskRequest(t, srv.URL, source.ID, adminTok, revision, `{"confirm":true}`)
	if code != 200 || replay.ID != child.ID {
		t.Fatal("replay", code, replay)
	}
	kept, err := m.Get(t.Context(), source.ID)
	if err != nil || !reflect.DeepEqual(kept, &source) {
		t.Fatal("source changed", kept, err)
	}
	graphs, err := a.Store.ListGraphs(t.Context())
	if err != nil || len(graphs) != 1 {
		t.Fatal("retry created graph", graphs, err)
	}
	executions, err := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{})
	if err != nil || len(executions) != 0 {
		t.Fatal("retry executed", executions, err)
	}
	var auth taskauth.Review
	if c := call(t, srv, "GET", "/api/v1/tasks/"+child.ID+"/authorization", viewerTok, "", "", &auth); c != 200 || auth.Record != nil {
		t.Fatal("authority inherited or review unavailable", c, auth)
	}
	var review taskrun.Review
	if c := call(t, srv, "GET", "/api/v1/tasks/"+child.ID+"/review", viewerTok, "", "", &review); c != 200 || review.Task.ID != child.ID {
		t.Fatal("fresh review unavailable", c, review)
	}
	if c, _, _ := retryTaskRequest(t, srv.URL, child.ID, operatorTok, "1", `{"confirm":true}`); c != 409 {
		t.Fatal("draft retry accepted", c)
	}
	if c, _, _ := retryTaskRequest(t, srv.URL, "tsk_unknown", operatorTok, "1", `{"confirm":true}`); c != 404 {
		t.Fatal("unknown source", c)
	}
	// A changed source workflow is kept as an explicit stale pin in the child.
	var another taskconfig.Draft
	call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", taskBody, &another)
	failed, err = m.Transition(t.Context(), &another, "failed", "system:task")
	if err != nil {
		t.Fatal(err)
	}
	g, err := a.Store.GetGraph(t.Context(), source.GraphID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.SaveGraph(t.Context(), g, g.Metadata.Version); err != nil {
		t.Fatal(err)
	}
	code, _, stale := retryTaskRequest(t, srv.URL, failed.ID, operatorTok, fmt.Sprint(failed.Revision), `{"confirm":true}`)
	if code != 201 || stale.GraphVersion != 1 {
		t.Fatal(code, stale)
	}
	if c := call(t, srv, "GET", "/api/v1/tasks/"+stale.ID+"/review", viewerTok, "", "", &review); c != 200 || review.CanStart || len(review.Blockers) == 0 {
		t.Fatal("stale pin admitted", c, review)
	}
}
