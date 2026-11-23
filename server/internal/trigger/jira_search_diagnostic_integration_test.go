package trigger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestJiraSearchDiagnosticSurvivesTLSGatewayAndPersistenceWithoutPayload(t *testing.T) {
	const token = "fixture-search-credential"
	const private = "private-issue-content-and-arbitrary-key"
	var mode, searches atomic.Int32
	complete := jiraIssue("APP-7", "Example", "weft")
	complete["id"], complete["self"] = "10007", "https://team.atlassian.net/rest/api/3/issue/10007"
	fields := complete["fields"].(map[string]any)
	fields["project"], fields["issuetype"] = map[string]string{"id": "10001"}, map[string]string{"id": "10002"}
	fields["status"].(map[string]any)["id"] = "10"
	fields["updated"] = "2026-11-23T12:00:00Z"
	fields["issuelinks"] = []any{}
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "search-diagnostic")
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "searchJiraIssuesUsingJql", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			requestedFields, ok := request.Params.Arguments["fields"].([]any)
			if request.Params.Name != "searchJiraIssuesUsingJql" || request.Params.Arguments["cloudId"] != cloud || request.Params.Arguments["view"] != "full" || !ok || len(requestedFields) != 10 {
				t.Error("search operation/scope/full view or explicit fields changed")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			searches.Add(1)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": private}}, private: private}
			if mode.Load() == 1 {
				result = map[string]any{"content": []any{}, "structuredContent": map[string]any{"issues": []any{}}}
			}
			if mode.Load() >= 2 {
				data := any(map[string]any{"issues": []any{complete}})
				switch mode.Load() {
				case 2:
					data = map[string]any{"issues": []any{}}
				case 5:
					data = nil
				case 6:
					data = map[string]any{"issues": "private-wrong-type"}
				case 9:
					data = map[string]any{"issues": []any{map[string]any{"id": "10007", "key": "APP-7", "fields": map[string]string{"summary": private}}}}
				case 10:
					if request.Params.Arguments["nextPageToken"] == nil {
						data = map[string]any{"issues": []any{complete}, "nextPageToken": "fixture-next"}
					} else if request.Params.Arguments["nextPageToken"] == "fixture-next" {
						second := map[string]any{}
						for k, v := range complete {
							second[k] = v
						}
						second["id"], second["key"] = "10008", "APP-8"
						data = map[string]any{"issues": []any{second}}
					} else {
						t.Error("unknown pagination cursor")
						w.WriteHeader(400)
						return
					}
				case 11:
					data = map[string]any{"issues": []any{}, "nextPageToken": "fixture-next"}
				}
				wrapped := map[string]any{"data": data}
				if mode.Load() == 7 {
					wrapped["isError"] = true
				}
				if mode.Load() == 8 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if mode.Load() == 3 || mode.Load() == 7 {
					result = map[string]any{"content": []any{}, "structuredContent": wrapped}
				} else {
					raw, err := json.Marshal(wrapped)
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					result = map[string]any{"content": []any{map[string]string{"type": "text", "text": string(raw)}}}
				}
			}
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			t.Error(err)
		}
	}))
	defer remote.Close()
	st := triggerStore(t)
	box, err := secrets.NewBox(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	manager := mcpconfig.New(st, box, nil)
	if _, err := manager.Put(t.Context(), mcpconfig.Input{ID: "atl", Name: "Jira diagnostic fixture", Service: "generic", Endpoint: remote.URL, Enabled: true, Credential: &mcpconfig.Credential{Value: token}}, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	manager.Discover = func(ctx context.Context, endpoint, credential string) ([]mcpconfig.Tool, error) {
		return mcpconfig.DiscoverWithClient(ctx, endpoint, credential, remote.Client())
	}
	manager.Call = func(ctx context.Context, endpoint, credential, name string, args json.RawMessage) (json.RawMessage, error) {
		return mcpconfig.CallWithClient(ctx, endpoint, credential, name, args, remote.Client())
	}
	if _, err := manager.Refresh(t.Context(), "atl", "admin"); err != nil {
		t.Fatal(err)
	}
	c := &clock{t: time.Now().UTC()}
	r := runner(st, c, "search-diagnostic")
	r.Gateway = tools.NewGateway(tools.NewRegistry(), nil, r.Triggers.Bus)
	r.Gateway.ResolveTool = manager.Resolve
	fake := &tasks{status: map[string]trigger.TaskStatus{}}
	r.Tasks = fake
	in := input()
	in.Source = trigger.SourceConfig{Kind: "jira", ConnectionID: "atl", CloudID: cloud}
	in.Owner = ""
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	r.Tick(t.Context())
	got, err := (trigger.Manager{Store: st}).Get(t.Context(), tr.ID)
	if err != nil || !strings.Contains(got.LastError, "stage=payload;") || !strings.Contains(got.LastError, "contentCount=1,block0=text") || !strings.Contains(got.LastError, "payload=invalid_json") || len(fake.started) != 0 || searches.Load() != 1 {
		t.Fatalf("diagnostic lost or malformed scan dispatched: %v %+v", err, got)
	}
	events, err := st.ListEvents(t.Context(), storage.EventFilter{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal([]any{got, events})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), private) || strings.Contains(string(raw), token) {
		t.Fatal("provider content or credential escaped diagnostic")
	}
	mode.Store(1)
	got = advance(r, c, t, tr.ID)
	if got.LastError != "" || len(fake.started) != 0 || searches.Load() != 2 {
		t.Fatalf("valid empty search rejected after recovery: %+v", got)
	}
	source, err := (trigger.SourceConfig{Kind: "jira", ConnectionID: "atl", CloudID: cloud, SiteURL: "https://api.atlassian.com/ex/jira/" + cloud, ProjectID: "10001", ProfileID: "fixture-profile", ProfileRevision: 1}).Open()
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(ctx context.Context, name string, args any) (*tools.Result, error) {
		remoteTool, err := manager.Resolve(ctx, name)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		return remoteTool.Invoke(ctx, &tools.Invocation{Principal: &tools.Principal{GraphID: "fixture", NodeID: "search", Grants: []domain.ToolGrant{{Name: name}}}, Args: raw})
	}
	for _, variant := range []struct {
		name  string
		mode  int32
		count int
		fail  bool
	}{
		{"wrapped empty", 2, 0, false}, {"structured wrapped complete", 3, 1, false}, {"text wrapped complete", 4, 1, false},
		{"null data", 5, 0, true}, {"nonarray issues", 6, 0, true}, {"explicit error", 7, 0, true}, {"transport failure", 8, 0, true}, {"incomplete scope fields", 9, 0, true},
		{"wrapped pagination", 10, 2, false},
	} {
		t.Run(variant.name, func(t *testing.T) {
			mode.Store(variant.mode)
			rows, err := source.Search(t.Context(), invoke, "weft")
			if variant.fail {
				if err == nil || rows != nil {
					t.Fatalf("invalid wrapped search admitted: %+v %v", rows, err)
				}
				if strings.Contains(err.Error(), private) || strings.Contains(err.Error(), token) {
					t.Fatal("failure disclosed provider data")
				}
				return
			}
			if err != nil || len(rows) != variant.count {
				t.Fatalf("valid wrapper rejected: %+v %v", rows, err)
			}
			if len(rows) == 1 && (rows[0].ID != "10007" || rows[0].IssueTypeID != "10002" || rows[0].StatusID != "10" || rows[0].Updated == "" || rows[0].Title != "Example") {
				t.Fatal("metadata lost across wrapped transport")
			}
		})
	}
	mode.Store(11)
	if matched, err := source.MatchesHeld(t.Context(), invoke, trigger.Issue{Key: "APP-7"}, "weft"); err == nil || matched {
		t.Fatal("paginated empty held-key result treated as final absence", err)
	}
}
