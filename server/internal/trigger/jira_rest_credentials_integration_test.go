package trigger_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/trigger"
)

// The transport keeps the reviewed vendor URL intact up to the actual TLS
// boundary, then reaches an isolated local server with its fixture CA trusted.
type cloudCredentialFixtureTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (f cloudCredentialFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.atlassian.com" {
		return nil, errors.New("unexpected Jira REST origin")
	}
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Host = f.target.Host
	copy.URL, copy.Host = &u, "api.atlassian.com"
	return f.base.RoundTrip(copy)
}

func TestJiraCloudTriggerSeparatesMCPAndRESTCredentialsOverTLS(t *testing.T) {
	const mcpToken = "trigger-mcp-scoped-fixture-token"
	const restEmail = "trigger-rest@example.test"
	const restToken = "trigger-jira-scoped-fixture-token"
	const endpoint = "https://mcp.atlassian.com/v2/mcp?tools=all"
	st := triggerStore(t)
	atl := newTracker("atl")
	atl.issues = []map[string]any{jiraIssue("APP-1", "Export", "weft", "bug")}
	var searches, restReads, restWrites atomic.Int32
	mcp := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+mcpToken || r.URL.RawQuery != "tools=all" {
			t.Error("MCP discovery/search received another audience's credential or URL")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var q struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&q) != nil {
			w.WriteHeader(400)
			return
		}
		var result any
		switch q.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "trigger-search")
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "searchJiraIssuesUsingJql", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			if q.Params.Name != "searchJiraIssuesUsingJql" || q.Params.Arguments["cloudId"] != cloud {
				t.Error("trigger search lost its explicit MCP operation/cloud scope")
				w.WriteHeader(400)
				return
			}
			searches.Add(1)
			atl.mu.Lock()
			data, err := json.Marshal(map[string]any{"issues": atl.issues})
			atl.mu.Unlock()
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": string(data)}}, "structuredContent": json.RawMessage(data)}
		default:
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(mcp.Close)
	rest := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte(restEmail+":"+restToken)) || !strings.HasPrefix(r.URL.Path, "/ex/jira/"+cloud+"/rest/api/3/") {
			t.Error("Jira read/write received another audience's credential or Cloud ID")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet {
			restReads.Add(1)
		} else {
			restWrites.Add(1)
		}
		var body any
		if r.Method == http.MethodPut {
			var request struct {
				Update map[string][]map[string]string `json:"update"`
				Fields map[string]any                 `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			body = map[string]any{"update": map[string]any{"labels": request.Update["labels"]}, "fields": request.Fields}
		} else if r.Method == http.MethodPost {
			var request struct {
				Body struct {
					Content []struct {
						Content []map[string]string `json:"content"`
					} `json:"content"`
				} `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			paragraphs := []any{}
			for _, p := range request.Body.Content {
				if len(p.Content) == 0 {
					continue
				}
				words := []any{}
				for _, word := range p.Content {
					words = append(words, word)
				}
				paragraphs = append(paragraphs, map[string]any{"content": words})
			}
			body = map[string]any{"body": map[string]any{"content": paragraphs}}
		}
		raw, code, err := atl.request(r.Context(), "atl", "jira", 0, r.Method, r.URL.RequestURI(), body)
		if err != nil {
			t.Errorf("Jira REST fixture: %v", err)
		}
		// Jira may return the authentication account as a normal reporter or
		// comment author. This is identity metadata, never reusable credentials.
		var response map[string]any
		if code >= 200 && code < 300 && len(raw) > 0 && json.Unmarshal(raw, &response) == nil {
			if r.Method == http.MethodGet {
				if fields, ok := response["fields"].(map[string]any); ok {
					fields["reporter"] = map[string]any{"accountId": "human-reporter", "accountType": "atlassian", "emailAddress": restEmail}
				}
				if strings.HasSuffix(r.URL.Path, "/myself") {
					response["emailAddress"] = restEmail
				}
			}
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comment") {
				response["author"] = map[string]any{"accountId": "jira-service", "emailAddress": restEmail}
			}
			raw, err = json.Marshal(response)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if len(raw) > 0 {
			if _, err := w.Write(raw); err != nil {
				t.Error(err)
			}
		}
	}))
	t.Cleanup(rest.Close)
	box, err := secrets.NewBox(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	manager := mcpconfig.New(st, box, nil)
	connection := mcpconfig.Input{ID: "atl", Name: "Separate Jira credentials", Service: "atlassian", Endpoint: endpoint, Enabled: true,
		Credential: &mcpconfig.Credential{Value: mcpToken}, JiraREST: &mcpconfig.JiraRESTConfig{CloudID: cloud},
		JiraRESTCredential: &mcpconfig.Credential{Username: restEmail, Value: restToken}}
	if _, err := manager.Put(t.Context(), connection, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	manager.Discover = func(ctx context.Context, canonical, token string) ([]mcpconfig.Tool, error) {
		if canonical != endpoint {
			t.Error("MCP canonical endpoint changed")
		}
		return mcpconfig.DiscoverWithClient(ctx, mcp.URL+"?tools=all", token, mcp.Client())
	}
	manager.Call = func(ctx context.Context, canonical, token, name string, args json.RawMessage) (json.RawMessage, error) {
		if canonical != endpoint {
			t.Error("MCP canonical endpoint changed")
		}
		return mcpconfig.CallWithClient(ctx, mcp.URL+"?tools=all", token, name, args, mcp.Client())
	}
	v, err := manager.Refresh(t.Context(), connection.ID, "admin")
	if err != nil || v.Status != "ready" {
		t.Fatalf("MCP discovery: %s %v", v.Status, err)
	}
	u, err := url.Parse(rest.URL)
	if err != nil {
		t.Fatal(err)
	}
	api := &mcpconfig.TrackerAPI{Connections: manager, Client: &http.Client{Transport: cloudCredentialFixtureTransport{target: u, base: rest.Client().Transport}}}
	c := &clock{t: time.Now().UTC()}
	r := runner(st, c, "separate-jira-credentials")
	r.Triggers.TrackerRevision = api.Revision
	reg := tools.NewRegistry()
	reg.MustRegister(trigger.TrackerTools(api.Call)...)
	r.Gateway = tools.NewGateway(reg, nil, r.Triggers.Bus)
	r.Gateway.ResolveTool = manager.Resolve
	fake := &tasks{status: map[string]trigger.TaskStatus{}}
	r.Tasks, r.BaseURL = fake, "https://weft.example/"
	var assessments atomic.Int32
	r.Assessor = assessFunc(func(ctx context.Context, tr *trigger.Trigger, input string) trigger.ReadinessAssessment {
		assessments.Add(1)
		if strings.Contains(input, restEmail) || strings.Contains(input, restToken) {
			t.Error("REST authentication material reached readiness")
		}
		return ready(ctx, tr, input)
	})
	in := input()
	in.Source = trigger.SourceConfig{Kind: "jira", ConnectionID: connection.ID, CloudID: cloud}
	in.Owner = ""
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	r.Tick(t.Context())
	got, err := r.Triggers.Get(t.Context(), tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.started) != 1 || got.CurrentTask == "" || searches.Load() != 1 || restReads.Load() == 0 || restWrites.Load() == 0 || assessments.Load() == 0 {
		t.Fatalf("trigger did not claim through both real transports: started=%d search=%d reads=%d writes=%d state=%s error=%s", len(fake.started), searches.Load(), restReads.Load(), restWrites.Load(), got.State, got.LastError)
	}
	fake.status[got.CurrentTask] = trigger.TaskStatus{State: "succeeded", PullRequestURL: "https://bitbucket.example/pull/2"}
	c.t = c.t.Add(time.Hour)
	r.Tick(t.Context())
	fields := atl.find("APP-1")["fields"].(map[string]any)
	if !slices.Contains(fields["labels"].([]string), "weft:done") || len(atl.comments["APP-1"]) != 2 || len(fake.started) != 1 {
		t.Fatalf("completion lost REST write-back: labels=%v comments=%v", fields["labels"], atl.comments["APP-1"])
	}
	if got, err := r.Triggers.Get(t.Context(), tr.ID); err != nil || got.CurrentTask != "" {
		t.Fatalf("task lifecycle: %+v %v", got, err)
	}
	events, err := st.ListEvents(t.Context(), storage.EventFilter{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	public, err := json.Marshal([]any{fake.started, got, events})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{restEmail, restToken, mcpToken, base64.StdEncoding.EncodeToString([]byte(restEmail + ":" + restToken))} {
		if strings.Contains(string(public), secret) {
			t.Fatal("credential identity or reusable secret reached task/evidence")
		}
	}
	// REST-only rotation advances the same reviewed revision. No stale trigger
	// may search or mutate even though its MCP credential is unchanged.
	connection.Credential = nil
	connection.JiraRESTCredential = &mcpconfig.Credential{Username: restEmail, Value: "rotated-fixture-rest-token"}
	if _, err := manager.Put(t.Context(), connection, v.Revision, "admin"); err != nil {
		t.Fatal(err)
	}
	beforeSearch, beforeWrites := searches.Load(), restWrites.Load()
	c.t = c.t.Add(time.Hour)
	r.Tick(t.Context())
	if searches.Load() != beforeSearch || restWrites.Load() != beforeWrites || len(fake.started) != 1 {
		t.Fatal("REST rotation retained stale trigger dispatch authority")
	}
	problem, err := r.Triggers.ConsentProblem(t.Context(), tr)
	if err != nil || problem == "" {
		t.Fatalf("REST rotation retained consent: %q %v", problem, err)
	}
}
