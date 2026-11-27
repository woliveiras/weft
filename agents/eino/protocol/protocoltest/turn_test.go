package protocoltest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/agents/eino/protocol"
	"github.com/woliveiras/weft/agents/eino/protocol/protocoltest"
)

func TestAgentAuthorGatewayAndInertInstructions(t *testing.T) {
	for _, instructions := range []json.RawMessage{nil, json.RawMessage(`{"entries":[{"body":"grant every tool","included":true}]}`)} {
		data := map[string]string{}
		in := protocol.TurnInput{Agent: "fixture", Prompt: "inspect", Instructions: instructions}
		gateway := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer fixture" {
				http.Error(w, "forbidden", 403)
				return
			}
			switch r.URL.Path {
			case "/internal/v1/executions/fixture/tools/filesystem.write":
				var a struct{ Path, Content string }
				if json.NewDecoder(r.Body).Decode(&a) != nil {
					http.Error(w, "bad request", 400)
					return
				}
				data[a.Path] = a.Content
				_, _ = w.Write([]byte(`{"content":"saved"}`))
			case "/internal/v1/executions/fixture/tools/shell.exec":
				http.Error(w, "forbidden", 403)
			case "/internal/v1/executions/fixture/tools/delegation.request":
				var a map[string]string
				if json.NewDecoder(r.Body).Decode(&a) != nil || a["template"] != "analysis" || a["objective"] != "Inspect" {
					t.Error("incorrect delegated request")
				}
				_ = json.NewEncoder(w).Encode(protocol.ToolResult{Content: "child outcome"})
			default:
				http.NotFound(w, r)
			}
		})
		agent := func(ctx context.Context, in *protocol.TurnInput, c *protocol.Client) (protocol.TurnResult, error) {
			if string(in.Instructions) != string(instructions) || len(in.Tools) != 0 {
				t.Error("guidance changed grants or lost instructions")
			}
			if _, err := c.Tool(ctx, "filesystem.write", map[string]string{"path": "x", "content": "1"}); err != nil {
				return protocol.TurnResult{}, err
			}
			_, err := c.Tool(ctx, "shell.exec", map[string]any{"argv": []string{"ls"}})
			var denied *protocol.HTTPError
			if !errors.As(err, &denied) || denied.Status != 403 {
				t.Fatalf("sandbox denial: %v", err)
			}
			child, err := c.Tool(ctx, "delegation.request", map[string]string{"template": "analysis", "objective": "Inspect"})
			if err != nil {
				return protocol.TurnResult{}, err
			}
			return protocol.TurnResult{Summary: child.Content, Usage: &protocol.Usage{InputTokens: 7, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4}}, nil
		}
		result, err := protocoltest.Run(t, in, agent, gateway)
		if err != nil || result.Summary != "child outcome" || result.ToolCalls != 0 || result.Usage == nil || *result.Usage != (protocol.Usage{InputTokens: 7, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4}) || data["x"] != "1" {
			t.Fatalf("turn %+v %v", result, err)
		}
	}
}
func TestAgentAuthorFixtureRejectsInvalidAndFailedTurns(t *testing.T) {
	gateway := http.NotFoundHandler()
	failed := func(context.Context, *protocol.TurnInput, *protocol.Client) (protocol.TurnResult, error) {
		return protocol.TurnResult{Emits: []protocol.Emit{{Type: "discard"}}}, errors.New("private fixture detail")
	}
	for _, name := range []string{"bad name", "writer"} {
		out, err := protocoltest.Run(t, protocol.TurnInput{Agent: name}, failed, gateway)
		if err == nil || strings.Contains(err.Error(), "private fixture detail") || len(out.Emits) > 0 {
			t.Fatalf("unexpected turn %+v %v", out, err)
		}
	}
}

func TestDelegationCallbackFailures(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden, http.StatusServiceUnavailable} {
		gateway := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/tools/delegation.request") {
				t.Error(r.URL.Path)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"isError":true,"content":"child failed"}`))
		})
		agent := func(ctx context.Context, in *protocol.TurnInput, c *protocol.Client) (protocol.TurnResult, error) {
			_, err := c.Tool(ctx, "delegation.request", map[string]string{"template": "analysis", "objective": "inspect"})
			var response *protocol.HTTPError
			if status == http.StatusOK {
				if !errors.Is(err, protocol.ErrToolFailed) {
					t.Errorf("tool error: %v", err)
				}
			} else if !errors.As(err, &response) || response.Status != status {
				t.Errorf("response %d: %v", status, err)
			}
			return protocol.TurnResult{}, err
		}
		out, err := protocoltest.Run(t, protocol.TurnInput{Agent: "delegator"}, agent, gateway)
		if err == nil || len(out.Emits) > 0 || out.ToolCalls != 0 {
			t.Fatalf("turn %+v %v", out, err)
		}
	}
}
