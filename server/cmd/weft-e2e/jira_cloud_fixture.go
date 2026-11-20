//go:build e2e

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
)

const cloudFixtureID = "11111111-2222-3333-4444-555555555555"
const cloudFixtureCredential = "cloud-browser-fixture-token"

type cloudFixtureTransport struct {
	target   string
	verified http.RoundTripper
}

func (v cloudFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Preserve the existing deterministic GitHub readiness fixture. This fallback
	// has no network transport and refuses any unrelated resource.
	if r.URL.Scheme == "https" && r.URL.Host == "api.github.com" {
		return (issueFixtureTransport{}).RoundTrip(r)
	}
	if r.URL.Scheme != "https" || r.URL.Host != "api.atlassian.com" || !strings.HasPrefix(r.URL.Path, "/ex/jira/"+cloudFixtureID+"/rest/api/3/") {
		return nil, errors.New("Cloud browser fixture refuses non-fixture destinations")
	}
	clone := r.Clone(r.Context())
	u := *r.URL
	clone.URL = &u
	clone.URL.Host = strings.TrimPrefix(v.target, "https://")
	return v.verified.RoundTrip(clone)
}
func cloudWorkflowFixture() *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+cloudFixtureCredential {
			w.WriteHeader(401)
			return
		}
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		prefix := "/ex/jira/" + cloudFixtureID + "/rest/api/3"
		switch r.URL.Path {
		case prefix + "/issue/APP-7":
			fmt.Fprint(w, `{"id":"10007","key":"APP-7","fields":{"project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":"10","name":"Ready"},"updated":"2026-11-20T12:00:00.000+0000"}}`)
		case prefix + "/issue/10007/transitions":
			fmt.Fprint(w, `{"transitions":[{"id":"31","name":"Review","to":{"id":"20","name":"Review"},"fields":{}},{"id":"32","name":"Review","to":{"id":"21","name":"Review"},"fields":null}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
}

// This disposable backend uses real verified TLS and the existing REST
// audience/revision checks. It never accepts a vendor mutation or external host.
func installCloudWorkflowFixture(ctx context.Context, a *app.App) (func(), error) {
	remote := cloudWorkflowFixture()
	previous := a.TrackerAPI.Client
	a.TrackerAPI.Client = &http.Client{Transport: cloudFixtureTransport{target: remote.URL, verified: remote.Client().Transport}}
	cleanup := func() { a.TrackerAPI.Client = previous; remote.Close() }
	_, err := a.MCP.Put(ctx, mcpconfig.Input{ID: "cloud-browser-api", Name: "Cloud browser REST fixture", Service: "jira", Endpoint: "https://mcp.atlassian.com/v2/mcp", Enabled: true, Credential: &mcpconfig.Credential{Value: cloudFixtureCredential}}, 0, "e2e-fixture")
	if err != nil {
		cleanup()
		return nil, err
	}
	return cleanup, nil
}
