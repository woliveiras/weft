package mcpconfig

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

type trackerFixtureTransport func(*http.Request) (*http.Response, error)

func (f trackerFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTrackerOriginRequiresFirstPartyScopedStaticCredential(t *testing.T) {
	view := View{Enabled: true, CredentialConfigured: true, Service: "github", Endpoint: "https://api.githubcopilot.com/mcp/"}
	if got := trackerOrigin(view, "github"); got != "https://api.github.com" {
		t.Fatal(got)
	}
	for _, mutate := range []func(*View){
		func(v *View) { v.Enabled = false },
		func(v *View) { v.OAuth = &OAuthView{} },
		func(v *View) { v.ArgumentPolicy = []ArgumentRule{{}} },
		func(v *View) { v.Endpoint = "https://api.githubcopilot.com.example.net/mcp/" },
		func(v *View) { v.CredentialType = "basic" },
	} {
		test := view
		mutate(&test)
		if trackerOrigin(test, "github") != "" {
			t.Fatal("out-of-scope credential accepted")
		}
	}
	jira := View{Enabled: true, CredentialConfigured: true, Service: "jira", Endpoint: atlassianFlatEndpoint, CredentialType: "basic"}
	if trackerOrigin(jira, "jira") != "https://api.atlassian.com" {
		t.Fatal("first-party Jira API scope refused")
	}
	jira.AtlassianPolicy = &AtlassianPolicy{}
	if trackerOrigin(jira, "jira") != "" {
		t.Fatal("API operations bypassed a restricted MCP connection")
	}
}

func TestTrackerAPIBindsRevisionAndRejectsRedirectAndCredentialEcho(t *testing.T) {
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	box, err := secrets.NewBox(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	manager := New(st, box, nil)
	const fixtureCredential = "unique-tracker-test-credential"
	view, err := manager.Put(t.Context(), Input{ID: "github", Name: "GitHub fixture", Service: "github", Endpoint: "https://api.githubcopilot.com/mcp/",
		Enabled: true, Credential: &Credential{Value: fixtureCredential}}, 0, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	calls, status := 0, 302
	body := "{}"
	api := &TrackerAPI{Connections: manager, Client: &http.Client{Transport: trackerFixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer "+fixtureCredential {
			t.Fatal("tracker operation used a different origin or credential")
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://other.example/"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	rev, err := api.Revision(t.Context(), view.ID, "github")
	if err != nil || rev != view.Revision {
		t.Fatalf("revision %d, %v", rev, err)
	}
	const path = "/repos/acme/app/issues/1"
	if _, _, err := api.Call(t.Context(), view.ID, "github", rev+1, "GET", path, nil); err == nil || calls != 0 {
		t.Fatal("stale consent dispatched to the tracker")
	}
	if _, code, err := api.Call(t.Context(), view.ID, "github", rev, "GET", path, nil); err == nil || code != 302 || calls != 1 {
		t.Fatalf("redirect was followed or accepted: %d %v calls %d", code, err, calls)
	}
	body, status = `{"body":"`+fixtureCredential+`"}`, 200
	if raw, _, err := api.Call(t.Context(), view.ID, "github", rev, "GET", path, nil); err == nil || len(raw) != 0 || strings.Contains(err.Error(), fixtureCredential) {
		t.Fatal("credential echo reached caller")
	}
}
