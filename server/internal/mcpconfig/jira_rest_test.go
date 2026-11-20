package mcpconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
)

const restTestCloud = "11111111-2222-3333-4444-555555555555"
const restTestOtherCloud = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
const restTestEmail = "rest-fixture@example.test"
const restTestToken = "rest-scoped-fixture-token"
const restTestMCPToken = "mcp-scoped-fixture-token"
const restTestPath = "/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP-7?fields=project,status"

// Decode the public input contract so these tests can fail on the old manager
// without introducing an implementation or a compile-only production skeleton.
func restContractInput(t *testing.T, patch map[string]any) Input {
	t.Helper()
	v := map[string]any{"id": "jira-rest", "name": "Jira REST fixture", "service": "atlassian", "endpoint": atlassianFlatEndpoint, "enabled": true,
		"credential": map[string]any{"value": restTestMCPToken}, "jiraRest": map[string]any{"cloudId": restTestCloud},
		"jiraRestCredential": map[string]any{"username": restTestEmail, "value": restTestToken}}
	for k, value := range patch {
		if value == nil {
			delete(v, k)
		} else {
			v[k] = value
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var in Input
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	return in
}

func restPublicView(t *testing.T, v View) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestJiraRESTInputRejectsInvalidIndependentCredential(t *testing.T) {
	for name, patch := range map[string]map[string]any{
		"invalid cloud":         {"jiraRest": map[string]any{"cloudId": "other/cloud"}},
		"empty cloud":           {"jiraRest": map[string]any{"cloudId": ""}},
		"unbound credential":    {"jiraRest": nil},
		"missing email":         {"jiraRestCredential": map[string]any{"value": restTestToken}},
		"oversized email":       {"jiraRestCredential": map[string]any{"username": strings.Repeat("x", 310) + "@example.test", "value": restTestToken}},
		"display name":          {"jiraRestCredential": map[string]any{"username": "User <fixture@example.test>", "value": restTestToken}},
		"header email":          {"jiraRestCredential": map[string]any{"username": "x\r\ny@example.test", "value": restTestToken}},
		"header token":          {"jiraRestCredential": map[string]any{"username": restTestEmail, "value": "bad\nheader"}},
		"nul token":             {"jiraRestCredential": map[string]any{"username": restTestEmail, "value": "bad\x00token"}},
		"empty replacement":     {"jiraRestCredential": map[string]any{"username": restTestEmail, "value": ""}},
		"oversized token":       {"jiraRestCredential": map[string]any{"username": restTestEmail, "value": strings.Repeat("x", 16385)}},
		"mixed clear":           {"jiraRestCredential": map[string]any{"clear": true, "value": restTestToken}},
		"clear email":           {"jiraRestCredential": map[string]any{"clear": true, "username": restTestEmail}},
		"custom endpoint":       {"endpoint": "https://other.example/mcp"},
		"other service":         {"service": "github"},
		"restricted MCP policy": {"argumentPolicy": []any{map[string]any{"tool": "getJiraIssue", "argument": "cloudId", "values": []string{restTestCloud}}}},
		"metadata headers":      {"metadataHeaders": map[string]string{"x-custom": "value"}},
	} {
		t.Run(name, func(t *testing.T) {
			m := testManager(t)
			in := restContractInput(t, patch)
			if err := m.Preview(t.Context(), in); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid explicit REST input accepted: %v", err)
			}
			if _, err := m.Put(t.Context(), in, 0, "admin"); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid REST input stored: %v", err)
			}
		})
	}
}

func TestJiraRESTIndependentSlotPersistenceAndDiscovery(t *testing.T) {
	m := testManager(t)
	in := restContractInput(t, nil)
	v, err := m.Put(t.Context(), in, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	assertSlot := func(v View, configured bool) {
		t.Helper()
		rest, ok := restPublicView(t, v)["jiraRest"].(map[string]any)
		if !ok || rest["cloudId"] != restTestCloud || rest["credentialConfigured"] != configured {
			t.Fatalf("missing independent REST state: %+v", rest)
		}
	}
	assertSlot(v, true)
	setting, err := m.Store.GetSetting(t.Context(), prefix+v.ID)
	if err != nil {
		t.Fatal(err)
	}
	safe, _ := json.Marshal(v)
	for _, secret := range []string{restTestEmail, restTestToken, restTestMCPToken, base64.StdEncoding.EncodeToString([]byte(restTestEmail + ":" + restTestToken))} {
		if strings.Contains(string(setting.Value), secret) || strings.Contains(string(safe), secret) {
			t.Fatal("credential material persisted or returned as plaintext")
		}
	}
	slots, err := m.SecretSlots(t.Context(), v.ID)
	if err != nil || !slots["jiraRest"] || !slots["credential"] {
		t.Fatalf("independent secret slots: %v %v", slots, err)
	}
	if matches, err := m.CredentialMatches(t.Context(), v.ID, "jiraRest", restTestEmail+":"+restTestToken); err != nil || !matches {
		t.Fatalf("REST secret reference comparison: %v %v", matches, err)
	}
	if matches, err := m.CredentialMatches(t.Context(), v.ID, "jiraRest", restTestMCPToken); err != nil || matches {
		t.Fatal("MCP slot confused with REST slot")
	}
	m.Discover = func(_ context.Context, _ string, token string) ([]Tool, error) {
		if token != restTestMCPToken {
			t.Fatal("REST credential dispatched to MCP")
		}
		return []Tool{{Name: "getJiraIssue", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
	}
	v, err = m.Refresh(t.Context(), v.ID, "operator")
	if err != nil || v.Status != "ready" || len(v.Tools) != 1 {
		t.Fatalf("MCP discovery: %v %v", v.Status, err)
	}
	assertSlot(v, true)
	m.Discover = func(context.Context, string, string) ([]Tool, error) {
		return nil, errors.New("fixture discovery unavailable")
	}
	v, err = m.Refresh(t.Context(), v.ID, "operator")
	if err != nil || v.Status != "unavailable" {
		t.Fatalf("transient discovery failure: %v %v", v.Status, err)
	}
	assertSlot(v, true)
	restarted := New(m.Store, m.Box, nil)
	v, err = restarted.Get(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertSlot(v, true)
	changed := restContractInput(t, map[string]any{"credential": nil, "jiraRest": nil, "jiraRestCredential": nil, "name": "Renamed fixture"})
	v, err = restarted.Put(t.Context(), changed, v.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	assertSlot(v, true)
	if _, err = restarted.Put(t.Context(), changed, v.Revision-1, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale REST connection edit accepted: %v", err)
	}
}

func TestJiraRESTScopeRotationAndClearNeverFallback(t *testing.T) {
	m := testManager(t)
	v, err := m.Put(t.Context(), restContractInput(t, nil), 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	changed := restContractInput(t, map[string]any{"credential": nil, "jiraRest": map[string]any{"cloudId": restTestOtherCloud}, "jiraRestCredential": nil})
	if err := m.Preview(t.Context(), changed); !errors.Is(err, ErrInvalid) {
		t.Fatalf("preview retained a foreign-cloud secret: %v", err)
	}
	if _, err := m.Put(t.Context(), changed, v.Revision, "admin"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("save retained a foreign-cloud secret: %v", err)
	}
	clear := restContractInput(t, map[string]any{"credential": nil, "jiraRest": map[string]any{"cloudId": restTestOtherCloud}, "jiraRestCredential": map[string]any{"clear": true}})
	v, err = m.Put(t.Context(), clear, v.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	state, ok := restPublicView(t, v)["jiraRest"].(map[string]any)
	if !ok || state["cloudId"] != restTestOtherCloud || state["credentialConfigured"] != false {
		t.Fatal("clear removed explicit mode instead of only its credential")
	}
	calls := 0
	api := &TrackerAPI{Connections: m, Client: &http.Client{Transport: trackerFixtureTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected network") })}}
	if _, err := api.Revision(t.Context(), v.ID, "jira"); err == nil {
		t.Fatal("cleared explicit mode fell back to MCP compatibility")
	}
	if _, _, err := api.Call(t.Context(), v.ID, "jira", v.Revision, "GET", strings.ReplaceAll(restTestPath, restTestCloud, restTestOtherCloud), nil); err == nil || calls != 0 {
		t.Fatal("cleared REST secret fell back to MCP")
	}
}

type jiraRESTTLSRedirect struct {
	target *url.URL
	base   http.RoundTripper
	calls  atomic.Int32
}

func (v *jiraRESTTLSRedirect) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.atlassian.com" {
		return nil, errors.New("wrong trusted REST origin")
	}
	v.calls.Add(1)
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Host = v.target.Host
	copy.URL = &u
	copy.Host = "api.atlassian.com"
	return v.base.RoundTrip(copy)
}

func restTLSFixture(t *testing.T, handler http.HandlerFunc) (*TrackerAPI, *Manager, View, *jiraRESTTLSRedirect) {
	t.Helper()
	remote := httptest.NewTLSServer(handler)
	t.Cleanup(remote.Close)
	u, err := url.Parse(remote.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := &jiraRESTTLSRedirect{target: u, base: remote.Client().Transport}
	m := testManager(t)
	v, err := m.Put(t.Context(), restContractInput(t, nil), 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return &TrackerAPI{Connections: m, Client: &http.Client{Transport: transport}}, m, v, transport
}

func TestJiraRESTTLSDispatchUsesOnlyIndependentBasicCredential(t *testing.T) {
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(restTestEmail+":"+restTestToken))
	api, m, v, transport := restTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != expected || r.URL.RequestURI() != restTestPath {
			t.Error("REST destination, path or independent Basic credential changed")
		}
		fmt.Fprint(w, `{"id":"10007","key":"APP-7"}`)
	})
	raw, status, err := api.Call(t.Context(), v.ID, "jira", v.Revision, "GET", restTestPath, nil)
	if err != nil || status != 200 || !strings.Contains(string(raw), `"key":"APP-7"`) || transport.calls.Load() != 1 {
		t.Fatalf("independent REST request failed: %d %v", status, err)
	}
	rotated := restContractInput(t, map[string]any{"credential": nil, "jiraRestCredential": map[string]any{"username": restTestEmail, "value": "rotated-fixture"}})
	if _, err := m.Put(t.Context(), rotated, v.Revision, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := api.Call(t.Context(), v.ID, "jira", v.Revision, "GET", restTestPath, nil); err == nil || transport.calls.Load() != 1 {
		t.Fatal("rotation retained stale dispatch authority")
	}
}

func TestJiraRESTPathRejectionPrecedesDecryptionQuotaAndNetwork(t *testing.T) {
	api, m, v, transport := restTLSFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{}`) })
	for _, path := range []string{
		"/ex/jira/" + restTestOtherCloud + "/rest/api/3/issue/APP-7",
		"//api.atlassian.com" + restTestPath,
		"/ex/jira/" + restTestCloud + "/rest/api/3/../myself",
		"/ex/jira/" + restTestCloud + "/rest/api/3/issue/%41PP-7",
		"/ex/jira/" + restTestCloud + "/rest/api/3/issue/%2e%2e/%2fother",
		"/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP-7#ignored",
		"/ex/jira/" + restTestCloud + "/rest/api/3\\issue/APP-7",
	} {
		box := m.Box
		m.Box = nil
		_, _, err := api.Call(t.Context(), v.ID, "jira", v.Revision, "GET", path, nil)
		m.Box = box
		if err == nil || transport.calls.Load() != 0 {
			t.Fatalf("malformed REST path dispatched: %q", path)
		}
	}
	in := restContractInput(t, map[string]any{"credential": nil, "jiraRestCredential": nil, "callsPerMinute": 1})
	v, err := m.Put(t.Context(), in, v.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := api.Call(t.Context(), v.ID, "jira", v.Revision, "GET", "/unrelated", nil); err == nil {
		t.Fatal("invalid path admitted")
	}
	if _, code, err := api.Call(t.Context(), v.ID, "jira", v.Revision, "GET", restTestPath+"&jql=project%20%3D%20APP", nil); err != nil || code != 200 {
		t.Fatalf("invalid path consumed the quota: %d %v", code, err)
	}
}

func TestJiraRESTHTTPFailuresRedirectAndCredentialReflection(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			api, _, v, transport := restTLSFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", "https://other.example/")
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(status)
				fmt.Fprint(w, restTestToken)
			})
			raw, got, err := api.Call(t.Context(), v.ID, "jira", v.Revision, "GET", restTestPath, nil)
			if err == nil || got != status || len(raw) != 0 || transport.calls.Load() != 1 || strings.Contains(err.Error(), restTestToken) {
				t.Fatalf("HTTP failure leaked or retried: %d %v", got, err)
			}
		})
	}
	basic := base64.StdEncoding.EncodeToString([]byte(restTestEmail + ":" + restTestToken))
	for _, secret := range []string{restTestEmail, restTestToken, basic, "Basic " + basic, url.PathEscape(basic), "%72est-scoped-fixture-token", "rest-fixture%40example.test"} {
		t.Run("reflection-"+fmt.Sprint(len(secret)), func(t *testing.T) {
			api, _, v, _ := restTLSFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				body, _ := json.Marshal(map[string]string{"value": secret})
				w.Write(body)
			})
			raw, _, err := api.Call(t.Context(), v.ID, "jira", v.Revision, "GET", restTestPath, nil)
			if err == nil || len(raw) != 0 || strings.Contains(err.Error(), secret) {
				t.Fatal("REST credential material reached caller")
			}
		})
	}
	api, _, v, _ := restTLSFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		escaped := strings.ReplaceAll(restTestToken, "r", `\u0072`)
		io.WriteString(w, `{"value":"`+escaped+`"}`)
	})
	if raw, _, err := api.Call(t.Context(), v.ID, "jira", v.Revision, "GET", restTestPath, nil); err == nil || len(raw) != 0 {
		t.Fatal("JSON-escaped REST token reached caller")
	}
}

func TestJiraRESTOAuthMCPDoesNotBecomeRESTCredential(t *testing.T) {
	m := testManager(t)
	in := restContractInput(t, map[string]any{"credential": nil, "oauth": map[string]any{"issuer": "https://auth.atlassian.com", "clientId": "fixture-client", "scopes": []string{"read:jira-work"}}})
	v, err := m.Put(t.Context(), in, 0, "admin")
	if err != nil {
		t.Fatalf("independent REST cannot coexist with OAuth MCP: %v", err)
	}
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(restTestEmail+":"+restTestToken))
		if r.Header.Get("Authorization") != expected {
			t.Error("OAuth MCP did not preserve independent REST Basic")
		}
		fmt.Fprint(w, `{}`)
	}))
	t.Cleanup(remote.Close)
	u, err := url.Parse(remote.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := &jiraRESTTLSRedirect{target: u, base: remote.Client().Transport}
	api := &TrackerAPI{Connections: m, Client: &http.Client{Transport: transport}}
	if rev, err := api.Revision(t.Context(), v.ID, "jira"); err != nil || rev != v.Revision {
		t.Fatalf("explicit REST incorrectly requires MCP OAuth token: %d %v", rev, err)
	}
	if _, code, err := api.Call(t.Context(), v.ID, "jira", v.Revision, "GET", restTestPath, nil); err != nil || code != 200 || transport.calls.Load() != 1 {
		t.Fatalf("OAuth MCP blocked independent REST: %d %v", code, err)
	}
}

// Corrupt copies use only synthetic encrypted fixtures; a slot must be bound to
// its purpose, connection and cloud even if all public metadata is consistent.
func TestJiraRESTEncryptedSlotRejectsCloudConnectionAndPurposeSwaps(t *testing.T) {
	for _, swap := range []string{"cloud", "connection", "purpose", "corrupt"} {
		t.Run(swap, func(t *testing.T) {
			m := testManager(t)
			v, err := m.Put(t.Context(), restContractInput(t, nil), 0, "admin")
			if err != nil {
				t.Fatal(err)
			}
			setting, err := m.Store.GetSetting(t.Context(), prefix+v.ID)
			if err != nil {
				t.Fatal(err)
			}
			var stored map[string]any
			if err := json.Unmarshal(setting.Value, &stored); err != nil {
				t.Fatal(err)
			}
			path, id, expected := restTestPath, v.ID, setting.Revision
			switch swap {
			case "cloud":
				stored["jiraRest"] = map[string]any{"cloudId": restTestOtherCloud, "credentialConfigured": true}
				path = strings.ReplaceAll(path, restTestCloud, restTestOtherCloud)
			case "connection":
				id, expected = "another-jira-fixture", 0
				stored["id"] = id
			case "purpose":
				stored["jiraRestSealed"] = stored["sealed"]
			case "corrupt":
				stored["jiraRestSealed"] = "invalid-ciphertext"
			}
			raw, err := json.Marshal(stored)
			if err != nil {
				t.Fatal(err)
			}
			revision, err := m.Store.PutSetting(t.Context(), prefix+id, raw, expected, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			api := &TrackerAPI{Connections: m, Client: &http.Client{Transport: trackerFixtureTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected transport") })}}
			if current, err := api.Revision(t.Context(), id, "jira"); err != nil || current != revision {
				t.Fatalf("AAD fixture rejected by metadata instead of decryption: %d %v", current, err)
			}
			result, _, err := api.Call(t.Context(), id, "jira", revision, "GET", path, nil)
			if err == nil || len(result) != 0 || calls != 0 {
				t.Fatalf("invalid REST ciphertext dispatched or fell back: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestJiraRESTCredentialLimitBoundary(t *testing.T) {
	m := testManager(t)
	if _, err := m.Put(t.Context(), restContractInput(t, map[string]any{"jiraRestCredential": map[string]any{"username": restTestEmail, "value": strings.Repeat("x", 16384)}}), 0, "admin"); err != nil {
		t.Fatalf("maximum valid REST credential rejected: %v", err)
	}
}

func TestJiraRESTCanonicalPathScope(t *testing.T) {
	for name, value := range map[string]string{
		"typed issue":   restTestPath,
		"encoded query": restTestPath + "&jql=project%20%3D%20APP",
	} {
		t.Run(name, func(t *testing.T) {
			if !jiraRESTPath(restTestCloud, value) {
				t.Fatal("valid canonical REST request rejected")
			}
		})
	}
	for _, value := range []string{
		"https://api.atlassian.com" + restTestPath,
		"/ex/jira/" + restTestCloud + "/rest/api/30/issue/APP-7",
		"/ex/jira/" + restTestCloud + "/rest/api/3/issue/../APP-7",
		"/ex/jira/" + restTestCloud + "/rest/api/3//issue/APP-7",
		"/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP%2D7",
	} {
		if jiraRESTPath(restTestCloud, value) {
			t.Fatalf("invalid canonical scope accepted: %q", value)
		}
	}
}

func TestJiraRESTCredentialReflectionForms(t *testing.T) {
	basic := base64.StdEncoding.EncodeToString([]byte(restTestEmail + ":" + restTestToken))
	for _, raw := range []string{
		`{"result":{"%72est-scoped-fixture-token":true}}`,
		`{"value":"\u002572est-scoped-fixture-token"}`,
		`{"invalid":"%XX","value":"rest-fixture%40example.test"}`,
	} {
		if !containsJiraRESTCredential([]byte(raw), basic) {
			t.Fatal("encoded credential reflection accepted")
		}
	}
	if containsJiraRESTCredential([]byte(`{"key":"APP-7","fields":{"summary":"Safe response"}}`), basic) {
		t.Fatal("ordinary Jira response rejected")
	}
}
