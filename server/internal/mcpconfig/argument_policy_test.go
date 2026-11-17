package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

func restrictedRead() Tool {
	return Tool{Name: "read", InputSchema: json.RawMessage(`{"type":"object","properties":{"workspace":{"type":"string"},"repo":{"type":"string"}}}`)}
}
func TestArgumentPolicyValidationAndMatching(t *testing.T) {
	good := ArgumentRule{Tool: "read", Argument: "repo", Values: []string{"one", "two"}}
	if validateArgumentPolicy([]ArgumentRule{good}) != nil {
		t.Fatal("valid rule rejected")
	}
	for _, rules := range [][]ArgumentRule{
		make([]ArgumentRule, 65), {good, good}, {{Tool: "*", Argument: "repo", Values: []string{"one"}}},
		{{Tool: "read", Argument: "nested.repo", Values: []string{"one"}}}, {{Tool: "read", Argument: "repo"}},
		{{Tool: "read", Argument: "repo", Values: []string{"one", "one"}}}, {{Tool: "read", Argument: "repo", Values: []string{" one"}}},
		{{Tool: "read", Argument: "repo", Values: []string{"one\n"}}},
	} {
		if validateArgumentPolicy(rules) == nil {
			t.Fatal("invalid rules accepted")
		}
	}
	rules := []ArgumentRule{good, {Tool: "read", Argument: "workspace", Values: []string{"team"}}}
	for _, tc := range []struct {
		args string
		ok   bool
	}{
		{`{"workspace":"team","repo":"one"}`, true}, {`{"workspace":"team","repo":"two"}`, true},
		{`{"workspace":"other","repo":"one"}`, false}, {`{"workspace":"team","repo":"ONE"}`, false},
		{`{"workspace":"team","repo":null}`, false}, {`{"workspace":"team","repo":["one"]}`, false},
		{`{"workspace":"team"}`, false}, {`{"workspace":"team","repo":"../one"}`, false},
	} {
		var args map[string]json.RawMessage
		json.Unmarshal([]byte(tc.args), &args)
		if argumentPolicyAllows(rules, restrictedRead(), args) != tc.ok {
			t.Fatal(tc.args)
		}
	}
	for _, tool := range []Tool{{Name: "write", InputSchema: restrictedRead().InputSchema}, {Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}, {Name: "read", InputSchema: json.RawMessage(`{"properties":{"repo":{"type":"number"}}}`)}} {
		if argumentPolicyTool(rules, tool) {
			t.Fatal("schema or tool bypass")
		}
	}
}
func TestArgumentPolicyGatewayPersistenceAndChanges(t *testing.T) {
	m := testManager(t)
	calls := 0
	m.Discover = func(context.Context, string, string) ([]Tool, error) {
		return []Tool{restrictedRead(), {Name: "write", InputSchema: restrictedRead().InputSchema}}, nil
	}
	m.Call = func(_ context.Context, _, _, _ string, args json.RawMessage) (json.RawMessage, error) {
		calls++
		var obj map[string]string
		if json.Unmarshal(args, &obj) != nil || obj["workspace"] != "team" || obj["repo"] != "one" {
			t.Error("unreviewed argument dispatched")
		}
		// The wire must not retain duplicate keys whose interpretation could differ.
		if string(args) != `{"repo":"one","workspace":"team"}` {
			t.Error("arguments not normalized", string(args))
		}
		return json.RawMessage(`{"content":[{"type":"text","text":"read"}]}`), nil
	}
	in := Input{ID: "scoped", Name: "Scoped", Service: "generic", Endpoint: "https://example.com/mcp", Enabled: true, ArgumentPolicy: []ArgumentRule{{Tool: "read", Argument: "workspace", Values: []string{"team"}}, {Tool: "read", Argument: "repo", Values: []string{"one"}}}}
	if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	view, err := m.Refresh(t.Context(), in.ID, "admin")
	if err != nil || len(view.Tools) != 1 {
		t.Fatal(view, err)
	}
	stored, err := m.Get(t.Context(), in.ID)
	if err != nil || len(stored.ArgumentPolicy) != 2 {
		t.Fatal(stored, err)
	}
	gateway := tools.NewGateway(tools.NewRegistry(), nil, nil)
	gateway.ResolveTool = m.Resolve
	p := &tools.Principal{GraphID: "test", NodeID: "reader", Grants: []domain.ToolGrant{{Name: "mcp.scoped.read"}, {Name: "mcp.scoped.write"}}}
	for _, args := range []string{`{}`, `{"workspace":"other","repo":"one"}`, `{"workspace":"team","repo":"two"}`} {
		if _, err := gateway.Invoke(t.Context(), p, "mcp.scoped.read", json.RawMessage(args)); !errors.Is(err, tools.ErrDenied) {
			t.Fatal("scope bypass", err)
		}
	}
	if _, err := gateway.Invoke(t.Context(), p, "mcp.scoped.write", json.RawMessage(`{}`)); !errors.Is(err, tools.ErrDenied) {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("denial dispatched")
	}
	valid := json.RawMessage(`{"workspace":"other","workspace":"team","repo":"one"}`)
	if _, err := gateway.Invoke(t.Context(), p, "mcp.scoped.read", valid); err != nil {
		t.Fatal(err)
	}
	stale, _ := m.Resolve(t.Context(), "mcp.scoped.read")
	in.ArgumentPolicy[1].Values = []string{"two"}
	saved, err := m.Put(t.Context(), in, view.Revision, "admin")
	if err != nil || len(saved.Tools) != 0 {
		t.Fatal(saved, err)
	}
	if _, err := stale.Invoke(t.Context(), &tools.Invocation{Principal: p, Args: valid}); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("stale grant used", err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	m.Discover = func(context.Context, string, string) ([]Tool, error) { return nil, errors.New("fixture unavailable") }
	failed, err := m.Refresh(t.Context(), in.ID, "admin")
	if err != nil || failed.Status != "unavailable" || len(failed.Tools) != 0 {
		t.Fatal("discovery failure hidden", failed, err)
	}
	if _, err := gateway.Invoke(t.Context(), p, "mcp.scoped.read", valid); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("failed discovery retained authority", err)
	}
}

func TestArgumentCombinations(t *testing.T) {
	good := ArgumentRule{Tool: "read", Arguments: []string{"workspace", "repo"}, Combinations: [][]string{{"team-a", "one"}, {"team-b", "two"}}}
	if err := validateArgumentPolicy([]ArgumentRule{good}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args    string
		allowed bool
	}{
		{`{"workspace":"team-a","repo":"one"}`, true}, {`{"workspace":"team-b","repo":"two"}`, true},
		{`{"workspace":"team-a","repo":"two"}`, false}, {`{"workspace":"team-b","repo":"one"}`, false},
		{`{"workspace":"team-a"}`, false}, {`{"workspace":null,"repo":"one"}`, false},
		{`{"workspace":["team-a"],"repo":"one"}`, false}, {`{"workspace":"TEAM-A","repo":"one"}`, false},
	} {
		var args map[string]json.RawMessage
		if err := json.Unmarshal([]byte(tc.args), &args); err != nil {
			t.Fatal(err)
		}
		if argumentPolicyAllows([]ArgumentRule{good}, restrictedRead(), args) != tc.allowed {
			t.Fatal(tc.args)
		}
	}
	for _, mutate := range []func(*ArgumentRule){
		func(r *ArgumentRule) { r.Argument = "repo" }, func(r *ArgumentRule) { r.Values = []string{"one"} },
		func(r *ArgumentRule) { r.Arguments = []string{"repo"} }, func(r *ArgumentRule) { r.Arguments = []string{"repo", "repo"} },
		func(r *ArgumentRule) { r.Arguments = []string{"workspace", "nested.repo"} },
		func(r *ArgumentRule) { r.Combinations = nil }, func(r *ArgumentRule) { r.Combinations = [][]string{{"a"}} },
		func(r *ArgumentRule) { r.Combinations = [][]string{{"a", "b"}, {"a", "b"}} },
		func(r *ArgumentRule) { r.Combinations = [][]string{{"a", " "}} },
		func(r *ArgumentRule) { r.Combinations = [][]string{{"a", "b\n"}} },
		func(r *ArgumentRule) { r.Combinations = make([][]string, 33) },
		func(r *ArgumentRule) { r.Arguments = make([]string, 9) },
	} {
		bad := good
		mutate(&bad)
		if validateArgumentPolicy([]ArgumentRule{bad}) == nil {
			t.Fatal("invalid combination accepted", bad)
		}
	}
	if validateArgumentPolicy([]ArgumentRule{good, good}) == nil {
		t.Fatal("duplicate tuple rule")
	}
	for _, schema := range []string{`{"properties":{"workspace":{"type":"string"}}}`, `{"properties":{"workspace":{"type":"string"},"repo":{"type":"number"}}}`} {
		if argumentPolicyTool([]ArgumentRule{good}, Tool{Name: "read", InputSchema: json.RawMessage(schema)}) {
			t.Fatal("schema bypass")
		}
	}
	var args map[string]json.RawMessage
	json.Unmarshal([]byte(`{"workspace":"team-b","repo":"two"}`), &args)
	if argumentPolicyAllows([]ArgumentRule{good, {Tool: "read", Argument: "repo", Values: []string{"one"}}}, restrictedRead(), args) {
		t.Fatal("independent constraint bypassed")
	}
}

func TestArgumentCombinationsGatewayPersistence(t *testing.T) {
	m := testManager(t)
	m.Discover = func(context.Context, string, string) ([]Tool, error) { return []Tool{restrictedRead()}, nil }
	calls := 0
	m.Call = func(context.Context, string, string, string, json.RawMessage) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"content":[]}`), nil
	}
	rule := ArgumentRule{Tool: "read", Arguments: []string{"workspace", "repo"}, Combinations: [][]string{{"team-a", "one"}, {"team-b", "two"}}}
	in := Input{ID: "tuple", Name: "Tuple", Service: "generic", Endpoint: "https://example.com/mcp", Enabled: true, ArgumentPolicy: []ArgumentRule{rule}}
	if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	view, err := m.Refresh(t.Context(), in.ID, "admin")
	if err != nil || len(view.Tools) != 1 {
		t.Fatal(view, err)
	}
	stored, err := m.Get(t.Context(), in.ID)
	if err != nil || len(stored.ArgumentPolicy[0].Combinations) != 2 {
		t.Fatal(stored, err)
	}
	gateway := tools.NewGateway(tools.NewRegistry(), nil, nil)
	gateway.ResolveTool = m.Resolve
	p := &tools.Principal{GraphID: "test", NodeID: "reader", Grants: []domain.ToolGrant{{Name: "mcp.tuple.read"}}}
	valid := json.RawMessage(`{"workspace":"team-a","repo":"one"}`)
	for _, args := range []string{`{"workspace":"team-a","repo":"two"}`, `{"workspace":"team-b","repo":"one"}`, `{"repo":"one"}`} {
		if _, err := gateway.Invoke(t.Context(), p, "mcp.tuple.read", json.RawMessage(args)); !errors.Is(err, tools.ErrDenied) {
			t.Fatal("cross tuple dispatch", err)
		}
	}
	if calls != 0 {
		t.Fatal("denied calls reached remote")
	}
	if _, err := gateway.Invoke(t.Context(), p, "mcp.tuple.read", valid); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.Invoke(t.Context(), &tools.Principal{GraphID: "test", NodeID: "ungranted"}, "mcp.tuple.read", valid); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("missing grant accepted", err)
	}
	stale, err := m.Resolve(t.Context(), "mcp.tuple.read")
	if err != nil {
		t.Fatal(err)
	}
	in.ArgumentPolicy[0].Combinations = [][]string{{"team-b", "two"}}
	saved, err := m.Put(t.Context(), in, view.Revision, "admin")
	if err != nil || len(saved.Tools) != 0 {
		t.Fatal(saved, err)
	}
	if _, err := stale.Invoke(t.Context(), &tools.Invocation{Principal: p, Args: valid}); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("stale tuple authority", err)
	}
	if _, err := m.Refresh(t.Context(), in.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.Invoke(t.Context(), p, "mcp.tuple.read", valid); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("removed tuple accepted", err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	m.Discover = func(context.Context, string, string) ([]Tool, error) { return nil, errors.New("fixture unavailable") }
	failed, err := m.Refresh(t.Context(), in.ID, "admin")
	if err != nil || failed.Status != "unavailable" {
		t.Fatal(failed, err)
	}
	if _, err := gateway.Invoke(t.Context(), p, "mcp.tuple.read", json.RawMessage(`{"workspace":"team-b","repo":"two"}`)); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("unavailable catalogue accepted", err)
	}
}
