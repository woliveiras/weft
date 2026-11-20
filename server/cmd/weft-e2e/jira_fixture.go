//go:build e2e

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/jiradc"
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
	return cleanup, nil
}
