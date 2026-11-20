package api_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestLifecycleRecoveryAPIIsRevisionScopedAndNeverRetriesUnknownPost(t *testing.T) {
	posts := 0
	status := "10"
	deny := false
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer lifecycle-fixture-token" {
			t.Error("wrong credential")
		}
		if deny {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/jira/rest/api/2/issue/APP-7":
			fmt.Fprintf(w, `{"id":"10007","key":"APP-7","fields":{"summary":"Sample","updated":"2026-11-20T12:00:00.000+0000","issuelinks":[],"project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":%q,"name":"Review","statusCategory":{"key":"new"}}}}`, status)
		case "/jira/rest/api/2/issue/10007/transitions":
			if r.Method == "GET" {
				fmt.Fprint(w, `{"transitions":[{"id":"31","name":"Review","to":{"id":"20","name":"Review"},"fields":{}}]}`)
				return
			}
			posts++
			status = "20"
			w.WriteHeader(503)
		default:
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	old := http.DefaultTransport
	http.DefaultTransport = remote.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = old })
	srv, a := setupApp(t, nil)
	connection := fmt.Sprintf(`{"name":"native","kind":"jira-dc","siteUrl":%q,"projectId":"10001","enabled":true,"projectRestricted":true,"token":"lifecycle-fixture-token"}`, remote.URL+"/jira")
	if code := call(t, srv, "POST", "/api/v1/issue-connections", adminTok, "application/json", connection, nil); code != 201 {
		t.Fatal(code)
	}
	mapping := `{"name":"reviewing","connection":"native","connectionRevision":1,"sampleIssueKey":"APP-7","sampleIssueId":"10007","issueTypeId":"10002","fromStatusId":"10","transitionId":"31","targetStatusId":"20","issueUpdated":"2026-11-20T12:00:00.000+0000","enabled":true}`
	if code := call(t, srv, "POST", "/api/v1/issue-workflow-mappings", adminTok, "application/json", mapping, nil); code != 201 {
		t.Fatal(code)
	}
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); code != 201 {
		t.Fatal(code)
	}
	input := fmt.Sprintf(`{"name":"Native lifecycle","workflow":"task-fixture","nodeId":"reader","source":{"kind":"jira-dc","connectionId":"native","siteUrl":%q,"projectId":"10001"},"label":"weft","readinessDisabled":true,"authorizationMode":"workspace","lifecycleMappings":{"prDelivered":{"name":"reviewing","revision":1}}}`, remote.URL+"/jira")
	var tr trigger.Trigger
	if code := call(t, srv, "POST", "/api/v1/triggers", adminTok, "application/json", input, &tr); code != 201 {
		t.Fatal(code)
	}
	makeJob := func(task, state string) trigger.LifecycleSync {
		raw, _ := json.Marshal([]string{tr.ID, task, "pr_delivered"})
		hash := sha256.Sum256(raw)
		id := hex.EncodeToString(hash[:])
		now := time.Now().UTC()
		v := trigger.LifecycleSync{ID: id, TriggerID: tr.ID, TaskID: task, Stage: "pr_delivered", Evidence: "evt-confirmed-pr", IssueID: "10007", IssueTypeID: "10002", IssueKey: "APP-7", Source: tr.Source, ConnectionRevision: 1, Mapping: trigger.MappingPin{Name: "reviewing", Revision: 1}, ConsentScope: tr.Consent.Scope, Actor: tr.Consent.Actor, State: state, CreatedAt: now, UpdatedAt: now}
		encoded, _ := json.Marshal(v)
		rev, err := a.Store.PutSetting(t.Context(), trigger.LifecycleSyncPrefix+id, encoded, 0, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		v.Revision = rev
		return v
	}
	syncer := jiradc.TransitionSync{Connections: a.IssueConnections}
	job := makeJob("tsk_1", "unknown")
	intent := jiradc.TransitionIntent{OperationID: "lifecycle:" + job.ID, MappingName: "reviewing", MappingRevision: 1, Connection: "native", ConnectionRevision: 1, IssueKey: "APP-7", IssueID: "10007", IssueTypeID: "10002", FromStatusID: "10", TransitionID: "31", TargetStatusID: "20"}
	if _, err := syncer.Apply(t.Context(), intent, "fixture"); err == nil || posts != 1 {
		t.Fatal(err, posts)
	}
	var page struct {
		Items []trigger.LifecycleSync `json:"items"`
		Next  string                  `json:"next"`
	}
	if code := call(t, srv, "GET", "/api/v1/lifecycle-syncs", viewerTok, "", "", &page); code != 200 || len(page.Items) != 1 {
		t.Fatal(code, page)
	}
	if code := call(t, srv, "GET", "/api/v1/lifecycle-syncs?after=../bad", viewerTok, "", "", nil); code != 422 {
		t.Fatal("invalid cursor", code)
	}
	path := "/api/v1/lifecycle-syncs/" + job.ID
	for _, suffix := range []string{"/retry", "/reconcile"} {
		code, _, _ := rawCall(t, "POST", srv.URL+path+suffix, viewerTok, `{"confirm":true}`, map[string]string{"If-Match": "1"})
		if code != 403 {
			t.Fatal(code)
		}
	}
	for _, body := range []string{`{}`, `{"confirm":false}`, `{"confirm":true,"issueKey":"OTHER-1"}`, `{"confirm":true}{}`} {
		code, _, _ := rawCall(t, "POST", srv.URL+path+"/reconcile", operatorTok, body, map[string]string{"If-Match": "1"})
		if code != 400 {
			t.Fatal("invalid recovery body", body, code)
		}
	}
	code, _, _ := rawCall(t, "POST", srv.URL+path+"/retry", adminTok, `{"confirm":true}`, map[string]string{"If-Match": "1"})
	if code != 409 || posts != 1 {
		t.Fatal("unknown write retry", code, posts)
	}
	code, _, _ = rawCall(t, "POST", srv.URL+path+"/reconcile", operatorTok, `{"confirm":true}`, map[string]string{"If-Match": "2"})
	if code != 409 {
		t.Fatal("stale recovery", code)
	}
	code, _, raw := rawCall(t, "POST", srv.URL+path+"/reconcile", operatorTok, `{"confirm":true}`, map[string]string{"If-Match": "1"})
	if json.Unmarshal([]byte(raw), &job) != nil || code != 200 || job.State != "target_observed" || job.Revision != 2 || posts != 1 || strings.Contains(raw, "lifecycle-fixture-token") {
		t.Fatal("read-only recovery", code, job, posts)
	}
	// A known pre-dispatch failure can only be queued by an administrator.
	status = "10"
	job = makeJob("tsk_2", "failed")
	intent.OperationID = "lifecycle:" + job.ID
	deny = true
	if _, err := syncer.Apply(t.Context(), intent, "fixture"); err == nil || posts != 1 {
		t.Fatal(err, posts)
	}
	path = "/api/v1/lifecycle-syncs/" + job.ID
	code, _, _ = rawCall(t, "POST", srv.URL+path+"/retry", operatorTok, `{"confirm":true}`, map[string]string{"If-Match": "1"})
	if code != 403 {
		t.Fatal(code)
	}
	code, _, raw = rawCall(t, "POST", srv.URL+path+"/retry", adminTok, `{"confirm":true}`, map[string]string{"If-Match": "1"})
	if json.Unmarshal([]byte(raw), &job) != nil || code != 200 || job.State != "queued" || job.RetryRevision < 1 || posts != 1 {
		t.Fatal("preflight recovery did not queue", code, job)
	}
}
