// The Chat evals run the normal authenticated HTTP entry points against a
// fresh SQLite installation per case. Only the inference boundary varies.
package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
)

type chatEval struct {
	srv             *httptest.Server
	app             *app.App
	provider, model string
	client          *evalProvider
	sequence        int
}

type evalProvider struct {
	chat  func(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error)
	mu    sync.Mutex
	trace []llm.ToolCall
	t     *testing.T
}

func (p *evalProvider) Name() string                                    { return "chat-eval" }
func (p *evalProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (p *evalProvider) Chat(ctx context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	response, err := p.chat(ctx, r)
	if err != nil {
		p.t.Logf("inference failed: %v", err)
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range response.Message.ToolCalls {
		p.trace = append(p.trace, llm.ToolCall{Name: c.Name, Arguments: slices.Clone(c.Arguments)})
		p.t.Logf("tool %s %s", c.Name, evalLogText(string(c.Arguments)))
	}
	return response, nil
}

func evalLogText(s string) string {
	if chat.ValidateMessage(s) != nil {
		return "[omitted]"
	}
	return s
}

func newChatEval(t *testing.T, scripted func(*llm.ChatRequest) (*llm.ChatResponse, error)) *chatEval {
	t.Helper()
	srv, a := setupApp(t, nil)
	client := &evalProvider{t: t, chat: func(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) { return scripted(r) }}
	e := &chatEval{srv: srv, app: a, client: client, provider: client.Name(), model: "fixture"}
	if *useEvalCodex {
		useCodexForEval(t, e)
	}
	entry := llm.Entry{Config: llm.ProviderConfig{Name: e.provider, Type: "mock", DefaultModel: e.model}, Provider: client}
	if err := a.Models.ReplaceProviders([]llm.Entry{entry}, e.provider); err != nil {
		t.Fatal(err)
	}
	settings, err := json.Marshal(map[string]string{"provider": e.provider, "name": e.model})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.PutSetting(t.Context(), "chat/model", settings, 0, "eval"); err != nil {
		t.Fatal(err)
	}
	t.Logf("Chat eval model=%s", e.model)
	return e
}

func (e *chatEval) calls() []llm.ToolCall {
	e.client.mu.Lock()
	defer e.client.mu.Unlock()
	return slices.Clone(e.client.trace)
}

func (e *chatEval) ask(t *testing.T, sessionID, token, text string) chat.Session {
	t.Helper()
	path := "/api/v1/chat/sessions"
	if sessionID != "" {
		path += "/" + sessionID + "/messages"
	}
	e.sequence++
	var result chat.Session
	status := call(t, e.srv, "POST", path, token, "application/json", fmt.Sprintf(`{"requestId":"%032x","text":%q}`, e.sequence, text), &result)
	if status != 200 || len(result.Messages) < 2 {
		t.Fatalf("submit %q: HTTP %d, messages %d", text, status, len(result.Messages))
	}
	reply := result.Messages[len(result.Messages)-1]
	t.Logf("operator %q → Weft %q", text, evalLogText(reply.Text))
	return result
}

func evalTool(name string, args any) (*llm.ChatResponse, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "fixture-call", Name: name, Arguments: data}}}, StopReason: "tool_calls"}, nil
}
