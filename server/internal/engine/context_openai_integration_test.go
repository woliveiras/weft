package engine_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/storage"
)

// chatCompletionsFake is an OpenAI-compatible server. It knows nothing about
// context windows: the configured window reaches the native runtime only
// through the openai plugin, over the real HTTP path.
type chatCompletionsFake struct {
	mu          sync.Mutex
	rounds      int
	compactions int
	lostTask    bool
	sawSummary  bool
}

type fakeWireRequest struct {
	Messages []struct {
		Role    string  `json:"role"`
		Content *string `json:"content"`
	} `json:"messages"`
	Tools []json.RawMessage `json:"tools"`
}

func (f *chatCompletionsFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req fakeWireRequest
	if r.URL.Path != "/v1/chat/completions" || json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var user []string
	for _, m := range req.Messages {
		if m.Role == "user" && m.Content != nil {
			user = append(user, *m.Content)
		}
		if m.Content != nil {
			f.sawSummary = f.sawSummary || strings.Contains(*m.Content, "Read docs/big.txt several times")
		}
	}
	// Compaction requests carry no tools and quote the task as "Task:".
	if len(req.Tools) == 0 && len(user) > 0 && strings.HasPrefix(user[0], "Task:\n") {
		f.compactions++
		writeCompletion(w, `{"role":"assistant","content":"Read docs/big.txt several times; nothing changed yet."}`, "stop", 1000, 20)
		return
	}
	if len(user) == 0 || !strings.Contains(user[0], "Read docs/big.txt") {
		f.lostTask = true
	}
	f.rounds++
	if f.rounds > 16 {
		writeCompletion(w, `{"role":"assistant","content":"done reading"}`, "stop", 10, 2)
		return
	}
	args, _ := json.Marshal(`{"path":"docs/big.txt","limit":30}`)
	msg := fmt.Sprintf(`{"role":"assistant","content":%q,"tool_calls":[{"id":"r%d","type":"function","function":{"name":%q,"arguments":%s}}]}`,
		strings.Repeat("Still checking the file for the setting. ", 20), f.rounds, llm.WireToolName("filesystem.read"), args)
	writeCompletion(w, msg, "tool_calls", 10, 2)
}

func writeCompletion(w http.ResponseWriter, message, finish string, in, out int) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"model":"fixture","choices":[{"message":%s,"finish_reason":%q}],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}}`,
		message, finish, in, out, in+out)
}

// An OpenAI-type provider (the type Gemini's compatible endpoint uses)
// configured with a small context window: the native turn budgets against
// that window, elides and compacts, and every request it sends passes the
// plugin's own pre-send check.
func TestNativeTurnHonoursAnOpenAIProvidersConfiguredWindow(t *testing.T) {
	const window = 4096
	fake := &chatCompletionsFake{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	cfg := llm.ProviderConfig{Name: "compat", Type: "vllm", BaseURL: srv.URL + "/v1", DefaultModel: "fixture", ContextWindow: window}
	prov, err := llm.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := llm.ContextWindowOf(prov); got != window {
		t.Fatalf("ContextWindowOf = %d, want %d", got, window)
	}

	dir := t.TempDir()
	r := newRig(t, openStore(t, dir), memory.New(), dir, "context-openai-instance", func(o *engine.Options) {
		o.Models.(*llm.Registry).Add(cfg, prov)
	})
	ws := filepath.Join(r.dir, "ws", "context-openai")
	if err := os.MkdirAll(filepath.Join(ws, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, fmt.Sprintf("line %03d: setting value for the context window test", i))
	}
	if err := os.WriteFile(filepath.Join(ws, "docs", "big.txt"), []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	r.deploy("metadata: {name: context-openai}\nspec:\n  nodes:\n    - id: reader\n      runtime: {kind: native}\n      prompt: Read docs/big.txt and report the setting\n      model: {provider: compat, name: fixture}\n      loop: {mode: once}\n      isolation: {mode: process}\n      tools: [filesystem.read]\n      limits: {turnTimeout: 20s, maxToolCalls: 40}\n")

	var x *domain.Execution
	eventually(t, 15*time.Second, "reader turn finished", func() bool {
		xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "context-openai", NodeID: "reader", Limit: 1})
		if len(xs) == 1 && xs[0].Status.Terminal() {
			x = xs[0]
			return true
		}
		return false
	})
	// A request over the window would have failed the turn with
	// ErrContextExceeded before reaching the server.
	if x.Status != domain.ExecSucceeded {
		t.Fatalf("turn %s: %s", x.Status, x.Error)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lostTask {
		t.Fatal("a request lost the task message")
	}
	if fake.compactions == 0 || !fake.sawSummary {
		t.Fatalf("compactions=%d summary seen=%v", fake.compactions, fake.sawSummary)
	}
	evs, err := r.store.ListEvents(r.ctx, storage.EventFilter{GraphID: "context-openai", Types: []domain.EventType{domain.EvAgentContext}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]bool{}
	for _, ev := range evs {
		var ce runtime.ContextEvent
		if err := json.Unmarshal(ev.Data, &ce); err != nil {
			t.Fatal(err)
		}
		if ce.Window != window || ce.Outcome != "ok" || ev.ExecutionID != x.ID {
			t.Fatalf("event %+v", ce)
		}
		ops[ce.Operation] = true
	}
	if !ops["elide"] || !ops["compact"] {
		t.Fatalf("expected elide and compact events, got %v", ops)
	}
}
