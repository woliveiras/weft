// Package factory implements the software-factory agents under their existing
// Protocol v1 names and message contracts.
package factory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/woliveiras/weft/agents/eino/protocol"
)

type review struct {
	item     map[string]json.RawMessage
	in       *protocol.TurnInput
	client   *protocol.Client
	problems []string
	verdict  string
	comments []string
}

func Reviewer(ctx context.Context) (protocol.Agent, error) {
	chain := compose.NewChain[*review, *review]()
	chain.AppendLambda(compose.InvokableLambda(analyse), compose.WithNodeName("analyse"))
	chain.AppendLambda(compose.InvokableLambda(decide), compose.WithNodeName("decide"))
	graph, err := chain.Compile(ctx)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, in *protocol.TurnInput, client *protocol.Client) (protocol.TurnResult, error) {
		var result protocol.TurnResult
		var reports []string
		for _, message := range in.Inputs {
			if message.Type != "code.ready" {
				continue
			}
			var item map[string]json.RawMessage
			if json.Unmarshal(message.Payload, &item) != nil || item == nil {
				return protocol.TurnResult{}, errors.New("invalid code.ready payload")
			}
			out, err := graph.Invoke(ctx, &review{item: item, in: in, client: client})
			if err != nil {
				return protocol.TurnResult{}, err
			}
			payload := make(map[string]any, len(item)+2)
			for key, value := range item {
				payload[key] = value
			}
			payload["verdict"], payload["comments"] = out.verdict, out.comments
			result.Emits = append(result.Emits, protocol.Emit{Type: "review." + out.verdict, Payload: payload})
			reports = append(reports, stringValue(item["task_id"])+": "+out.verdict)
		}
		result.Summary = strings.Join(reports, "; ")
		return result, nil
	}, nil
}

func stringValue(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return ""
	}
	return text
}

func stringList(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) != nil {
		list = []string{stringValue(raw)}
	}
	var out []string
	for _, v := range list {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

func analyse(ctx context.Context, r *review) (*review, error) {
	paths := stringList(r.item["files"])
	if len(paths) == 0 {
		r.problems = append(r.problems, "no files were changed")
	}
	var listing strings.Builder
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		file, err := r.client.Tool(ctx, "filesystem.read", map[string]string{"path": path})
		var status *protocol.HTTPError
		switch {
		case errors.Is(err, protocol.ErrToolFailed), errors.As(err, &status) && status.Status == http.StatusForbidden:
			r.problems = append(r.problems, path+" is empty or unreadable")
		case err != nil:
			return nil, err
		case strings.TrimSpace(file.Content) == "" || file.Truncated:
			r.problems = append(r.problems, path+" is empty or unreadable")
		case strings.Contains(file.Content, "TODO"):
			r.problems = append(r.problems, path+" contains TODO")
		}
		snippet := []rune(file.Content)
		fmt.Fprintf(&listing, "\n--- %s\n%s", path, string(snippet[:min(len(snippet), 4000)]))
	}
	request := fmt.Sprintf("%s\nReview the change for task %s: %s.\n%s\n%s\nReturn a JSON object with verdict approved or changes_requested, and a comments array.", r.in.Prompt, stringValue(r.item["task_id"]), stringValue(r.item["title"]), stringValue(r.item["description"]), listing.String())
	resp, err := r.client.Chat(ctx, protocol.ChatRequest{System: r.in.System, JSON: true, Messages: []protocol.ChatMessage{{Role: "user", Content: request}}})
	if err != nil {
		return nil, err
	}
	if resp.Message.Role != "assistant" || len(resp.Message.ToolCalls) > 0 || (resp.StopReason != "stop" && resp.StopReason != "") {
		return nil, errors.New("incomplete model review")
	}
	var verdict struct {
		Verdict  string
		Comments json.RawMessage
	}
	start := strings.IndexByte(resp.Message.Content, '{')
	if start >= 0 && json.NewDecoder(strings.NewReader(resp.Message.Content[start:])).Decode(&verdict) == nil && (verdict.Verdict == "approved" || verdict.Verdict == "changes_requested") {
		r.verdict = verdict.Verdict
		r.comments = stringList(verdict.Comments)
	}
	return r, nil
}

func decide(_ context.Context, r *review) (*review, error) {
	switch {
	case len(r.problems) > 0:
		r.verdict = "changes_requested"
		r.comments = append(r.comments, r.problems...)
	case r.verdict == "":
		r.verdict = "approved"
		r.comments = []string{"heuristic review: files present, non-empty, no TODOs"}
	}
	if r.comments == nil {
		r.comments = []string{}
	}
	return r, nil
}
