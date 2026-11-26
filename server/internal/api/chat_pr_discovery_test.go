package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/scm"
)

type prDiscoveryModel struct{ manualPRFixture }

func (m *prDiscoveryModel) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if !req.JSON {
		return m.manualPRFixture.Chat(ctx, req)
	}
	text := req.Messages[len(req.Messages)-1].Content
	p := map[string]string{"kind": "pr.analyze", "prNumber": "418", "title": "Review a pull request"}
	switch text {
	case "Review PR 418 in team/docs.":
		p["prRepository"] = "team/docs"
	case "Review PR 418 in team/masked.":
		p["prRepository"] = "team/masked"
	case "Review https://scm.example/team/app/pull/418.":
		delete(p, "prNumber")
		p["prUrl"] = "https://scm.example/team/app/pull/418"
	case "Review PR 418 in that repository.":
		found := false
		for _, message := range req.Messages[:len(req.Messages)-1] {
			if strings.Contains(strings.ToLower(message.Content), "selected") && strings.Contains(message.Content, "b-docs") {
				found = true
			}
			if strings.Contains(message.Content, "Update docs retention") || strings.Contains(message.Content, "Mobile checksum") {
				return nil, fmt.Errorf("repository title entered inference context")
			}
		}
		if found {
			p["prConnection"] = "b-docs"
		}
	case "Revise o PR 418.":
		for _, message := range req.Messages[:len(req.Messages)-1] {
			if strings.Contains(message.Content, "explicitly selected scm b-docs") {
				// Model output alone cannot authorize reuse of an older source.
				p["prConnection"] = "b-docs"
			}
		}
	}
	body, _ := json.Marshal(p)
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: string(body)}}, nil
}

func TestChatPRDiscoverySeparatesRepositoryMatchesAndReviewers(t *testing.T) {
	srv, app := setupApp(t, nil)
	var docsExists atomic.Bool
	var maskedExists atomic.Bool
	var accessUnknown atomic.Bool
	var writes atomic.Int32
	mu := sync.Mutex{}
	reads := map[string]int{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		reads[r.URL.Path]++
		mu.Unlock()
		repo, title, sha := "", "", ""
		switch r.URL.Path {
		case "/repos/team/app/pulls/418":
			repo, title, sha = "team/app", "Mobile checksum", strings.Repeat("a", 40)
		case "/repos/team/docs/pulls/418":
			if !docsExists.Load() {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			repo, title, sha = "team/docs", "Update docs retention", strings.Repeat("c", 40)
		case "/repos/team/docs/pulls":
			fmt.Fprint(w, "[]")
			return
		case "/repos/team/masked/pulls/418":
			if !maskedExists.Load() {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			repo, title, sha = "team/masked", "Verified through saved reader", strings.Repeat("d", 40)
		case "/repos/team/masked/pulls":
			if accessUnknown.Load() {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			fmt.Fprint(w, "[]")
			return
		default:
			t.Errorf("unexpected fixture URL %s", r.URL.RequestURI())
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"number":418,"html_url":%q,"state":"open","title":%q,"body":"body excluded from discovery","head":{"ref":"feature","sha":%q,"repo":{"full_name":%q}},"base":{"ref":"main","sha":%q,"repo":{"full_name":%q}}}`, "https://scm.example/"+repo+"/pull/418", title, sha, repo, strings.Repeat("b", 40), repo)
	}))
	defer remote.Close()
	for name, repo := range map[string]string{"a-app": "team/app", "b-docs": "team/docs", "c-masked": "team/masked"} {
		synthetic := "local-pr-discovery-fixture"
		if _, err := app.SCM.Save(t.Context(), scm.Input{Name: name, Kind: "github", Repository: repo, APIURL: remote.URL, GitURL: "https://scm.example/" + repo + ".git", Token: &synthetic}, "fixture", false); err != nil {
			t.Fatal(err)
		}
	}
	model := &prDiscoveryModel{}
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: model}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const savedGraph = `{"metadata":{"id":%q},"spec":{"nodes":[{"id":"reader","prompt":"Review the change.","runtime":{"kind":"native"},"model":{"provider":"chat-fixture","name":"fixture"},"loop":{"mode":"event"},"tools":[{"name":"scm.pull_request.read","repositories":%s}]}],"channels":[{"id":"in","to":["reader"]}]}}`
	for _, g := range []struct {
		id, repos string
		active    bool
	}{
		{"app-review", `["a-app"]`, true},
		{"docs-review", `["b-docs"]`, true},
		{"docs-second", `["b-docs"]`, true},
		{"metadata-only", `["c-masked"]`, false},
	} {
		if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", fmt.Sprintf(savedGraph, g.id, g.repos), nil); code != 201 {
			t.Fatalf("save graph %s: %d", g.id, code)
		}
		if g.active {
			if code := call(t, srv, "POST", "/api/v1/graphs/"+g.id+"/start", operatorTok, "", "", nil); code != 200 {
				t.Fatalf("start graph %s: %d", g.id, code)
			}
			waitFor(t, 5*time.Second, "PR reviewer is running", func() bool {
				r, err := app.Store.GetGraphRecord(t.Context(), g.id)
				return err == nil && r.State == domain.GraphRunning && r.RunID != ""
			})
		}
	}
	request := func(index int, path, token, text string) chat.Session {
		t.Helper()
		var session chat.Session
		body := fmt.Sprintf(`{"requestId":"%032x","text":%q}`, index, text)
		if code := call(t, srv, "POST", path, token, "application/json", body, &session); code != 200 || len(session.Messages) < 2 {
			t.Fatalf("request: %d %+v", code, session)
		}
		return session
	}
	last := func(s chat.Session) chat.Message { return s.Messages[len(s.Messages)-1] }
	const base = "/api/v1/chat/sessions"
	const question = "Revise o PR 418."
	if reply := last(request(1, base, viewerTok, question)); reply.Review != nil || !strings.Contains(reply.Text, "operator") {
		t.Fatalf("viewer saw result: %+v", reply)
	}
	mu.Lock()
	zero := len(reads) == 0
	mu.Unlock()
	if !zero {
		t.Fatal("viewer caused a remote read")
	}
	if reply := last(request(16, base, operatorTok, "Revise o PR 419.")); reply.Review != nil || !strings.Contains(reply.Text, "number differs") {
		t.Fatalf("unrequested model-selected PR number survived: %+v", reply)
	}
	mu.Lock()
	zero = len(reads) == 0
	mu.Unlock()
	if !zero {
		t.Fatal("mismatched PR number caused a remote read")
	}
	if reply := last(request(2, base, operatorTok, question)); reply.Review == nil || reply.Review.PR.Repository != "team/app" {
		t.Fatalf("unique verified lookup: %+v", reply)
	}
	mu.Lock()
	checkedOthers := reads["/repos/team/docs/pulls"] == 1 && reads["/repos/team/masked/pulls"] == 1
	mu.Unlock()
	if !checkedOthers {
		t.Fatal("the positive result skipped other repositories")
	}

	accessUnknown.Store(true)
	partial := request(3, base, operatorTok, question)
	if reply := last(partial); reply.Review != nil || !strings.Contains(reply.Text, "Search incomplete") || !strings.Contains(reply.Text, "1. team/app") {
		t.Fatalf("partial search inferred a target: %+v", reply)
	}
	if narrowed := last(request(4, base, operatorTok, "Review https://scm.example/team/app/pull/418.")); narrowed.Review == nil || narrowed.Review.PR.Connection != "a-app" {
		t.Fatalf("explicit URL did not narrow coverage: %+v", narrowed)
	}
	if absent := last(request(5, base, operatorTok, "Review PR 418 in team/docs.")); absent.Review != nil || !strings.Contains(absent.Text, "not found in the checked repositories") {
		t.Fatalf("verified absence: %+v", absent)
	}
	docsExists.Store(true)
	both := request(6, base, operatorTok, question)
	if reply := last(both); reply.Review != nil || !strings.Contains(reply.Text, "Mobile checksum") || !strings.Contains(reply.Text, "Update docs retention") || strings.Contains(reply.Text, "body excluded") {
		t.Fatalf("repository choice: %+v", reply)
	}
	continueAt := base + "/" + both.ID + "/messages"
	targets := request(7, continueAt, operatorTok, "o de docs retention")
	if reply := last(targets); reply.Review != nil || !strings.Contains(reply.Text, "docs-review / reader") || !strings.Contains(reply.Text, "docs-second / reader") {
		t.Fatalf("reviewer choice: %+v", reply)
	}
	choice := last(request(8, continueAt, operatorTok, "2"))
	if choice.Review == nil || choice.Review.PR.Graph != "docs-second" || choice.Review.PR.Connection != "b-docs" || choice.Review.PR.Head != strings.Repeat("c", 40) {
		t.Fatalf("choice bound wrong PR: %+v", choice)
	}
	if reply := last(request(9, continueAt, operatorTok, "Review PR 418 in that repository.")); reply.Review != nil ||
		!strings.Contains(reply.Text, "team/docs") || strings.Contains(reply.Text, "team/app") {
		t.Fatalf("explicit reference did not reuse selected scope: %+v", reply)
	}
	if reply := last(request(10, continueAt, operatorTok, question)); reply.Review != nil ||
		!strings.Contains(reply.Text, "team/docs") || !strings.Contains(reply.Text, "team/app") {
		t.Fatalf("bare request silently reused a previous scope: %+v", reply)
	}
	if reply := last(request(11, base, operatorTok, "Review PR 418 in that repository.")); reply.Review != nil ||
		!strings.Contains(reply.Text, "Which repository should I use?") {
		t.Fatalf("reference without a prior selection: %+v", reply)
	}
	stale := request(12, base, operatorTok, "Review PR 418 in team/docs.")
	if code := call(t, srv, "POST", "/api/v1/graphs/docs-second/nodes/reader/pause", operatorTok, "", "", nil); code != 200 {
		t.Fatalf("pause second reviewer: %d", code)
	}
	if reply := last(request(13, base+"/"+stale.ID+"/messages", operatorTok, "2")); reply.Review != nil || !strings.Contains(reply.Text, "selected reviewer changed") {
		t.Fatalf("stale selection silently switched to another reviewer: %+v", reply)
	}
	maskedExists.Store(true)
	setup := request(14, base, operatorTok, "Review PR 418 in team/masked.")
	if reply := last(setup); reply.Review != nil || !strings.Contains(reply.Text, "no running reviewer") || !strings.Contains(reply.Text, "Review starting stopped workflow metadata-only") {
		t.Fatalf("known PR with no running reviewer: %+v", reply)
	}
	start := last(request(15, base+"/"+setup.ID+"/messages", operatorTok, "2"))
	if start.Review == nil || start.Review.WorkflowControl == nil || start.Review.WorkflowControl.Action != "start" || start.Review.WorkflowControl.Scope.WorkflowID != "metadata-only" || start.Review.PR != nil {
		t.Fatalf("setup choice did not prepare exact native start review: %+v", start)
	}
	record, err := app.Store.GetGraphRecord(t.Context(), "metadata-only")
	if err != nil || record.RunID != "" || record.State != domain.GraphStopped {
		t.Fatalf("setup prematurely started workflow: %+v %v", record, err)
	}
	staleRepository := request(17, base, operatorTok, question)
	if reply := last(staleRepository); reply.Review != nil || !strings.Contains(reply.Text, "a-app") {
		t.Fatalf("expected repository choice before connection edit: %+v", reply)
	}
	if _, err := app.SCM.Save(t.Context(), scm.Input{Name: "a-app", Kind: "github", Repository: "team/new-app", APIURL: remote.URL}, "fixture", true); err != nil {
		t.Fatal(err)
	}
	if reply := last(request(18, base+"/"+staleRepository.ID+"/messages", operatorTok, "a-app")); reply.Review != nil || !strings.Contains(reply.Text, "selected scope could not be verified") {
		t.Fatalf("stale repository choice followed a reconfigured connection: %+v", reply)
	}
	if model.analyses.Load() != 0 || writes.Load() != 0 {
		t.Fatal("discovery or choice dispatched work")
	}
}
