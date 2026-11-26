package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

var useEvalCodex = flag.Bool("chat-eval-codex", false, "use the local Codex CLI with ChatGPT sign-in for Chat evals")
var evalCodexModel = flag.String("chat-eval-model", "", "optional model for the signed-in Codex CLI")

// Codex supplies the assistant decision. Only RunTurn and the fixture's real
// domain services execute the suggested Weft tools. No provider is configured
// in the developer's running Weft installation.
const codexEvalSchema = `{"type":"object","additionalProperties":false,"required":["text","calls"],"properties":{"text":{"type":"string"},"calls":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["name","arguments"],"properties":{"name":{"type":"string"},"arguments":{"type":"string"}}}}}}`

func codexEvalEnv() []string {
	// Let the CLI use its normal login/keychain, without inheriting API key,
	// provider overrides, project secrets or active desktop thread settings.
	var env []string
	for _, key := range []string{"HOME", "CODEX_HOME", "PATH", "TMPDIR", "USER", "LOGNAME", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func useCodexForEval(t *testing.T, e *chatEval) {
	t.Helper()
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal("codex executable is required")
	}
	dir := t.TempDir()
	login := exec.CommandContext(t.Context(), binary, "login", "status")
	login.Dir, login.Env = dir, codexEvalEnv()
	status, err := login.CombinedOutput()
	if err != nil || !strings.Contains(string(status), "Logged in using ChatGPT") {
		t.Fatal("Codex must be signed in with ChatGPT for subscription evals; check codex login status")
	}
	schema := filepath.Join(dir, "response.schema.json")
	if err := os.WriteFile(schema, []byte(codexEvalSchema), 0600); err != nil {
		t.Fatal(err)
	}
	e.model = *evalCodexModel
	if e.model == "" {
		e.model = "codex-cli-default"
	}
	e.client.chat = func(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
		wire, err := json.Marshal(struct {
			System   string            `json:"system"`
			Messages []llm.ChatMessage `json:"messages"`
			Tools    []llm.ToolSchema  `json:"tools"`
		}{req.System, req.Messages, req.Tools})
		if err != nil {
			return nil, err
		}
		work, err := os.MkdirTemp(dir, "turn-")
		if err != nil {
			return nil, err
		}
		output := filepath.Join(work, "response.json")
		args := []string{
			"exec", "--ephemeral", "--skip-git-repo-check", "--ignore-user-config", "--sandbox", "read-only",
			"--disable", "shell_tool", "--disable", "multi_agent", "--disable", "plugins", "--disable", "hooks",
			"-c", `web_search="disabled"`, "-c", `forced_login_method="chatgpt"`, "-c", `model_reasoning_effort="low"`,
			"--output-schema", schema, "--output-last-message", output,
		}
		if *evalCodexModel != "" {
			args = append(args, "-m", *evalCodexModel)
		}
		cmd := exec.CommandContext(ctx, binary, append(args, "-")...)
		cmd.Dir, cmd.Env = work, codexEvalEnv()
		cmd.Stdin = io.MultiReader(strings.NewReader("Produce the next assistant turn for the supplied Weft Chat request. Follow its system instructions. Represent each proposed Weft tool call with its name and JSON-encoded arguments string. For a final answer use text and an empty calls array. The Weft server executes calls and supplies observations. Use only the information supplied here.\n"), bytes.NewReader(wire))
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("codex exec failed (%w); check CLI version, ChatGPT login, selected model and plan availability", err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			return nil, err
		}
		return decodeCodexEval(data)
	}
}

func decodeCodexEval(data []byte) (*llm.ChatResponse, error) {
	var decision struct {
		Text  string                             `json:"text"`
		Calls []struct{ Name, Arguments string } `json:"calls"`
	}
	if err := json.Unmarshal(data, &decision); err != nil {
		return nil, err
	}
	if len(decision.Calls) == 0 && strings.TrimSpace(decision.Text) == "" {
		return nil, errors.New("empty Codex eval decision")
	}
	r := llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: decision.Text}, StopReason: "stop"}
	for _, c := range decision.Calls {
		if c.Name == "" || !json.Valid([]byte(c.Arguments)) {
			return nil, errors.New("invalid Codex eval tool call")
		}
		r.Message.ToolCalls = append(r.Message.ToolCalls, llm.ToolCall{ID: domain.NewID("evalcall"), Name: c.Name, Arguments: json.RawMessage(c.Arguments)})
		r.StopReason = "tool_calls"
	}
	return &r, nil
}

func TestCodexEvalDecisions(t *testing.T) {
	r, err := decodeCodexEval([]byte(`{"text":"","calls":[{"name":"query_tasks","arguments":"{\"mode\":\"list\"}"},{"name":"query_tasks","arguments":"{\"mode\":\"count\"}"}]}`))
	if err != nil || len(r.Message.ToolCalls) != 2 || r.Message.ToolCalls[0].ID == r.Message.ToolCalls[1].ID || string(r.Message.ToolCalls[0].Arguments) != `{"mode":"list"}` {
		t.Fatalf("tool calls: %+v %v", r, err)
	}
	r, err = decodeCodexEval([]byte(`{"text":"Há duas tarefas.","calls":[]}`))
	if err != nil || r.StopReason != "stop" || r.Message.Content != "Há duas tarefas." {
		t.Fatal(r, err)
	}
	for _, input := range []string{`not JSON`, `{}`, `{"calls":[{"name":"query_tasks","arguments":"oops"}]}`} {
		if _, err := decodeCodexEval([]byte(input)); err == nil {
			t.Errorf("accepted invalid decision %s", input)
		}
	}
}
