package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/reviewroute"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
	"gopkg.in/yaml.v3"
)

type portableReviewModel struct {
	args  json.RawMessage
	round atomic.Int32
}

func (*portableReviewModel) Name() string                                    { return "portable-review-fixture" }
func (*portableReviewModel) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (m *portableReviewModel) Chat(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	var args map[string]any
	if err := json.Unmarshal(m.args, &args); err != nil {
		return nil, err
	}
	name := "scm.pull_request.read"
	switch m.round.Add(1) {
	case 1:
		args["diff"], args["files"], args["checks"] = true, true, true
	case 2:
		name = "scm.pull_request.review"
		args["headSha"], args["baseSha"] = strings.Repeat("a", 40), strings.Repeat("b", 40)
		args["verdict"] = "changes_requested"
		args["summary"] = "No confirmed defect in this fixture. Review incomplete: no CI evidence or required merge rules. Security, scalability, performance, accessibility and architecture inspected within the supplied diff. Operator verification required."
	default:
		confirmed := false
		for _, message := range r.Messages {
			if message.Role == llm.RoleTool && message.Name == "scm.pull_request.review" && strings.Contains(message.Content, `"status":"published"`) {
				confirmed = true
			}
		}
		if !confirmed {
			return nil, fmt.Errorf("fixture refuses to claim an unconfirmed review publication")
		}
		name = "finish"
		args = map[string]any{"summary": "Portable review summary publication confirmed; operator verification required."}
	}
	raw, _ := json.Marshal(args)
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: fmt.Sprint(m.round.Load()), Name: name, Arguments: raw}}}}, nil
}
func TestPortableReviewExamplePublishesOneScopedSummary(t *testing.T) {
	t.Run("confirmed", func(t *testing.T) { portableReviewExample(t, true) })
	t.Run("missing publication evidence", func(t *testing.T) { portableReviewExample(t, false) })
}
func portableReviewExample(t *testing.T, confirm bool) {
	sha, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
	var comments atomic.Int32
	var commentMu sync.Mutex
	commentBody := ""
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/repos/acme/app/pulls/7":
			fmt.Fprintf(w, `{"number":7,"html_url":"https://scm.example/pr/7","state":"open","head":{"ref":"weft/fix","sha":%q,"repo":{"full_name":"acme/app"}},"base":{"ref":"main","sha":%q,"repo":{"full_name":"acme/app"}}}`, sha, base)
		case strings.Contains(p, "/compare/"):
			if r.Header.Get("Accept") == "application/vnd.github.diff" {
				fmt.Fprint(w, "diff --git a/a.go b/a.go\n+// reviewed fixture\n")
			} else {
				fmt.Fprintf(w, `{"base_commit":{"sha":%q},"files":[{"filename":"a.go","status":"modified"}]}`, base)
			}
		case strings.HasSuffix(p, "/check-runs"):
			fmt.Fprint(w, `{"total_count":0,"check_runs":[]}`)
		case strings.HasSuffix(p, "/status"):
			fmt.Fprintf(w, `{"sha":%q,"total_count":0,"statuses":[]}`, sha)
		case strings.HasSuffix(p, "/comments") && r.Method == "GET":
			commentMu.Lock()
			defer commentMu.Unlock()
			if confirm && commentBody != "" {
				_ = json.NewEncoder(w).Encode([]map[string]string{{"body": commentBody}})
			} else {
				fmt.Fprint(w, `[]`)
			}
		case strings.HasSuffix(p, "/comments") && r.Method == "POST":
			var input struct {
				Body string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Errorf("comment body: %v", err)
			}
			commentMu.Lock()
			commentBody = input.Body
			commentMu.Unlock()
			comments.Add(1)
			w.WriteHeader(201)
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected provider operation %s %s", r.Method, p)
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	previous := http.DefaultTransport
	http.DefaultTransport = remote.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	srv, a := setupApp(t, nil)
	synthetic := "portable-fixture-token"
	c, err := a.SCM.Save(t.Context(), scm.Input{Name: "team-scm", Kind: "github", Repository: "acme/app", APIURL: remote.URL, Token: &synthetic}, "admin", false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../examples/portable-code-review/agent.json")
	if err != nil {
		t.Fatal(err)
	}
	var agent map[string]any
	if json.Unmarshal(raw, &agent) != nil {
		t.Fatal("invalid example")
	}
	definition := agent["definition"].(map[string]any)
	definition["model"] = map[string]any{"provider": "portable-review-fixture"}
	raw, _ = json.Marshal(agent)
	args, _ := json.Marshal(map[string]any{"connection": c.Name, "connectionRevision": c.UpdatedAt.UTC().Format(time.RFC3339Nano), "number": 7})
	model := &portableReviewModel{args: args}
	a.Models.Add(llm.ProviderConfig{Name: "portable-review-fixture", Type: "mock", DefaultModel: "fixture"}, model)
	if code := call(t, srv, "POST", "/api/v1/agents", adminTok, "application/json", string(raw), nil); code != 201 {
		t.Fatal("example agent", code)
	}
	body, err := os.ReadFile("../../../examples/portable-code-review/workflow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var g domain.Graph
	if yaml.Unmarshal(body, &g) != nil {
		t.Fatal("invalid workflow example")
	}
	raw, _ = json.Marshal(g)
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", string(raw), nil); code != 201 {
		t.Fatal("example workflow", code)
	}
	route := reviewroute.Input{Name: "portable", Connection: c.Name, ConnectionRevision: c.UpdatedAt.UTC().Format(time.RFC3339Nano), Provider: c.Kind, Instance: remote.URL, Repository: c.Repository, GraphID: g.Metadata.ID, GraphVersion: 1, ChannelID: "requests", NodeID: "reviewer", Enabled: true}
	body, _ = json.Marshal(map[string]any{"route": route, "confirm": true})
	if code := call(t, srv, "POST", "/api/v1/review-routes", adminTok, "application/json", string(body), nil); code != 201 {
		t.Fatal("example route", code)
	}
	if err = a.Engine.GraphAction(t.Context(), g.Metadata.ID, "start", "admin"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		ss, _ := a.Store.ListReplicaStatus(t.Context(), g.Metadata.ID)
		if len(ss) == 1 && ss[0].State == domain.ReplicaIdle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("example runtime not ready", ss)
		}
		time.Sleep(10 * time.Millisecond)
	}
	eventData := map[string]any{"connection": c.Name, "connectionRevision": route.ConnectionRevision, "provider": c.Kind, "instance": route.Instance, "repository": c.Repository, "number": 7, "url": "https://scm.example/pr/7", "headSha": sha, "baseSha": base, "reviewRevisionAvailable": true}
	for i := 0; i < 2; i++ {
		if err = a.Store.AppendEvent(t.Context(), domain.NewEvent(domain.EvPullRequestOpened, "task-flow", eventData)); err != nil {
			t.Fatal(err)
		}
	}
	if err = a.ReviewRoutes.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		xs, _ := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: g.Metadata.ID, Limit: 100})
		expected := domain.ExecSucceeded
		if !confirm {
			expected = domain.ExecFailed
		}
		if len(xs) == 1 && xs[0].Status == expected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("portable review did not complete", xs, model.round.Load(), comments.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if comments.Load() != 1 {
		t.Fatal("review summary count", comments.Load())
	}
	events, eventErr := a.Store.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{domain.EvReviewSummaryPublished}})
	if eventErr != nil || (confirm && len(events) != 1) || (!confirm && len(events) != 0) {
		t.Fatal("confirmed summary event", events, eventErr)
	}
	stored, storeErr := a.Store.ListSettings(t.Context(), "scm/reviews/")
	if storeErr != nil || len(stored) != 1 {
		t.Fatal("summary receipt", stored, storeErr)
	}
	var publication scm.ReviewReceipt
	if err := json.Unmarshal(stored[0].Value, &publication); err != nil {
		t.Fatal(err)
	}
	want := "published"
	if !confirm {
		want = "pending"
	}
	if publication.Status != want {
		t.Fatal("publication status", publication.Status, want)
	}
	rows, _, err := a.ReviewRoutes.ReceiptsPage(t.Context(), "", 100)
	if err != nil || len(rows) != 1 || rows[0].State != "published" {
		t.Fatal("dedup receipt", rows, err)
	}
}
