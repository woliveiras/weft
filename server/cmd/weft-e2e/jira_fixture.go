//go:build e2e

package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/trigger"
)

// This verified-TLS fake belongs exclusively to the disposable browser backend.
// It supports read-only discovery; every remote mutation is rejected.
func nativeJiraFixture() *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jira-mapping-fixture-token" {
			w.WriteHeader(401)
			return
		}
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/jira/rest/api/2/issue/APP-8":
			fmt.Fprint(w, `{"id":"10008","key":"APP-8","fields":{"summary":"Retained recovery sample","updated":"2026-11-20T12:00:00.000+0000","issuelinks":[],"project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":"20","name":"Review","statusCategory":{"key":"indeterminate"}}}}`)
		case "/jira/rest/api/2/issue/APP-7":
			fmt.Fprint(w, `{"id":"10007","key":"APP-7","fields":{"summary":"Browser mapping sample","updated":"2026-11-20T12:00:00.000+0000","issuelinks":[],"project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":"10","name":"Ready","statusCategory":{"key":"new"}}}}`)
		case "/jira/rest/api/2/issue/10007/transitions":
			fmt.Fprint(w, `{"transitions":[{"id":"31","name":"Review","to":{"id":"20","name":"Review"},"fields":{}},{"id":"32","name":"Review","to":{"id":"21","name":"Review"},"fields":null}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
}
func installNativeJiraFixture(ctx context.Context, a *app.App) (func(), error) {
	remote := nativeJiraFixture()
	prior := http.DefaultTransport
	transport, ok := prior.(*http.Transport)
	if !ok {
		remote.Close()
		return nil, fmt.Errorf("unexpected fixture HTTP transport")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		remote.Close()
		return nil, err
	}
	roots.AddCert(remote.Certificate())
	trusted := transport.Clone()
	trusted.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	http.DefaultTransport = trusted
	cleanup := func() { http.DefaultTransport = prior; trusted.CloseIdleConnections(); remote.Close() }
	token := "jira-mapping-fixture-token"
	_, err = a.IssueConnections.Put(ctx, jiradc.ConnectionInput{Name: "jira-mapping-fixture", Kind: "jira-dc", SiteURL: remote.URL + "/jira", ProjectID: "10001", Enabled: true, ProjectRestricted: true, Token: &token}, 0, "e2e-fixture")
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := seedLifecycleRecovery(ctx, a, remote.URL+"/jira"); err != nil {
		cleanup()
		return nil, err
	}
	return cleanup, nil
}

// Retained uncertainty models a prior interrupted dispatch. The fake can only
// answer GET, so browser reconciliation cannot accidentally repeat a transition.
func seedLifecycleRecovery(ctx context.Context, a *app.App, site string) error {
	raw, _ := json.Marshal([]string{"trg_fixture", "tsk_fixture", "pr_delivered"})
	sum := sha256.Sum256(raw)
	id := hex.EncodeToString(sum[:])
	now := time.Now().UTC()
	v := trigger.LifecycleSync{ID: id, TriggerID: "trg_fixture", TaskID: "tsk_fixture", Stage: "pr_delivered", Evidence: "evt-fixture-pr", IssueID: "10008", IssueTypeID: "10002", IssueKey: "APP-8", Source: trigger.SourceConfig{Kind: "jira-dc", ConnectionID: "jira-mapping-fixture", SiteURL: site, ProjectID: "10001"}, ConnectionRevision: 1, Mapping: trigger.MappingPin{Name: "retained-review", Revision: 1}, ConsentScope: "fixture-reviewed-scope", Actor: "e2e-fixture", State: "unknown", Error: "The prior dispatch response was lost. Reconcile by reading.", CreatedAt: now, UpdatedAt: now}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err = a.Store.PutSetting(ctx, trigger.LifecycleSyncPrefix+id, data, 0, "e2e-fixture"); err != nil {
		return err
	}
	core := jiradc.SyncRecord{Intent: jiradc.TransitionIntent{OperationID: "lifecycle:" + id, MappingName: v.Mapping.Name, MappingRevision: 1, Connection: v.Source.ConnectionID, ConnectionRevision: 1, IssueKey: v.IssueKey, IssueID: v.IssueID, IssueTypeID: v.IssueTypeID, FromStatusID: "10", TransitionID: "31", TargetStatusID: "20"}, SiteURL: site, ProjectID: "10001", State: "unknown", Error: "Dispatch response unavailable", CreatedAt: now, UpdatedAt: now}
	data, err = json.Marshal(core)
	if err != nil {
		return err
	}
	sum = sha256.Sum256([]byte(core.Intent.OperationID))
	_, err = a.Store.PutSetting(ctx, jiradc.SyncPrefix+hex.EncodeToString(sum[:]), data, 0, "e2e-fixture")
	return err
}
