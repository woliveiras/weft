package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/llm/gemini"
)

func TestGeminiRetrySeparatesLatestQuestionFromOlderHandoff(t *testing.T) {
	const first = "Open budgets."
	const second = "Link the task tsk_coral."
	const later = "Open workflows."
	var attempts atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Contents []struct {
				Role  string
				Parts []struct{ Text string }
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Contents) == 0 {
			t.Errorf("invalid Gemini request")
			w.WriteHeader(400)
			return
		}
		current := body.Contents[len(body.Contents)-1]
		if current.Role != "user" || len(current.Parts) != 1 {
			t.Errorf("Gemini received merged operator requests: %+v", current)
			w.WriteHeader(400)
			return
		}
		var part any
		switch current.Parts[0].Text {
		case first, later:
			dest := "budgets"
			if current.Parts[0].Text == later {
				dest = "workflows"
			}
			part = map[string]any{"functionCall": map[string]any{"name": "open_console_page", "args": map[string]string{"destination": dest}}}
		case second:
			if len(body.Contents) != 3 || body.Contents[0].Parts[0].Text != first || body.Contents[1].Role != "model" {
				t.Errorf("retry did not stop at the original turn: %+v", body.Contents)
			}
			if attempts.Add(1) == 1 {
				part = map[string]string{"text": "unstructured answer"}
			} else {
				part = map[string]any{"functionCall": map[string]any{"name": "inspect_task", "args": map[string]string{"task": "tsk_coral", "taskView": "link"}}}
			}
		default:
			t.Errorf("unexpected fixture question: %q", current.Parts[0].Text)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"role": "model", "parts": []any{part}}}}})
	}))
	defer remote.Close()
	p, err := gemini.New(llm.ProviderConfig{Name: "fixture", BaseURL: remote.URL, APIKeyValue: "fixture-only"})
	if err != nil {
		t.Fatal(err)
	}
	e := newChatEval(t, nil)
	e.client.chat = p.Chat
	seedEvalTasks(t, e, "failed", []evalTask{{"tsk_coral", "Revisão"}})
	s := e.ask(t, "", viewerTok, first)
	s = e.ask(t, s.ID, viewerTok, second)
	original := s.Messages[2]
	if original.State != "unavailable" {
		t.Fatalf("expected a retryable failure, got %s", original.State)
	}
	s = e.ask(t, s.ID, viewerTok, later)
	path := fmt.Sprintf("/api/v1/chat/sessions/%s/messages/%s/retry", s.ID, original.ID)
	var retried chat.Session
	if status := call(t, e.srv, "POST", path, viewerTok, "", "", &retried); status != 200 {
		t.Fatalf("retry HTTP %d", status)
	}
	if len(retried.Messages) != 6 || retried.Messages[2].State != "interpreted" || retried.Messages[3].Task == nil || retried.Messages[3].Task.ID != "tsk_coral" || retried.Messages[5].Destination != "workflows" || attempts.Load() != 2 {
		t.Fatalf("incorrect retry result: %+v", retried.Messages)
	}
}
