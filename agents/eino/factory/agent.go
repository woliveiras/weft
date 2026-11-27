package factory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/woliveiras/weft/agents/eino/protocol"
)

type job struct {
	in      *protocol.TurnInput
	client  *protocol.Client
	message protocol.Message
	item    map[string]json.RawMessage
	emits   []protocol.Emit
	summary string
}

type action func(context.Context, *job) error

// Each eligible message completes its graph before the next message starts.
// The service returns the whole batch's emits only after every graph succeeds.
func messageAgent(ctx context.Context, name string, actions map[string]action) (protocol.Agent, error) {
	graph := compose.NewChain[*job, *job]()
	graph.AppendLambda(compose.InvokableLambda(func(ctx context.Context, j *job) (*job, error) { return j, actions[j.message.Type](ctx, j) }), compose.WithNodeName(name))
	runner, err := graph.Compile(ctx)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, in *protocol.TurnInput, client *protocol.Client) (protocol.TurnResult, error) {
		var result protocol.TurnResult
		var summaries []string
		for _, m := range in.Inputs {
			if actions[m.Type] == nil {
				continue
			}
			j := &job{in: in, client: client, message: m}
			if err := json.Unmarshal(m.Payload, &j.item); err != nil || j.item == nil {
				j.item = map[string]json.RawMessage{"text": m.Payload}
			}
			out, err := runner.Invoke(ctx, j)
			if err != nil {
				return protocol.TurnResult{}, err
			}
			result.Emits = append(result.Emits, out.emits...)
			if out.summary != "" {
				summaries = append(summaries, out.summary)
			}
		}
		result.Summary = strings.Join(summaries, "; ")
		return result, nil
	}, nil
}

func (j *job) text(key string) string { return stringValue(j.item[key]) }

func (j *job) emit(kind string, payload any) {
	j.emits = append(j.emits, protocol.Emit{Type: kind, Payload: payload})
}

func (j *job) model(ctx context.Context, prompt string, structured bool) (string, error) {
	resp, err := j.client.Chat(ctx, protocol.ChatRequest{System: j.in.System, JSON: structured, Messages: []protocol.ChatMessage{{Role: "user", Content: j.in.Prompt + "\n\n" + prompt}}})
	if err != nil {
		return "", err
	}
	if resp.Message.Role != "assistant" || len(resp.Message.ToolCalls) != 0 || (resp.StopReason != "" && resp.StopReason != "stop") {
		return "", errors.New("incomplete model response")
	}
	return resp.Message.Content, nil
}

func decodeObject(s string, dst any) bool {
	i := strings.IndexByte(s, '{')
	return i >= 0 && json.NewDecoder(strings.NewReader(s[i:])).Decode(dst) == nil
}

func (j *job) write(ctx context.Context, path, content string) error {
	_, err := j.client.Tool(ctx, "filesystem.write", map[string]string{"path": path, "content": content})
	return err
}

func (j *job) save(ctx context.Context, key string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return j.client.StatePut(ctx, key, raw)
}

func (j *job) load(ctx context.Context, key string, dst any) (bool, error) {
	raw, found, err := j.client.StateGet(ctx, key)
	if err != nil || !found {
		return false, err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return false, fmt.Errorf("decode saved factory state: %w", err)
	}
	return true, nil
}

func first(values ...string) string {
	for _, s := range values {
		if s != "" {
			return s
		}
	}
	return ""
}
