package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/scm"
)

func TestChatEvalPullRequest(t *testing.T) {
	const question = "Revise o PR 418."
	e := newChatEval(t, func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
		if last := req.Messages[len(req.Messages)-1]; last.Role != llm.RoleUser || last.Content != question {
			return nil, fmt.Errorf("unexpected inference during PR source selection")
		}
		return evalTool("analyze_pull_request", map[string]string{"prNumber": "418", "title": "Revisar PR"})
	})
	const mobileRepo, docsRepo = "team/client", "team/docs"
	const mobileTitle, docsTitle = "Mobile checksum", "Improve onboarding docs"
	const mobileConnection, docsConnection = "eval-mobile", "eval-docs"
	mobileHead, docsHead, baseSHA := strings.Repeat("a", 40), strings.Repeat("c", 40), strings.Repeat("b", 40)
	var mobileReads, docsReads, diffReads, writes atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if strings.Contains(r.URL.Path, "/compare/") || strings.Contains(r.Header.Get("Accept"), "diff") {
			diffReads.Add(1)
		}
		var repo, title, head string
		switch r.URL.Path {
		case "/repos/" + mobileRepo + "/pulls/418":
			mobileReads.Add(1)
			repo, title, head = mobileRepo, mobileTitle, mobileHead
		case "/repos/" + docsRepo + "/pulls/418":
			docsReads.Add(1)
			repo, title, head = docsRepo, docsTitle, docsHead
		default:
			t.Errorf("unexpected SCM read before confirmation: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"number":418,"html_url":%q,"title":%q,"body":"fixture private PR body","state":"open","head":{"ref":"feature","sha":%q,"repo":{"full_name":%q}},"base":{"ref":"main","sha":%q,"repo":{"full_name":%q}}}`, "https://scm.example/"+repo+"/pull/418", title, head, repo, baseSHA, repo)
	}))
	t.Cleanup(remote.Close)
	for _, input := range []struct{ name, repo string }{{mobileConnection, mobileRepo}, {docsConnection, docsRepo}} {
		synthetic := "local-chat-pr-eval-fixture"
		if _, err := e.app.SCM.Save(t.Context(), scm.Input{Name: input.name, Kind: "github", Repository: input.repo, APIURL: remote.URL, Token: &synthetic}, "fixture", false); err != nil {
			t.Fatal(err)
		}
	}
	const workflowID = "eval-pr-review"
	graph := fmt.Sprintf(`{"metadata":{"id":%q},"spec":{"nodes":[{"id":"reader","prompt":"Review the pinned change.","runtime":{"kind":"native"},"model":{"provider":%q,"name":%q},"loop":{"mode":"event"},"tools":[{"name":"scm.pull_request.read","repositories":[%q,%q]}]}],"channels":[{"id":"in","to":["reader"]}]}}`, workflowID, e.provider, e.model, mobileConnection, docsConnection)
	if status := call(t, e.srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); status != http.StatusCreated {
		t.Fatalf("save reviewer workflow: %d", status)
	}
	if status := call(t, e.srv, "POST", "/api/v1/graphs/"+workflowID+"/start", operatorTok, "", "", nil); status != http.StatusOK {
		t.Fatalf("start reviewer workflow: %d", status)
	}
	var runID string
	waitFor(t, 5*time.Second, "reviewer is running", func() bool {
		row, err := e.app.Store.GetGraphRecord(t.Context(), workflowID)
		if err != nil || row.State != domain.GraphRunning || row.RunID == "" {
			return false
		}
		runID = row.RunID
		return true
	})

	session := e.ask(t, "", operatorTok, question)
	reply := session.Messages[len(session.Messages)-1]
	if reply.State != "discovery" || reply.Review != nil || !strings.Contains(reply.Text, mobileRepo) || !strings.Contains(reply.Text, mobileTitle) ||
		!strings.Contains(reply.Text, docsRepo) || !strings.Contains(reply.Text, docsTitle) || strings.Contains(reply.Text, "fixture private PR body") {
		t.Fatalf("first request did not present both candidate repositories: %+v", reply)
	}
	if mobileReads.Load() != 1 || docsReads.Load() != 1 {
		t.Fatalf("first query did not verify both sources, mobile=%d docs=%d", mobileReads.Load(), docsReads.Load())
	}
	observed := e.calls()
	if len(observed) == 0 || len(observed) > 2 {
		t.Fatalf("expected a native PR request with at most one correction, got %d calls", len(observed))
	}
	for _, invocation := range observed {
		if invocation.Name != "analyze_pull_request" {
			t.Fatalf("unexpected operation for PR analysis: %s", invocation.Name)
		}
	}
	plan, recognized, err := chat.NativeOperationPlan(observed[len(observed)-1])
	if err != nil || !recognized || plan.Kind != chat.AnalyzePR || plan.PRSelector != (chat.PRSelector{Number: "418"}) {
		t.Fatalf("model did not leave an unscoped number for repository discovery: %+v, %v", plan, err)
	}

	session = e.ask(t, session.ID, operatorTok, "o do mobile")
	after := session.Messages[len(session.Messages)-1].Review
	if after == nil || after.State != chat.ReviewAwaiting || after.PR == nil || after.Digest == "" {
		t.Fatalf("choosing a candidate did not prepare a review: %+v", session.Messages[len(session.Messages)-1])
	}
	p := after.PR
	if p.Number != 418 || p.Repository != mobileRepo || p.Connection != mobileConnection || p.Head != mobileHead || p.Base != baseSHA ||
		p.Graph != workflowID || p.Version != 1 || p.Node != "reader" || p.Run != runID || p.ModelProvider != e.provider || p.ModelName != e.model {
		t.Fatalf("review was not pinned to the selected PR and reviewer: %+v", p)
	}
	if mobileReads.Load() < 2 || docsReads.Load() != 1 || diffReads.Load() != 0 || writes.Load() != 0 || len(e.calls()) != len(observed) {
		t.Fatalf("selection had unexpected effects: mobile=%d docs=%d diffs=%d writes=%d calls=%d", mobileReads.Load(), docsReads.Load(), diffReads.Load(), writes.Load(), len(e.calls()))
	}
}
