package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestLifecycleRecoveryAPIIsRevisionScopedAndNeverRetriesUnknownPost(t *testing.T) {
	posts := 0
	status := "10"
	deny := false
	transitionID, targetID := "31", "20"
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
				fmt.Fprintf(w, `{"transitions":[{"id":%q,"name":"Review","to":{"id":%q,"name":"Review"},"fields":{}}]}`, transitionID, targetID)
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
	firstJobID := job.ID
	detailPath := "/api/v1/lifecycle-syncs/" + job.ID
	var detail struct {
		Sync       trigger.LifecycleSync `json:"sync"`
		Transition *jiradc.SyncRecord    `json:"transition"`
	}
	code, _, raw := rawCall(t, "GET", srv.URL+detailPath, viewerTok, "", nil)
	if json.Unmarshal([]byte(raw), &detail) != nil || code != 200 || detail.Sync.ID != job.ID || detail.Sync.State != "unknown" || detail.Transition != nil || strings.Contains(raw, `"transition"`) || strings.Contains(raw, "lifecycle-fixture-token") {
		t.Fatal("missing retained intent must remain absent", code, raw)
	}
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
	if code := call(t, srv, "GET", "/api/v1/lifecycle-syncs?connection=other", viewerTok, "", "", &page); code != 200 || len(page.Items) != 0 {
		t.Fatal("connection filter", code, page)
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
	code, _, _ = rawCall(t, "POST", srv.URL+path+"/retry", adminTok, `{"confirm":true}`, map[string]string{"If-Match": "1"})
	if code != 409 || posts != 1 {
		t.Fatal("unknown write retry", code, posts)
	}
	code, _, _ = rawCall(t, "POST", srv.URL+path+"/reconcile", operatorTok, `{"confirm":true}`, map[string]string{"If-Match": "2"})
	if code != 409 {
		t.Fatal("stale recovery", code)
	}
	code, _, raw = rawCall(t, "POST", srv.URL+path+"/reconcile", operatorTok, `{"confirm":true}`, map[string]string{"If-Match": "1"})
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
	// The detail must retain the exact reviewed intent even after the current
	// named mapping changes. Reading it must not rediscover or dispatch Jira.
	deny = false
	transitionID, targetID = "41", "30"
	newMapping := strings.ReplaceAll(strings.ReplaceAll(mapping, `"transitionId":"31"`, `"transitionId":"41"`), `"targetStatusId":"20"`, `"targetStatusId":"30"`)
	code, _, _ = rawCall(t, "PUT", srv.URL+"/api/v1/issue-workflow-mappings/reviewing", adminTok, newMapping, map[string]string{"If-Match": "1"})
	if code != 200 {
		t.Fatal("update current mapping", code)
	}
	detailPath = "/api/v1/lifecycle-syncs/" + firstJobID
	code, _, raw = rawCall(t, "GET", srv.URL+detailPath, viewerTok, "", nil)
	if json.Unmarshal([]byte(raw), &detail) != nil || code != 200 || detail.Sync.ID != firstJobID || detail.Transition == nil {
		t.Fatal("retained transition detail", code, raw)
	}
	retained := detail.Transition.Intent
	if retained.OperationID != "lifecycle:"+firstJobID || retained.FromStatusID != "10" || retained.TransitionID != "31" || retained.TargetStatusID != "20" || retained.MappingRevision != 1 || detail.Transition.State != "target_observed" || strings.Contains(raw, "lifecycle-fixture-token") || posts != 1 {
		t.Fatal("detail replaced the historical mapping or leaked a credential", retained, posts)
	}
	// Each retained scope identifier is independently checked before exposure.
	sum := sha256.Sum256([]byte("lifecycle:" + firstJobID))
	coreKey := jiradc.SyncPrefix + hex.EncodeToString(sum[:])
	row, err := a.Store.GetSetting(t.Context(), coreKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*jiradc.TransitionIntent)
	}{
		{"connection", func(v *jiradc.TransitionIntent) { v.Connection = "another" }},
		{"connection revision", func(v *jiradc.TransitionIntent) { v.ConnectionRevision++ }},
		{"issue ID", func(v *jiradc.TransitionIntent) { v.IssueID = "10008" }},
		{"issue type ID", func(v *jiradc.TransitionIntent) { v.IssueTypeID = "10003" }},
		{"issue key", func(v *jiradc.TransitionIntent) { v.IssueKey = "APP-8" }},
		{"mapping name", func(v *jiradc.TransitionIntent) { v.MappingName = "another" }},
		{"mapping revision", func(v *jiradc.TransitionIntent) { v.MappingRevision++ }},
	} {
		t.Run("detail rejects mismatched "+tc.name, func(t *testing.T) {
			var core jiradc.SyncRecord
			if err := json.Unmarshal(row.Value, &core); err != nil {
				t.Fatal(err)
			}
			tc.edit(&core.Intent)
			encoded, err := json.Marshal(core)
			if err != nil {
				t.Fatal(err)
			}
			rev, err := a.Store.PutSetting(t.Context(), coreKey, encoded, row.Revision, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			code, _, raw := rawCall(t, "GET", srv.URL+detailPath, viewerTok, "", nil)
			if code != 409 || strings.Contains(raw, `"transition"`) || posts != 1 {
				t.Fatal("scope mismatch exposed retained transition", code, raw)
			}
			row.Revision, err = a.Store.PutSetting(t.Context(), coreKey, row.Value, rev, "fixture")
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, field := range []string{"site", "project"} {
		t.Run("detail rejects mismatched "+field, func(t *testing.T) {
			var core jiradc.SyncRecord
			if err := json.Unmarshal(row.Value, &core); err != nil {
				t.Fatal(err)
			}
			if field == "site" {
				core.SiteURL = "https://another.example"
			} else {
				core.ProjectID = "99999"
			}
			encoded, err := json.Marshal(core)
			if err != nil {
				t.Fatal(err)
			}
			rev, err := a.Store.PutSetting(t.Context(), coreKey, encoded, row.Revision, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			code, _, raw := rawCall(t, "GET", srv.URL+detailPath, viewerTok, "", nil)
			if code != 409 || strings.Contains(raw, `"transition"`) {
				t.Fatal("scope mismatch exposed evidence", code, raw)
			}
			row.Revision, err = a.Store.PutSetting(t.Context(), coreKey, row.Value, rev, "fixture")
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	// A core read failure is not the expected absence of an intent.
	originalStore := a.API.IssueConnections.Store
	a.API.IssueConnections.Store = lifecycleCoreReadFailure{Store: originalStore}
	t.Cleanup(func() { a.API.IssueConnections.Store = originalStore })
	code, _, raw = rawCall(t, "GET", srv.URL+detailPath, viewerTok, "", nil)
	if code != 500 || strings.Contains(raw, `"transition"`) || strings.Contains(raw, "private-store-failure") {
		t.Fatal("core storage failure was hidden or exposed", code, raw)
	}
}

type lifecycleCoreReadFailure struct{ storage.Store }

func (s lifecycleCoreReadFailure) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	if strings.HasPrefix(key, jiradc.SyncPrefix) {
		return nil, errors.New("private-store-failure")
	}
	return s.Store.GetSetting(ctx, key)
}
