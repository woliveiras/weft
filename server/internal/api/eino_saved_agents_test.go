package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
)

type savedAgentModel struct{ catalogModel }

func (savedAgentModel) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	last := req.Messages[len(req.Messages)-1]
	for _, m := range req.Messages {
		if m.Role != llm.RoleUser {
			continue
		}
		for prompt, path := range map[string]string{"Review alpha.": "docs/input.md", "Review beta.": "src/input.md"} {
			if m.Content != prompt {
				continue
			}
			var call llm.ToolCall
			switch {
			case last.Role != llm.RoleTool:
				call = llm.ToolCall{ID: "read", Name: "filesystem.read", Arguments: json.RawMessage(fmt.Sprintf(`{"path":%q}`, path))}
			case last.ToolCallID == "read":
				if !strings.Contains(last.Content, "Evidence for "+prompt) {
					return nil, fmt.Errorf("scoped input missing for %s", prompt)
				}
				call = llm.ToolCall{ID: "emit", Name: "emit", Arguments: json.RawMessage(fmt.Sprintf(`{"type":"review.done","payload":{"prompt":%q}}`, prompt))}
			default:
				return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: "Done"}}, nil
			}
			return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}}}, nil
		}
	}

	return nil, fmt.Errorf("saved agent prompt was not resolved")
}

func TestEinoRunsSavedInlineAndPinnedAgentsAfterRestart(t *testing.T) {
	root := t.TempDir()
	configure := func(c *config.Config) {
		c.Storage.Dir = root
		c.Queue = config.Queue{Type: "fs", Dir: filepath.Join(root, "queue")}
		c.Workspaces.Root = filepath.Join(root, "ws")
	}
	const endpoint = "/api/v1/graphs/saved-agents"
	var original domain.Graph
	var catalog agentcatalog.Agent
	readCatalog := func(t *testing.T, s *httptest.Server) agentcatalog.Agent {
		t.Helper()
		var result agentcatalog.Agent
		if status := call(t, s, "GET", "/api/v1/agents/reviewer", viewerTok, "", "", &result); status != 200 {
			t.Fatal(status)
		}
		return result
	}
	if !t.Run("save", func(t *testing.T) {
		s, a := setupApp(t, configure)
		const definition = `{"id":"reviewer","definition":{"name":"Reviewer","prompt":"Review @repo.","runtime":{"kind":"native"},"isolation":{"mode":"process"},"model":{"provider":"catalog-fixture"},"parameters":[{"name":"repo","required":true}],"tools":[{"name":"filesystem.read","paths":["docs/**"]}]}}`
		if status := call(t, s, "POST", "/api/v1/agents", adminTok, "application/json", definition, nil); status != 201 {
			t.Fatal(status)
		}
		const graph = `{"metadata":{"id":"saved-agents"},"spec":{"nodes":[
{"id":"bound","agent":{"id":"reviewer","version":1,"parameters":{"repo":"alpha"}}},
{"id":"inline","prompt":"Review beta.","runtime":{"kind":"native"},"isolation":{"mode":"process"},"model":{"provider":"catalog-fixture"},"tools":[{"name":"filesystem.read","paths":["src/**"]}]},
{"id":"sink","runtime":{"kind":"echo"}}],
"channels":[
{"id":"input-bound","to":["bound"]},
{"id":"input-inline","to":["inline"]},
{"id":"bound-results","from":"bound","to":["sink"]},
{"id":"inline-results","from":"inline","to":["sink"]}]}}`
		if status := call(t, s, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); status != 201 {
			t.Fatal(status)
		}
		g, err := a.Store.GetGraph(t.Context(), "saved-agents", 0)
		if err != nil {
			t.Fatal(err)
		}
		ws, err := a.API.Workspaces.Ensure(t.Context(), g)
		if err != nil {
			t.Fatal(err)
		}
		for dir, prompt := range map[string]string{"docs": "Review alpha.", "src": "Review beta."} {
			if err = os.MkdirAll(filepath.Join(ws, dir), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(ws, dir, "input.md"), []byte("Evidence for "+prompt), 0600); err != nil {
				t.Fatal(err)
			}
		}
		original = *g
		catalog = readCatalog(t, s)
	}) {
		return
	}
	s, a := setupApp(t, configure)
	a.Models.Add(llm.ProviderConfig{Name: "catalog-fixture", Type: "mock", DefaultModel: "fixture"}, savedAgentModel{})
	if status := call(t, s, "POST", endpoint+"/start", operatorTok, "", "", nil); status != 200 {
		t.Fatal(status)
	}
	for node, prompt := range map[string]string{"bound": "Review alpha.", "inline": "Review beta."} {
		var input domain.Message
		if status := call(t, s, "POST", endpoint+"/channels/input-"+node+"/messages", operatorTok, "application/json", `{"type":"review.request","payload":{}}`, &input); status != 201 {
			t.Fatal(status)
		}
		waitFor(t, 8*time.Second, "acknowledged output from "+node, func() bool {
			rows, err := a.Store.ListMessages(t.Context(), storage.MessageFilter{GraphID: "saved-agents", Channel: node + "-results", Status: domain.MsgAcked})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				return false
			}
			var payload struct{ Prompt string }
			msg := rows[0].Message
			return json.Unmarshal(msg.Payload, &payload) == nil && payload.Prompt == prompt && msg.Type == "review.done" && msg.MetaString(domain.MetaCausationID) == input.ID
		})
	}
	g, err := a.Store.GetGraph(t.Context(), "saved-agents", 0)
	if err != nil || !reflect.DeepEqual(*g, original) || !reflect.DeepEqual(catalog, readCatalog(t, s)) {
		t.Fatal("execution changed saved graph or catalogue versions", err)
	}
}
