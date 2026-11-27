package factory

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/woliveiras/weft/agents/eino/protocol"
)

func TestDecide(t *testing.T) {
	for _, tc := range []struct {
		model    string
		problems []string
		want     string
	}{
		{"approved", []string{"empty"}, "changes_requested"}, {"", nil, "approved"}, {"changes_requested", nil, "changes_requested"}, {"", []string{"TODO"}, "changes_requested"},
	} {
		out, err := decide(t.Context(), &review{verdict: tc.model, problems: tc.problems})
		if err != nil || out.verdict != tc.want || out.comments == nil {
			t.Fatal(tc, out, err)
		}
	}
}

func TestReviewerRoutesEachCodeReady(t *testing.T) {
	agent, err := Reviewer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, reply, want, comment string }{
		{"src/ok", "not JSON", "approved", "heuristic review: files present, non-empty, no TODOs"},
		{"src/ok", "Verdict: " + `{"verdict":"changes_requested","comments":["naming"]}`, "changes_requested", "naming"},
		{"src/todo", `{"verdict":"approved"}`, "changes_requested", "src/todo contains TODO"},
		{"src/empty", `{"verdict":"approved"}`, "changes_requested", "src/empty is empty or unreadable"},
		{"src/forbidden", `{"verdict":"approved"}`, "changes_requested", "src/forbidden is empty or unreadable"},
		{"src/missing", `{"verdict":"approved"}`, "changes_requested", "src/missing is empty or unreadable"},
		{"", `{"verdict":"approved"}`, "changes_requested", "no files were changed"},
		{"src/ok", "unavailable", "", ""},
	} {
		t.Run(test.path+test.want+test.reply, func(t *testing.T) {
			reads, modelCalls := 0, 0
			callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/tools/filesystem.read"):
					var args struct{ Path string }
					if json.NewDecoder(r.Body).Decode(&args) != nil || args.Path != test.path {
						t.Error("wrong read")
					}
					reads++
					if args.Path == "src/forbidden" {
						http.Error(w, "denied", 403)
						return
					}
					contents := map[string]string{"src/ok": "ready", "src/empty": " ", "src/todo": "fix TODO"}
					_ = json.NewEncoder(w).Encode(protocol.ToolResult{Content: contents[args.Path], IsError: args.Path == "src/missing"})
				case strings.HasSuffix(r.URL.Path, "/llm/chat"):
					modelCalls++
					if test.reply == "unavailable" {
						http.Error(w, "unavailable", 502)
						return
					}
					_ = json.NewEncoder(w).Encode(protocol.ChatResponse{StopReason: "stop", Message: protocol.ChatMessage{Role: "assistant", Content: test.reply}})
				default:
					t.Errorf("unexpected callback %s", r.URL.Path)
				}
			}))
			defer callback.Close()
			handler, err := protocol.NewHandler([]string{callback.URL}, "", map[string]protocol.Agent{"reviewer": agent})
			if err != nil {
				t.Fatal(err)
			}
			in := protocol.TurnInput{Agent: "reviewer", ExecutionID: "exe_1"}
			in.Callback.URL, in.Callback.Token = callback.URL+"/internal/v1/executions/exe_1", "fixture"
			payload, err := json.Marshal(map[string]any{"task_id": "t1", "request_id": "req", "revision": json.Number("12345678901234567890"), "files": []string{test.path}})
			if err != nil {
				t.Fatal(err)
			}
			in.Inputs = []protocol.Message{{Type: "other"}, {Type: "code.ready", Payload: payload}, {Type: "code.ready", Payload: payload}}
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/turn", bytes.NewReader(data))
			out := httptest.NewRecorder()
			handler.ServeHTTP(out, req)
			var result struct {
				Emits []struct {
					Type    string
					Payload struct {
						Verdict  string
						Comments []string
						Request  string `json:"request_id"`
						Revision json.Number
					}
				}
			}
			if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if out.Code != 500 || len(result.Emits) != 0 {
					t.Fatal(out.Code, result)
				}
				return
			}
			if out.Code != 200 || modelCalls != 2 || (test.path != "" && reads != 2) || len(result.Emits) != 2 {
				t.Fatal(out.Code, modelCalls, reads, result)
			}
			for _, em := range result.Emits {
				if em.Type != "review."+test.want || em.Payload.Verdict != test.want || !slices.Contains(em.Payload.Comments, test.comment) || em.Payload.Request != "req" || em.Payload.Revision != "12345678901234567890" {
					t.Fatal(em)
				}
			}
		})
	}
}
