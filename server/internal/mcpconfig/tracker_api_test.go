package mcpconfig

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestGitHubRateLimitMetadata(t *testing.T) {
	reset := time.Unix(1794484800, 0).UTC()
	for _, tc := range []struct {
		name      string
		remaining string
		retry     string
		message   string
		limited   bool
		delay     time.Duration
		deadline  time.Time
	}{
		{"primary", "0", "", "API rate limit exceeded for user ID 7.", true, 0, reset},
		{"secondary", "42", "90", "You have exceeded a secondary rate limit.", true, 90 * time.Second, time.Time{}},
		{"secondary without headers", "42", "", "You have exceeded a secondary rate limit.", true, 0, time.Time{}},
		{"retry date", "", reset.Format(http.TimeFormat), "", true, 0, reset},
		{"ordinary refusal", "42", "", "Resource not accessible by personal access token", false, 0, time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := make(http.Header)
			headers.Set("X-RateLimit-Remaining", tc.remaining)
			headers.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
			headers.Set("Retry-After", tc.retry)
			raw, _ := json.Marshal(map[string]string{"message": tc.message})
			err := githubRateLimit(&http.Response{StatusCode: 403, Header: headers, Body: io.NopCloser(bytes.NewReader(raw))})
			limited, delay, deadline := err.TrackerRateLimit()
			if limited != tc.limited || delay != tc.delay || !deadline.Equal(tc.deadline) || err.Error() != "tracker API returned HTTP 403" {
				t.Fatalf("rate limit metadata: %+v", err)
			}
		})
	}
}
