package conformance

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/woliveiras/weft/server/internal/llm"
)

func str(v any) string { s, _ := v.(string); return s }

func list(v any) []any { l, _ := v.([]any); return l }

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }

// jsonInstruction recognises JSON mode expressed as a system instruction.
func jsonInstruction(system string) bool { return strings.Contains(system, "JSON") }

func args(tc *llm.ToolCall) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(tc.Arguments, &m)
	return m
}

// OpenAI is the Chat Completions protocol (openai, vllm, lmstudio,
// openrouter, huggingface/tgi).
var OpenAI = Wire{
	Name: "openai", ChatPath: "/chat/completions", ModelsPath: "/models", IDs: true, CacheRead: true,
	Decode: func(r *http.Request, b map[string]any) Observed {
		o := Observed{Credential: r.Header.Get("Authorization"), Model: str(b["model"])}
		for _, m := range list(b["messages"]) {
			m := obj(m)
			switch role := str(m["role"]); role {
			case "system":
				o.System += str(m["content"])
			case "tool":
				o.Roles = append(o.Roles, role)
				o.ToolResult, o.ToolCallID = str(m["content"]), str(m["tool_call_id"])
			default:
				o.Roles = append(o.Roles, role)
			}
		}
		for _, t := range list(b["tools"]) {
			o.ToolNames = append(o.ToolNames, str(obj(obj(t)["function"])["name"]))
		}
		o.JSON = str(obj(b["response_format"])["type"]) == "json_object" || jsonInstruction(o.System)
		return o
	},
	EncodeChat: func(a Answer) any {
		msg := map[string]any{"role": "assistant", "content": a.Text}
		finish := "stop"
		if a.ToolCall != nil {
			msg["tool_calls"] = []any{map[string]any{"id": a.ToolCall.ID, "type": "function",
				"function": map[string]any{"name": llm.WireToolName(a.ToolCall.Name), "arguments": string(a.ToolCall.Arguments)}}}
			finish = "tool_calls"
		}
		return map[string]any{"model": "conf-small", "choices": []any{map[string]any{"message": msg, "finish_reason": finish}},
			"usage": map[string]any{"prompt_tokens": a.Usage.InputTokens, "completion_tokens": a.Usage.OutputTokens,
				"prompt_tokens_details": map[string]any{"cached_tokens": a.Usage.CacheReadTokens}}}
	},
	EncodeModels: func(ids []string) any {
		var data []any
		for _, id := range ids {
			data = append(data, map[string]any{"id": id})
		}
		return map[string]any{"data": data}
	},
}

// Anthropic is the Messages protocol.
var Anthropic = Wire{
	Name: "anthropic", ChatPath: "/v1/messages", ModelsPath: "/v1/models", IDs: true, CacheRead: true, CacheWrite: true,
	Decode: func(r *http.Request, b map[string]any) Observed {
		o := Observed{Credential: r.Header.Get("x-api-key"), Model: str(b["model"]), System: str(b["system"])}
		for _, m := range list(b["messages"]) {
			m := obj(m)
			role := str(m["role"])
			for _, blk := range list(m["content"]) {
				if blk := obj(blk); str(blk["type"]) == "tool_result" {
					role = "tool"
					o.ToolCallID = str(blk["tool_use_id"])
					if s, ok := blk["content"].(string); ok {
						o.ToolResult = s
					}
					for _, c := range list(blk["content"]) {
						o.ToolResult += str(obj(c)["text"])
					}
				}
			}
			o.Roles = append(o.Roles, role)
		}
		for _, t := range list(b["tools"]) {
			o.ToolNames = append(o.ToolNames, str(obj(t)["name"]))
		}
		o.JSON = jsonInstruction(o.System)
		return o
	},
	EncodeChat: func(a Answer) any {
		content := []any{}
		stop := "end_turn"
		if a.Text != "" {
			content = append(content, map[string]any{"type": "text", "text": a.Text})
		}
		if a.ToolCall != nil {
			content = append(content, map[string]any{"type": "tool_use", "id": a.ToolCall.ID, "name": llm.WireToolName(a.ToolCall.Name), "input": args(a.ToolCall)})
			stop = "tool_use"
		}
		return map[string]any{"model": "conf-small", "stop_reason": stop, "content": content,
			// input_tokens excludes the cached parts on this protocol.
			"usage": map[string]any{"input_tokens": a.Usage.InputTokens - a.Usage.CacheReadTokens - a.Usage.CacheWriteTokens, "output_tokens": a.Usage.OutputTokens,
				"cache_read_input_tokens": a.Usage.CacheReadTokens, "cache_creation_input_tokens": a.Usage.CacheWriteTokens}}
	},
	EncodeModels: OpenAI.EncodeModels,
}

// Ollama is Ollama's native /api/chat protocol. It has no credential and
// identifies tool results by tool name instead of call id.
var Ollama = Wire{
	Name: "ollama", Keyless: true, ChatPath: "/api/chat", ModelsPath: "/api/tags",
	Decode: func(r *http.Request, b map[string]any) Observed {
		o := Observed{Credential: r.Header.Get("Authorization"), Model: str(b["model"])}
		for _, m := range list(b["messages"]) {
			m := obj(m)
			switch role := str(m["role"]); role {
			case "system":
				o.System += str(m["content"])
			case "tool":
				o.Roles = append(o.Roles, role)
				o.ToolResult = str(m["content"])
			default:
				o.Roles = append(o.Roles, role)
			}
		}
		for _, t := range list(b["tools"]) {
			o.ToolNames = append(o.ToolNames, str(obj(obj(t)["function"])["name"]))
		}
		o.JSON = str(b["format"]) == "json"
		return o
	},
	EncodeChat: func(a Answer) any {
		msg := map[string]any{"role": "assistant", "content": a.Text}
		if a.ToolCall != nil {
			msg["tool_calls"] = []any{map[string]any{"function": map[string]any{"name": llm.WireToolName(a.ToolCall.Name), "arguments": args(a.ToolCall)}}}
		}
		return map[string]any{"model": "conf-small", "message": msg, "done_reason": "stop",
			"prompt_eval_count": a.Usage.InputTokens, "eval_count": a.Usage.OutputTokens}
	},
	EncodeModels: func(ids []string) any {
		var models []any
		for _, id := range ids {
			models = append(models, map[string]any{"name": id})
		}
		return map[string]any{"models": models}
	},
}
