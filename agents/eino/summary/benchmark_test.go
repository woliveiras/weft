package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/agents/eino/protocol"
)

// Both variants serve Protocol v1 and perform the same two HTTP callbacks.
// The model fixture returns immediately to isolate orchestration overhead.
func BenchmarkExternalSummary(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	eino, err := New(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	direct := summaryAgent(func(ctx context.Context, input *turn) (*turn, error) {
		item, err := readDocument(ctx, input)
		if err != nil {
			return nil, err
		}
		return summarize(ctx, item)
	})
	for _, tc := range []struct {
		name  string
		agent protocol.Agent
	}{{"direct", direct}, {"eino", eino}} {
		b.Run(tc.name, func(b *testing.B) {
			var reads, models atomic.Int64
			controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/tools/filesystem.read"):
					reads.Add(1)
					_ = json.NewEncoder(w).Encode(protocol.ToolResult{Content: "An offline document."})
				case strings.HasSuffix(r.URL.Path, "/llm/chat"):
					models.Add(1)
					_ = json.NewEncoder(w).Encode(protocol.ChatResponse{Message: protocol.ChatMessage{Role: "assistant", Content: "Summary."}})
				default:
					http.NotFound(w, r)
				}
			}))
			defer controller.Close()
			handler, err := protocol.NewHandler([]string{controller.URL}, "", map[string]protocol.Agent{"summary": tc.agent})
			if err != nil {
				b.Fatal(err)
			}
			service := httptest.NewServer(handler)
			defer service.Close()
			input := protocol.TurnInput{Agent: "summary", ExecutionID: "bench", Inputs: []protocol.Message{{ID: "doc", Type: "summary.requested", Payload: json.RawMessage(`{"path":"docs/input.md"}`)}}}
			input.Callback.URL = controller.URL + "/internal/v1/executions/bench"
			input.Callback.Token = "fixture"
			raw, err := json.Marshal(input)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				resp, err := service.Client().Post(service.URL+"/v1/turn", "application/json", bytes.NewReader(raw))
				if err != nil {
					b.Fatal(err)
				}
				var result protocol.TurnResult
				err = json.NewDecoder(resp.Body).Decode(&result)
				resp.Body.Close()
				if err != nil || resp.StatusCode != 200 || len(result.Emits) != 1 || result.Emits[0].Type != "summary.proposed" {
					b.Fatalf("turn %d %+v %v", resp.StatusCode, result, err)
				}
			}
			if reads.Load() != int64(b.N) || models.Load() != int64(b.N) {
				b.Fatalf("%d reads and %d model calls for %d turns", reads.Load(), models.Load(), b.N)
			}
		})
	}
}
