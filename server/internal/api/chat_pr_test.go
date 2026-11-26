package api_test

import (
	"context"
	"fmt"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/scm"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type manualPRFixture struct {
	chatAgentFixture
	analyses atomic.Int32
}

func (f *manualPRFixture) Chat(_ context.Context, q *llm.ChatRequest) (*llm.ChatResponse, error) {
	result := `{"kind":"pr.analyze","prNumber":"418","title":"Review change"}`
	if !q.JSON {
		if len(q.Tools) != 0 || !strings.Contains(q.Messages[len(q.Messages)-1].Content, "fixture change") {
			return nil, fmt.Errorf("unscoped review")
		}
		f.analyses.Add(1)
		result = "The fixture change needs a boundary test."
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: result}, Usage: llm.Usage{InputTokens: 15, OutputTokens: 8}}, nil
}

func TestChatManualPRReturnsAnExactDraftWithoutPublication(t *testing.T) {
	for _, kind := range []string{"github", "bitbucket-cloud", "bitbucket-dc"} {
		t.Run(kind, func(t *testing.T) { verifyChatPRFixture(t, kind) })
	}
}

func verifyChatPRFixture(t *testing.T, kind string) {
	t.Helper()
	server, a := setupApp(t, nil)
	var changed atomic.Bool
	var reads, writes atomic.Int32
	base := strings.Repeat("b", 40)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes.Add(1)
			w.WriteHeader(405)
			return
		}
		reads.Add(1)
		head := strings.Repeat("a", 40)
		if changed.Load() {
			head = strings.Repeat("c", 40)
		}
		switch kind + " " + r.URL.RequestURI() {
		case "github /repos/team/repo/pulls/418":
			fmt.Fprintf(w, `{"number":418,"html_url":"https://scm.example/pr/418","state":"open","head":{"ref":"feature","sha":%q,"repo":{"full_name":"team/repo"}},"base":{"ref":"main","sha":%q,"repo":{"full_name":"team/repo"}}}`, head, base)
		case "github /repos/team/repo/compare/" + base + "..." + head:
			fmt.Fprint(w, "diff --git a/file b/file\n+fixture change\n")
		case "bitbucket-cloud /2.0/repositories/team/repo/pullrequests/418":
			fmt.Fprintf(w, `{"id":418,"state":"OPEN","source":{"branch":{"name":"feature"},"repository":{"full_name":"team/repo"},"commit":{"hash":%q}},"destination":{"branch":{"name":"main"},"repository":{"full_name":"team/repo"},"commit":{"hash":%q}},"links":{"html":{"href":"https://scm.example/pr/418"}}}`, head, base)
		case "bitbucket-cloud /2.0/repositories/team/repo/diff/" + head + ".." + base + "?topic=true":
			fmt.Fprint(w, "diff --git a/file b/file\n+fixture change\n")
		case "bitbucket-dc /rest/api/1.0/projects/TEAM/repos/repo/pull-requests/418":
			fmt.Fprintf(w, `{"id":418,"state":"OPEN","fromRef":{"id":"refs/heads/feature","latestCommit":%q,"repository":{"project":{"key":"TEAM"},"slug":"repo"}},"toRef":{"id":"refs/heads/main","latestCommit":%q,"repository":{"project":{"key":"TEAM"},"slug":"repo"}},"links":{"self":[{"href":"https://scm.example/pr/418"}]}}`, head, base)
		case "bitbucket-dc /rest/api/1.0/projects/TEAM/repos/repo/pull-requests/418/diff?sinceId=" + base + "&untilId=" + head + "&withComments=false":
			fmt.Fprintf(w, `{"fromHash":%q,"toHash":%q,"truncated":false,"diffs":[{"hunks":[{"segments":[{"type":"ADDED","lines":[{"line":"fixture change"}]}]}]}]}`, base, head)
		default:
			t.Errorf("unexpected fixture request %s", r.URL.RequestURI())
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	synthetic := "manual-review-scoped-fixture"
	connection := scm.Input{Name: "origin", Kind: kind, Repository: "team/repo", APIURL: remote.URL, Token: &synthetic}
	switch kind {
	case "bitbucket-cloud":
		connection.APIURL += "/2.0"
		connection.AuthType, connection.Username = "api-token", "fixture@example.invalid"
	case "bitbucket-dc":
		connection.Repository = "TEAM/repo"
	}
	if _, err := a.SCM.Save(t.Context(), connection, "fixture", false); err != nil {
		t.Fatal(err)
	}
	model := &manualPRFixture{}
	if err := a.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: model}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	graph := `{"metadata":{"id":"pr-review"},"spec":{"nodes":[{"id":"reader","prompt":"Review the change.","runtime":{"kind":"native"},"model":{"provider":"chat-fixture","name":"fixture"},"loop":{"mode":"event"},"tools":[{"name":"scm.pull_request.read","repositories":["origin"]}]}],"channels":[{"id":"in","to":["reader"]}]}}`
	var created map[string]any
	if code := call(t, server, "POST", "/api/v1/graphs", adminTok, "application/json", graph, &created); code != 201 {
		t.Fatalf("graph: %d %+v", code, created)
	}
	if code := call(t, server, "POST", "/api/v1/graphs/pr-review/start", operatorTok, "", "", nil); code != 200 {
		t.Fatalf("start: %d", code)
	}
	waitFor(t, 5*time.Second, "fixture workflow running with an assigned run ID", func() bool {
		record, err := a.Store.GetGraphRecord(t.Context(), "pr-review")
		return err == nil && record.DesiredState == domain.GraphRunning && record.State == domain.GraphRunning && record.RunID != ""
	})
	request := func(id, token string) chat.Session {
		t.Helper()
		var s chat.Session
		if code := call(t, server, "POST", "/api/v1/chat/sessions", token, "application/json", fmt.Sprintf(`{"requestId":%q,"text":"Revise o PR 418."}`, id), &s); code != 200 || len(s.Messages) != 2 {
			t.Fatalf("request %d %+v", code, s.Messages)
		}
		return s
	}
	confirm := func(s chat.Session) *chat.OperationReview {
		t.Helper()
		r := s.Messages[1].Review
		var result chat.Session
		if code := call(t, server, "POST", "/api/v1/chat/sessions/"+s.ID+"/operations/"+r.ID+"/confirm", operatorTok, "application/json", fmt.Sprintf(`{"digest":%q}`, r.Digest), &result); code != 200 {
			t.Fatalf("confirm %d", code)
		}
		return result.Messages[1].Review
	}
	const first = "6334bcfa09134567aef46254553a6104"
	if request(first, viewerTok).Messages[1].Review != nil || reads.Load() != 0 {
		t.Fatal("unauthorized lookup")
	}
	stale := request(first, operatorTok)
	if stale.Messages[1].Review == nil || stale.Messages[1].Review.PR == nil {
		t.Fatalf("missing proposal: %+v", stale.Messages)
	}
	p := stale.Messages[1].Review.PR
	if p.Provider != kind || p.Repository != connection.Repository || p.Head != strings.Repeat("a", 40) || p.Base != base {
		t.Fatalf("incorrect review identity: %+v", p)
	}
	changed.Store(true)
	if result := confirm(stale); result.State != chat.ReviewConflict || model.analyses.Load() != 0 {
		t.Fatalf("stale: %+v", result)
	}
	s := request("8334bcfa09134567aef46254553a6104", operatorTok)
	if result := confirm(s); result.State != chat.ReviewQueued {
		t.Fatalf("dispatch: %+v", result)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var got chat.Session
		if code := call(t, server, "GET", "/api/v1/chat/sessions/"+s.ID, operatorTok, "", "", &got); code != 200 {
			t.Fatal(code)
		}
		r := got.Messages[1].Review
		if r.State == chat.ReviewApplied {
			if r.ResultID == "" || r.PRSummary != "The fixture change needs a boundary test." || !got.Messages[1].Unread {
				t.Fatalf("evidence: %+v", r)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no terminal receipt: %+v", r)
		}
		time.Sleep(50 * time.Millisecond)
	}
	confirm(s)
	if model.analyses.Load() != 1 || writes.Load() != 0 {
		t.Fatalf("analyses %d writes %d", model.analyses.Load(), writes.Load())
	}
}
