package trigger_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/woliveiras/weft/server/internal/tools"
)

// tracker is an in-memory GitHub or Jira project behind MCP tool names.
type tracker struct {
	pageSearch          bool
	editOnAssignment    bool
	transientComments   int
	loseCommentResponse bool
	rest                []string
	status              map[string]int
	replies             map[string][]map[string]any
	labelEvents         map[string][]map[string]any
	roles               map[string]string
	mu                  sync.Mutex
	conn                string
	issues              []map[string]any // GitHub: search items; Jira: issues
	calls               []string         // tool names in call order
	args                []map[string]any
	comments            map[string][]string
	transitions         []map[string]any // Jira transitions offered
	fail                map[string]bool  // tool short name → answer an error
}

func newTracker(conn string) *tracker {
	return &tracker{conn: conn, status: map[string]int{}, comments: map[string][]string{}, fail: map[string]bool{}, replies: map[string][]map[string]any{}, labelEvents: map[string][]map[string]any{}, roles: map[string]string{}}
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
		if t.pageSearch {
			page := int(args["page"].(float64))
			size := int(args["perPage"].(float64))
			start := min((page-1)*size, len(t.issues))
			return ok(map[string]any{"items": t.issues[start:min(start+size, len(t.issues))], "total_count": len(t.issues)})
		}
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
	return map[string]any{"number": n, "title": title, "body": "Body of " + title, "html_url": fmt.Sprintf("https://github.example/acme/app/issues/%d", n), "state": "open", "user": map[string]string{"login": "author", "type": "User"}, "labels": ls, "assignees": []any{}}
}

func jiraIssue(key, title string, labels ...string) map[string]any {
	return map[string]any{"key": key, "fields": map[string]any{
		"summary": title, "description": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Details of " + title}}}}},
		"labels": labels, "status": map[string]any{"name": "To Do", "statusCategory": map[string]any{"key": "new"}},
	}}
}

func (t *tracker) request(_ context.Context, connection, kind string, _ int64, method, route string, body any) (json.RawMessage, int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	u, err := url.Parse(route)
	if err != nil || connection != t.conn {
		return nil, 400, fmt.Errorf("unexpected tracker scope")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	index := 4
	if kind == "jira" {
		index = 7
	}
	t.rest = append(t.rest, method+" "+u.Path)
	if status := t.status[method+" "+u.Path]; status != 0 {
		return nil, status, fmt.Errorf("tracker fixture returned HTTP %d", status)
	}
	if t.fail[method+" "+u.Path] {
		return nil, 403, fmt.Errorf("tracker fixture refused operation")
	}
	if u.Path == "/user" {
		return rawFixture(map[string]any{"id": 700, "login": "weft-service"})
	}
	if strings.HasSuffix(u.Path, "/myself") {
		return rawFixture(map[string]string{"accountId": "jira-service"})
	}
	if kind == "github" && strings.Contains(u.Path, "/collaborators/") {
		return rawFixture(map[string]string{"role_name": t.roles[parts[4]]})
	}
	if kind == "jira" && strings.HasSuffix(u.Path, "/permissions/check") {
		return rawFixture(map[string]any{"projectPermissions": []any{map[string]any{"permission": "ADMINISTER_PROJECTS", "projects": []int{101}}}})
	}
	if len(parts) <= index {
		return nil, 404, fmt.Errorf("invalid fixture issue route %q", u.Path)
	}
	key := parts[index]
	if kind == "github" {
		key = "#" + key
	}
	i := t.find(key)
	if i == nil {
		return nil, 404, fmt.Errorf("fixture issue not found")
	}
	segment := ""
	if len(parts) > index+1 {
		segment = parts[index+1]
	}
	if method == "GET" {
		switch segment {
		case "":
			return rawFixture(i)
		case "comments":
			return rawFixture(nonNil(t.replies[key]))
		case "comment":
			return rawFixture(map[string]any{"comments": nonNil(t.replies[key]), "total": len(t.replies[key])})
		case "events":
			return rawFixture(nonNil(t.labelEvents[key]))
		case "changelog":
			return rawFixture(map[string]any{"total": len(t.labelEvents[key]), "values": nonNil(t.labelEvents[key])})
		case "labels":
			return rawFixture(i["labels"])
		}
	}
	if method == "POST" && (segment == "comments" || segment == "comment") {
		if t.transientComments > 0 {
			t.transientComments--
			return nil, 503, fmt.Errorf("tracker temporarily unavailable")
		}
		if kind == "github" {
			t.comments[key] = append(t.comments[key], body.(map[string]string)["body"])
		} else {
			doc := body.(map[string]any)["body"].(map[string]any)
			lines := []string{}
			for _, node := range doc["content"].([]any) {
				if words, ok := node.(map[string]any)["content"].([]any); ok {
					lines = append(lines, words[0].(map[string]string)["text"])
				}
			}
			t.comments[key] = append(t.comments[key], strings.Join(lines, "\n"))
		}
		if t.loseCommentResponse {
			return nil, 0, fmt.Errorf("tracker fixture lost the response")
		}
		if kind == "jira" {
			return rawFixture(map[string]any{"id": fmt.Sprint(len(t.comments[key])), "author": map[string]string{"accountId": "jira-service"}})
		}
		return rawFixture(map[string]any{"id": len(t.comments[key]), "user": map[string]int{"id": 700}})
	}
	if kind == "github" {
		labels := i["labels"].([]map[string]any)
		switch segment {
		case "labels":
			if method == "POST" {
				for _, label := range body.(map[string]any)["labels"].([]string) {
					if !slices.ContainsFunc(labels, func(l map[string]any) bool { return l["name"] == label }) {
						labels = append(labels, map[string]any{"name": label})
					}
				}
			} else if method == "DELETE" {
				labels = slices.DeleteFunc(labels, func(l map[string]any) bool { return l["name"] == parts[index+2] })
			}
			i["labels"] = labels
			return rawFixture(labels)
		case "assignees":
			owner := body.(map[string]any)["assignees"].([]string)[0]
			if method == "DELETE" {
				i["assignees"] = slices.DeleteFunc(i["assignees"].([]map[string]any), func(a map[string]any) bool { return a["login"] == owner })
			} else {
				i["assignees"] = []map[string]any{{"login": owner}}
				if t.editOnAssignment {
					i["body"] = "Edited during assignment."
				}
			}
			return rawFixture(i)
		}
	}
	if kind == "jira" && method == "PUT" && segment == "" {
		m := body.(map[string]any)
		fields := i["fields"].(map[string]any)
		labels := fields["labels"].([]string)
		for _, delta := range m["update"].(map[string]any)["labels"].([]map[string]string) {
			if add := delta["add"]; add != "" && !slices.Contains(labels, add) {
				labels = append(labels, add)
			}
			if remove := delta["remove"]; remove != "" {
				labels = slices.DeleteFunc(labels, func(l string) bool { return l == remove })
			}
		}
		fields["labels"] = labels
		if update, ok := m["fields"].(map[string]any); ok {
			fields["assignee"] = update["assignee"]
		}
		return nil, 204, nil
	}
	return nil, 400, fmt.Errorf("unhandled tracker operation %s %s", method, u.Path)
}

func rawFixture(value any) (json.RawMessage, int, error) {
	b, err := json.Marshal(value)
	return b, 200, err
}

func nonNil(rows []map[string]any) []map[string]any {
	if rows == nil {
		return []map[string]any{}
	}
	return rows
}
