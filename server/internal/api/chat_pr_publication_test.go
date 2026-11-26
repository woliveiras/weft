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
	"github.com/woliveiras/weft/server/internal/storage"
)

type publishPRFixture struct{ manualPRFixture }

func (f *publishPRFixture) Chat(ctx context.Context, q *llm.ChatRequest) (*llm.ChatResponse, error) {
	base, err := f.manualPRFixture.Chat(ctx, q)
	if err != nil || q.Metadata["operation"] == "chat_interpretation" {
		return base, err
	}
	base.Message.Content = `{"summary":"Bounded line changes.","comments":[{"path":"file","side":"RIGHT","line":1,"body":"First approved."},{"path":"file","side":"RIGHT","line":2,"body":"Reply will be lost."},{"path":"file","side":"RIGHT","line":3,"body":"Provider rejects this."},{"path":"file","side":"LEFT","line":1,"body":"Leave this unselected."}]}`
	return base, nil
}

func TestChatPublishesOnlyConfirmedPRCommentsAndReconcilesLostResponse(t *testing.T) {
	server, app := setupApp(t, nil)
	const metadata = "/repos/team/repo/pulls/418"
	const comments = metadata + "/comments"
	head, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
	var moved atomic.Bool
	var mu sync.Mutex
	var posted []map[string]any
	var accepted []map[string]any
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == metadata:
			current := head
			if moved.Load() {
				current = strings.Repeat("c", 40)
			}
			fmt.Fprintf(w, `{"number":418,"html_url":"https://scm.example/team/repo/pull/418","state":"open","head":{"ref":"feature","sha":%q,"repo":{"full_name":"team/repo"}},"base":{"ref":"main","sha":%q,"repo":{"full_name":"team/repo"}}}`, current, base)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/team/repo/compare/"+base+"..."):
			fmt.Fprint(w, "diff --git a/file b/file\n--- a/file\n+++ b/file\n@@ -1 +1,3 @@\n-old\n+fixture change\n+second addition\n+third addition\n")
		case r.Method == "GET" && r.URL.Path == comments:
			mu.Lock()
			defer mu.Unlock()
			if err := json.NewEncoder(w).Encode(accepted); err != nil {
				t.Error(err)
			}
		case r.Method == "POST" && r.URL.Path == comments:
			var item map[string]any
			if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			item["id"] = 100 + len(posted)
			posted = append(posted, item)
			body, _ := item["body"].(string)
			reject := strings.HasPrefix(body, "Provider rejects this.")
			if !reject {
				accepted = append(accepted, item)
			}
			mu.Unlock()
			if reject {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			if strings.HasPrefix(body, "Reply will be lost.") {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
				return
			}
			w.WriteHeader(http.StatusCreated)
			if err := json.NewEncoder(w).Encode(item); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected fixture request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer remote.Close()
	synthetic := "inline-comment-fixture"
	if _, err := app.SCM.Save(t.Context(), scm.Input{Name: "origin", Kind: "github", Repository: "team/repo", APIURL: remote.URL, Token: &synthetic}, "fixture", false); err != nil {
		t.Fatal(err)
	}
	model := &publishPRFixture{}
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: model}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	graph := `{"metadata":{"id":"pr-review"},"spec":{"nodes":[{"id":"reader","prompt":"Review the change.","runtime":{"kind":"native"},"model":{"provider":"chat-fixture","name":"fixture"},"loop":{"mode":"event"},"tools":[{"name":"scm.pull_request.read","repositories":["origin"]},{"name":"scm.pull_request.comment","repositories":["origin"]}]}],"channels":[{"id":"in","to":["reader"]}]}}`
	if code := call(t, server, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); code != 201 {
		t.Fatalf("create workflow: %d", code)
	}
	if code := call(t, server, "POST", "/api/v1/graphs/pr-review/start", operatorTok, "", "", nil); code != 200 {
		t.Fatalf("start workflow: %d", code)
	}
	waitFor(t, 5*time.Second, "review workflow running", func() bool {
		r, err := app.Store.GetGraphRecord(t.Context(), "pr-review")
		return err == nil && r.DesiredState == domain.GraphRunning && r.State == domain.GraphRunning && r.RunID != ""
	})
	const sessions = "/api/v1/chat/sessions"
	confirm := func(id string, rv *chat.OperationReview, digest string) (chat.Session, int) {
		t.Helper()
		var result chat.Session
		status := call(t, server, "POST", sessions+"/"+id+"/operations/"+rv.ID+"/confirm", operatorTok, "application/json", fmt.Sprintf(`{"digest":%q}`, digest), &result)
		return result, status
	}
	analyze := func(id string) chat.Session {
		t.Helper()
		var session chat.Session
		if code := call(t, server, "POST", sessions, operatorTok, "application/json", fmt.Sprintf(`{"requestId":%q,"text":"Revise o PR 418."}`, id), &session); code != 200 || len(session.Messages) != 2 || session.Messages[1].Review == nil {
			t.Fatalf("analysis review: %d %+v", code, session.Messages)
		}
		if _, code := confirm(id, session.Messages[1].Review, session.Messages[1].Review.Digest); code != 200 {
			t.Fatalf("analyze: %d", code)
		}
		waitFor(t, 8*time.Second, "correlated structured analysis", func() bool {
			return call(t, server, "GET", sessions+"/"+id, operatorTok, "", "", &session) == 200 && session.Messages[1].Review.State == chat.ReviewApplied
		})
		if draft := session.Messages[1].Review; len(draft.PRComments) != 4 || draft.PRSummary != "Bounded line changes." {
			t.Fatalf("draft did not expose individual comments: %+v", draft)
		}
		return session
	}
	prepare := func(session chat.Session, token, body string) (chat.Session, int) {
		t.Helper()
		var result chat.Session
		code := call(t, server, "POST", sessions+"/"+session.ID+"/operations/"+session.Messages[1].Review.ID+"/pr-comment-review", token, "application/json", body, &result)
		return result, code
	}
	stale := analyze("4325bcfa09134567aef46254553a6104")
	for _, test := range []struct {
		token, body string
		code        int
	}{
		{viewerTok, `{"commentIds":[1]}`, 403}, {adminTok, `{"commentIds":[1]}`, 404},
		{operatorTok, `{"commentIds":[7]}`, 400}, {operatorTok, `{"commentIds":[1,1]}`, 400},
	} {
		if _, code := prepare(stale, test.token, test.body); code != test.code {
			t.Fatalf("prepare status %d, want %d", code, test.code)
		}
	}
	s, code := prepare(stale, operatorTok, `{"commentIds":[1]}`)
	if code != 200 || s.Messages[2].Review == nil || s.Messages[2].Review.State != chat.ReviewAwaiting {
		t.Fatalf("prepare stale case: %d %+v", code, s.Messages)
	}
	moved.Store(true)
	rv := s.Messages[2].Review
	if got, code := confirm(stale.ID, rv, rv.Digest); code != 200 || got.Messages[2].Review.State != chat.ReviewConflict {
		t.Fatalf("changed head was accepted: %d %+v", code, got.Messages)
	}
	mu.Lock()
	writesBefore := len(posted)
	mu.Unlock()
	if writesBefore != 0 {
		t.Fatal("changed head reached the provider")
	}
	moved.Store(false)
	current := analyze("5325bcfa09134567aef46254553a6104")
	selected, code := prepare(current, operatorTok, `{"commentIds":[3,1,2]}`)
	if code != 200 || len(selected.Messages) != 3 {
		t.Fatalf("prepare selected comments: %d %+v", code, selected.Messages)
	}
	rv = selected.Messages[2].Review
	if moved := rv.PRPublication; moved == nil || len(moved.Comments) != 3 || moved.Analysis.Head != head {
		t.Fatalf("incorrect reviewed selection: %+v", rv)
	}
	if _, code = confirm(current.ID, rv, strings.Repeat("0", 64)); code != 409 {
		t.Fatalf("publication accepted a mismatched digest: %d", code)
	}
	if _, code = prepare(current, operatorTok, `{"commentIds":[1,2,3]}`); code != 200 {
		t.Fatalf("repeated preview failed: %d", code)
	}
	mu.Lock()
	writesBefore = len(posted)
	mu.Unlock()
	if writesBefore != 0 {
		t.Fatal("preview was treated as approval")
	}
	result, code := confirm(current.ID, rv, rv.Digest)
	if code != 200 || len(result.Messages) != 3 {
		t.Fatalf("confirm: %d %+v", code, result.Messages)
	}
	rv = result.Messages[2].Review
	if reads := rv.PRReceipts; rv.State != chat.ReviewIncomplete || len(reads) != 3 || reads[0].State != "published" || reads[1].State != "unknown" || reads[2].Code != "provider_rejected" {
		t.Fatalf("did not distinguish partial outcomes: %+v", rv)
	}
	moved.Store(true)
	var reopened chat.Session
	if code := call(t, server, "GET", sessions+"/"+current.ID, operatorTok, "", "", &reopened); code != 200 || reopened.Messages[2].Review.PRReceipts[1].State != "unknown" {
		t.Fatalf("stored partial result was unavailable after reopening: %d", code)
	}
	if code := call(t, server, "POST", sessions+"/"+current.ID+"/operations/"+rv.ID+"/pr-comment-receipts", operatorTok, "", "", &reopened); code != 200 || reopened.Messages[2].Review.PRReceipts[1].State != "published" || reopened.Messages[2].Review.PRReceipts[1].ID != 101 {
		t.Fatalf("lost response did not reconcile at the original head: %d %+v", code, reopened.Messages)
	}
	if _, code := confirm(current.ID, rv, rv.Digest); code != 200 {
		t.Fatalf("reopened confirmation was not idempotent: %d", code)
	}

	moved.Store(false)
	repeated := analyze("6325bcfa09134567aef46254553a6104")
	s, code = prepare(repeated, operatorTok, `{"commentIds":[1,2,3]}`)
	if code != 200 || len(s.Messages) != 3 {
		t.Fatalf("prepare repeated selection: %d %+v", code, s.Messages)
	}
	repeatedReview := s.Messages[2].Review
	s, code = confirm(repeated.ID, repeatedReview, repeatedReview.Digest)
	if code != 200 || s.Messages[2].Review.PRReceipts[1].ID != 101 {
		t.Fatalf("a separate review did not recover the original receipts: %d %+v", code, s.Messages)
	}

	s, code = prepare(repeated, operatorTok, `{"commentIds":[4]}`)
	if code != 200 || len(s.Messages) != 4 {
		t.Fatalf("prepare grant-change case: %d %+v", code, s.Messages)
	}
	revokedReview := s.Messages[3].Review
	withoutWrite := strings.Replace(graph, `,{"name":"scm.pull_request.comment","repositories":["origin"]}`, "", 1)
	if code := call(t, server, "PUT", "/api/v1/graphs/pr-review", adminTok, "application/json", withoutWrite, nil); code != 200 {
		t.Fatalf("fixture could not save the workflow without the publication grant: %d", code)
	}
	if _, code := prepare(repeated, operatorTok, `{"commentIds":[4]}`); code != 403 {
		t.Fatalf("saved grant change did not invalidate a preview: %d", code)
	}
	if got, code := confirm(repeated.ID, revokedReview, revokedReview.Digest); code != 200 || got.Messages[3].Review.State != chat.ReviewConflict {
		t.Fatalf("saved grant change did not invalidate confirmation: %d %+v", code, got.Messages)
	}
	audit, err := app.Store.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{domain.EvReviewCommentPublished, domain.EvToolInvoked, domain.EvToolCompleted}})
	if err != nil {
		t.Fatal(err)
	}
	publishedEvents := 0
	for _, event := range audit {
		if event.Type == domain.EvReviewCommentPublished {
			publishedEvents++
		}
		raw, err := json.Marshal(event.Data)
		if err != nil || strings.Contains(string(raw), "First approved") || strings.Contains(string(raw), "Reply will be lost") || strings.Contains(string(raw), "weft-inline:") {
			t.Fatalf("review comment content leaked into %s", event.Type)
		}
	}
	if publishedEvents != 2 {
		t.Fatalf("expected one publication event per provider receipt, got %d", publishedEvents)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(posted) != 3 || len(accepted) != 2 {
		t.Fatalf("unexpected provider effects: posted %d, accepted %d", len(posted), len(accepted))
	}
	for i, post := range posted {
		if post["commit_id"] != head || post["side"] != "RIGHT" || post["path"] != "file" || post["line"] != float64(i+1) || strings.Contains(fmt.Sprint(post["body"]), "unselected") {
			t.Fatalf("comment %d was not posted to its reviewed line: %+v", i+1, post)
		}
	}
}
