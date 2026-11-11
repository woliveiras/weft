package trigger_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/woliveiras/weft/server/internal/tools"
)

// tracker is an in-memory GitHub or Jira project behind MCP tool names.
type tracker struct {
	mu          sync.Mutex
	conn        string
	issues      []map[string]any // GitHub: search items; Jira: issues
	calls       []string         // tool names in call order
	args        []map[string]any
	comments    map[string][]string
	transitions []map[string]any // Jira transitions offered
	fail        map[string]bool  // tool short name → answer an error
}

func newTracker(conn string) *tracker {
	return &tracker{conn: conn, comments: map[string][]string{}, fail: map[string]bool{}}
}

// toolFn is a tools.Tool backed by the tracker.
type toolFn struct {
	t    *tracker
	name string
}

func (f toolFn) Name() string            { return "mcp." + f.t.conn + "." + f.name }
func (f toolFn) Capability() string      { return f.Name() }
func (f toolFn) Description() string     { return "fixture" }
func (f toolFn) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f toolFn) Invoke(_ context.Context, inv *tools.Invocation) (*tools.Result, error) {
	return f.t.handle(f.name, inv.Args), nil
}

func (t *tracker) tools(names ...string) []tools.Tool {
	out := []tools.Tool{}
	for _, n := range names {
		out = append(out, toolFn{t: t, name: n})
	}
	return out
}

func ok(v any) *tools.Result {
	b, _ := json.Marshal(v)
	return &tools.Result{Content: string(b)}
}

func (t *tracker) find(key string) map[string]any {
	for _, i := range t.issues {
		if k, _ := i["key"].(string); k == key {
			return i
		}
		if n, ok := i["number"].(int); ok && strings.HasSuffix(key, fmt.Sprintf("#%d", n)) {
			return i
		}
	}
	return nil
}

func (t *tracker) handle(name string, raw json.RawMessage) *tools.Result {
	t.mu.Lock()
	defer t.mu.Unlock()
	var args map[string]any
	_ = json.Unmarshal(raw, &args)
	t.calls, t.args = append(t.calls, name), append(t.args, args)
	if t.fail[name] {
		return &tools.Result{Content: name + " failed: 403 forbidden", IsError: true}
	}
	switch name {
	case "search_issues":
		return ok(map[string]any{"items": t.issues})
	case "searchJiraIssuesUsingJql":
		return ok(map[string]any{"issues": t.issues})
	case "update_issue":
		i := t.find(fmt.Sprintf("#%v", args["issue_number"]))
		labels := []map[string]any{}
		for _, l := range args["labels"].([]any) {
			labels = append(labels, map[string]any{"name": l})
		}
		i["labels"] = labels
		if a, ok := args["assignees"].([]any); ok {
			i["assignees"] = []map[string]any{{"login": a[0]}}
		}
		return ok(map[string]any{"ok": true})
	case "editJiraIssue":
		i := t.find(args["issueIdOrKey"].(string))
		f := args["fields"].(map[string]any)
		fields := i["fields"].(map[string]any)
		fields["labels"] = f["labels"]
		if a, ok := f["assignee"]; ok {
			fields["assignee"] = a
		}
		return ok(map[string]any{"ok": true})
	case "add_issue_comment":
		key := fmt.Sprintf("#%v", args["issue_number"])
		t.comments[key] = append(t.comments[key], args["body"].(string))
		return ok(map[string]any{"id": 1})
	case "addCommentToJiraIssue":
		key := args["issueIdOrKey"].(string)
		t.comments[key] = append(t.comments[key], args["commentBody"].(string))
		return ok(map[string]any{"id": "1"})
	case "getTransitionsForJiraIssue":
		return ok(map[string]any{"transitions": t.transitions})
	case "transitionJiraIssue":
		i := t.find(args["issueIdOrKey"].(string))
		id := args["transition"].(map[string]any)["id"]
		for _, tr := range t.transitions {
			if tr["id"] == id {
				i["fields"].(map[string]any)["status"] = map[string]any{"name": tr["name"], "statusCategory": map[string]any{"key": "indeterminate"}}
			}
		}
		return ok(map[string]any{})
	}
	return &tools.Result{Content: "unknown tool " + name, IsError: true}
}

func ghIssue(n int, title string, labels ...string) map[string]any {
	ls := []map[string]any{}
	for _, l := range labels {
		ls = append(ls, map[string]any{"name": l})
	}
	return map[string]any{"number": n, "title": title, "body": "Body of " + title, "html_url": fmt.Sprintf("https://github.example/acme/app/issues/%d", n), "state": "open", "labels": ls, "assignees": []any{}}
}

func jiraIssue(key, title string, labels ...string) map[string]any {
	return map[string]any{"key": key, "fields": map[string]any{
		"summary": title, "description": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Details of " + title}}}}},
		"labels": labels, "status": map[string]any{"name": "To Do", "statusCategory": map[string]any{"key": "new"}},
	}}
}
