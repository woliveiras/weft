package api_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
)

const scmToken = "scm-api-secret"

// apiFakeSCM answers identity for the right token only.
type apiFakeSCM struct{ token string }

func (f apiFakeSCM) Identity(context.Context) (scm.Identity, error) {
	if f.token != scmToken {
		return scm.Identity{}, errors.New("401 bad credentials for " + f.token)
	}
	return scm.Identity{User: "bot", Repository: "acme/app", CanPush: true}, nil
}
func (apiFakeSCM) FindPullRequest(context.Context, string, string) (*scm.PullRequest, error) {
	return nil, nil
}
func (apiFakeSCM) CreatePullRequest(context.Context, scm.PullRequestInput) (*scm.PullRequest, error) {
	return nil, errors.New("not used")
}
func (apiFakeSCM) Comment(context.Context, int, string) error { return nil }
func (f apiFakeSCM) GitAuthHeader() string                    { return "Bearer " + f.token }
func (apiFakeSCM) RemoteURL() string                          { return "https://git.example/acme/app.git" }

func init() {
	scm.RegisterKind("api-fake", func(_ scm.Connection, tok string) (scm.Provider, error) { return apiFakeSCM{token: tok}, nil })
}

func TestSCMConnectionsThroughAPI(t *testing.T) {
	srv, a := setupApp(t, nil)
	body := `{"name":"app","kind":"api-fake","repository":"acme/app","gitUrl":"https://git.example/acme/app.git","token":"` + scmToken + `"}`
	for _, tok := range []string{viewerTok, operatorTok} {
		if c := call(t, srv, "POST", "/api/v1/scm/connections", tok, "application/json", body, nil); c != 403 {
			t.Fatalf("non-admin create: %d", c)
		}
	}
	var bad apiError
	if c := call(t, srv, "POST", "/api/v1/scm/connections", adminTok, "application/json", `{"name":"x","kind":"svn","repository":"a/b","token":"t"}`, &bad); c != 422 || bad.Details.Errors[0].Path != "kind" {
		t.Fatalf("invalid: %d %+v", c, bad)
	}
	if c, _, out := rawCall(t, "POST", srv.URL+"/api/v1/scm/connections", adminTok, `{"name":"x","token":"`+scmToken, nil); c != 400 || strings.Contains(out, scmToken) {
		t.Fatalf("malformed body must not be echoed: %d %s", c, out)
	}
	if c, _, out := rawCall(t, "POST", srv.URL+"/api/v1/scm/connections", adminTok, body, nil); c != 201 || strings.Contains(out, scmToken) || !strings.Contains(out, `"tokenConfigured":true`) {
		t.Fatalf("create: %d %s", c, out)
	}
	if c, _, out := rawCall(t, "GET", srv.URL+"/api/v1/scm/connections", viewerTok, "", nil); c != 200 || strings.Contains(out, scmToken) || strings.Contains(out, `"token":"v1.`) {
		t.Fatalf("list leaks credential material: %d %s", c, out)
	}
	st, _ := a.Store.GetSetting(t.Context(), scm.SettingKey)
	if strings.Contains(string(st.Value), scmToken) {
		t.Fatal("token stored in plain text")
	}
	var res struct {
		OK       bool
		Identity scm.Identity
		Error    string
	}
	if c := call(t, srv, "POST", "/api/v1/scm/connections/app/test", viewerTok, "", "", nil); c != 403 {
		t.Fatalf("viewer test: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/scm/connections/app/test", operatorTok, "", "", &res); c != 200 || !res.OK || res.Identity.User != "bot" {
		t.Fatalf("test: %d %+v", c, res)
	}
	// A wrong token fails the test without echoing it.
	if c := call(t, srv, "PUT", "/api/v1/scm/connections/app", adminTok, "application/json", `{"kind":"api-fake","repository":"acme/app","token":"wrong-token-value"}`, nil); c != 200 {
		t.Fatalf("update: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/scm/connections/app/test", operatorTok, "", "", &res); c != 200 || res.OK || strings.Contains(res.Error, "wrong-token-value") || !strings.Contains(res.Error, "[redacted]") {
		t.Fatalf("failing test: %+v", res)
	}
	if c := call(t, srv, "DELETE", "/api/v1/scm/connections/app", adminTok, "", "", nil); c != 204 {
		t.Fatalf("delete: %d", c)
	}
	if c := call(t, srv, "DELETE", "/api/v1/scm/connections/app", adminTok, "", "", nil); c != 404 {
		t.Fatalf("delete again: %d", c)
	}
	evs, _ := a.Store.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{domain.EvSCMChanged}})
	if len(evs) != 3 {
		t.Fatalf("audit = %d events", len(evs))
	}
	for _, ev := range evs {
		if strings.Contains(string(ev.Data), scmToken) || ev.Actor != "admin" {
			t.Fatalf("audit event: %+v", ev)
		}
	}
}

func TestTaskSCMHandOffThroughAPI(t *testing.T) {
	srv, a := setupApp(t, nil)
	graph := `{"metadata":{"id":"handoff","name":"Handoff"},"spec":{"nodes":[{"id":"worker","runtime":{"kind":"echo"},"loop":{"mode":"once"},"tools":[]},{"id":"integrator","runtime":{"kind":"echo"},"loop":{"mode":"once"},"tools":[]}],"channels":[{"id":"done","from":"worker","to":["integrator"],"approval":true}]}}`
	if c, _, b := rawCall(t, "POST", srv.URL+"/api/v1/graphs", adminTok, graph, nil); c != 201 {
		t.Fatal(c, b)
	}
	body := `{"title":"Fix parser","request":"Fix it","outcome":"implement","graphId":"handoff","graphVersion":1,"nodeId":"worker","resultNodeId":"integrator","scmConnection":"app"}`
	var d taskconfig.Draft
	if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", body, &d); c != 201 || d.SCMConnection != "app" {
		t.Fatalf("draft: %d %+v", c, d)
	}
	if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", strings.Replace(body, `"app"`, `"Bad Name"`, 1), nil); c != 400 && c != 422 {
		t.Fatalf("invalid connection name: %d", c)
	}
	var r taskrun.Review
	if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/review", viewerTok, "", "", &r); c != 200 || r.CanStart || len(r.Blockers) == 0 || !strings.Contains(strings.Join(r.Blockers, " "), `"app" no longer exists`) {
		t.Fatalf("missing connection must block: %d %+v", c, r)
	}
	if c := call(t, srv, "POST", "/api/v1/scm/connections", adminTok, "application/json", `{"name":"app","kind":"api-fake","repository":"acme/app","token":"`+scmToken+`"}`, nil); c != 201 {
		t.Fatal(c)
	}
	if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/review", viewerTok, "", "", &r); c != 200 || !r.CanStart {
		t.Fatalf("review: %d %+v", c, r)
	}
	if c := taskStart(t, srv.URL, d.ID, adminTok, d.Revision); c != 202 {
		t.Fatal(c)
	}
	gid := taskrun.ExecutionID(d.ID)
	g, err := a.Store.GetGraph(t.Context(), gid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var push, pr bool
	for _, gr := range g.Node("integrator").Tools {
		push = push || (gr.Name == "git.push" && len(gr.Repositories) == 1 && gr.Repositories[0] == "app")
		pr = pr || (gr.Name == "scm.pull_request.create" && len(gr.Repositories) == 1 && gr.Repositories[0] == "app")
	}
	if !push || !pr || len(g.Node("worker").Tools) != 0 || g.Metadata.Labels[domain.LabelTaskSCM] != "app" {
		t.Fatalf("execution grants: %+v / %+v", g.Node("integrator").Tools, g.Node("worker").Tools)
	}
	// The task's progress reports what the hand-off did.
	a.Bus.Publish(t.Context(), domain.NewEvent(domain.EvBranchPushed, gid, map[string]any{"branch": "weft/task-x", "repository": "acme/app"}))
	a.Bus.Publish(t.Context(), domain.NewEvent(domain.EvPullRequestOpened, gid, map[string]any{"url": "https://git.example/pr/3", "number": 3}))
	var v taskrun.View
	if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/progress", viewerTok, "", "", &v); c != 200 || v.Delivery == nil || v.Delivery.PullRequestNumber != 3 || v.Delivery.Branch != "weft/task-x" {
		t.Fatalf("progress delivery: %d %+v", c, v.Delivery)
	}
}
