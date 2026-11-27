package summary_test

import (
	"bytes"
	"context"
	"encoding/json"

	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/agents/eino/protocol"
	"github.com/woliveiras/weft/agents/eino/summary"
)

func TestSummaryService(t *testing.T) {
	agent, err := summary.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		scenario              string
		status, models, emits int
	}{
		{"valid", 200, 1, 1}, {"denied", 500, 0, 0}, {"tool error", 500, 0, 0}, {"truncated read", 500, 0, 0}, {"yield", 200, 0, 0}, {"provider failure", 500, 1, 0}, {"truncated model", 500, 1, 0}, {"missing input", 500, 0, 0}, {"wrong secret", 401, 0, 0}, {"unknown agent", 404, 0, 0},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			var models atomic.Int32
			controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer ephemeral-fixture" {
					t.Error("wrong execution credential")
				}
				switch r.URL.Path {
				case "/internal/v1/executions/exe_1/tools/filesystem.read":
					var args struct{ Path string }
					if json.NewDecoder(r.Body).Decode(&args) != nil || args.Path != "docs/input.md" {
						t.Error("incorrect file read")
					}
					if tc.scenario == "denied" {
						http.Error(w, "private-fixture", 403)
						return
					}
					_ = json.NewEncoder(w).Encode(protocol.ToolResult{Content: "document fixture", IsError: tc.scenario == "tool error", Yield: tc.scenario == "yield", Truncated: tc.scenario == "truncated read"})
				case "/internal/v1/executions/exe_1/llm/chat":
					models.Add(1)
					var req protocol.ChatRequest
					if json.NewDecoder(r.Body).Decode(&req) != nil || req.System != "controller preamble" || len(req.Messages) != 2 || req.Messages[0].Content != "write a summary" || !strings.Contains(req.Messages[1].Content, "document fixture") {
						t.Error("missing model context")
					}
					if tc.scenario == "provider failure" {
						http.Error(w, "private-fixture", 502)
						return
					}
					stop := "stop"
					if tc.scenario == "truncated model" {
						stop = "length"
					}
					_ = json.NewEncoder(w).Encode(protocol.ChatResponse{StopReason: stop, Message: protocol.ChatMessage{Role: "assistant", Content: "A reviewable summary."}})
				default:
					t.Errorf("unexpected callback %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer controller.Close()
			handler, err := protocol.NewHandler([]string{controller.URL}, "shared-fixture", map[string]protocol.Agent{"summarize": agent})
			if err != nil {
				t.Fatal(err)
			}
			in := protocol.TurnInput{Agent: "summarize", ExecutionID: "exe_1", System: "controller preamble", Prompt: "write a summary", Inputs: []protocol.Message{{ID: "msg_123", Type: "summary.requested", Payload: json.RawMessage(`{"path":"docs/input.md"}`)}}}
			in.Callback.URL = controller.URL + "/internal/v1/executions/exe_1"
			in.Callback.Token = "ephemeral-fixture"
			if tc.scenario == "missing input" {
				in.Inputs = nil
			}
			if tc.scenario == "unknown agent" {
				in.Agent = "missing"
			}
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/turn", bytes.NewReader(data))
			if tc.scenario != "wrong secret" {
				req.Header.Set("X-Weft-Runtime-Secret", "shared-fixture")
			}
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			var result struct {
				Emits []struct {
					Type    string
					Payload struct{ SourceMessageID, Summary string }
				}
			}
			if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if res.Code != tc.status || int(models.Load()) != tc.models || len(result.Emits) != tc.emits || strings.Contains(res.Body.String(), "private-fixture") {
				t.Fatalf("status=%d models=%d result=%s", res.Code, models.Load(), res.Body)
			}
			if tc.emits == 1 && (result.Emits[0].Type != "summary.proposed" || result.Emits[0].Payload.SourceMessageID != "msg_123" || result.Emits[0].Payload.Summary != "A reviewable summary.") {
				t.Fatal(result)
			}
		})
	}
}
