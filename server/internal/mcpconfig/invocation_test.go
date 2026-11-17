package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

type approvalFunc func(context.Context, *domain.Approval) (*domain.Approval, error)

func (f approvalFunc) RequestApproval(c context.Context, a *domain.Approval) (*domain.Approval, error) {
	return f(c, a)
}

type auditLog struct{ events []*domain.Event }

func (a *auditLog) Audit(_ context.Context, e *domain.Event) { a.events = append(a.events, e) }
func readyManager(t *testing.T) (*Manager, *tools.Gateway, *tools.Principal, *auditLog) {
	m := testManager(t)
	in := input()
	in.Credential = &Credential{Value: "unit-secret"}
	if _, e := m.Put(t.Context(), in, 0, "admin"); e != nil {
		t.Fatal(e)
	}
	m.Discover = func(context.Context, string, string) ([]Tool, error) {
		return []Tool{{Name: "search", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
	}
	if _, e := m.Refresh(t.Context(), in.ID, "admin"); e != nil {
		t.Fatal(e)
	}
	audit := &auditLog{}
	gw := tools.NewGateway(tools.NewRegistry(), nil, audit)
	gw.ResolveTool = m.Resolve
	return m, gw, &tools.Principal{GraphID: "g", NodeID: "worker", ExecutionID: "exec", Grants: []domain.ToolGrant{{Name: "mcp.team.search"}}}, audit
}
func TestInvocationAuthorizationAndAuditing(t *testing.T) {
	m, gw, p, a := readyManager(t)
	calls := 0
	m.Call = func(_ context.Context, endpoint, token, name string, args json.RawMessage) (json.RawMessage, error) {
		calls++
		if endpoint != input().Endpoint || token != "unit-secret" || name != "search" {
			t.Fatal("routing")
		}
		return json.RawMessage(`{"content":[{"type":"text","text":"found"}],"structuredContent":{"count":1}}`), nil
	}
	for _, g := range []string{"", "*", "mcp.*", "mcp.team.*", "mcp.other.search"} {
		p.Grants = []domain.ToolGrant{{Name: g}}
		if _, e := gw.Invoke(t.Context(), p, "mcp.team.search", nil); !errors.Is(e, tools.ErrDenied) {
			t.Fatalf("%s: %v", g, e)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
	p.Grants = []domain.ToolGrant{{Name: "mcp.team.search", RateLimit: 1}}
	r, e := gw.Invoke(t.Context(), p, "mcp.team.search", json.RawMessage(`{"query":"private query"}`))
	if e != nil || r.IsError || !strings.Contains(r.Content, "found") {
		t.Fatal(r, e)
	}
	if _, e = gw.Invoke(t.Context(), p, "mcp.team.search", nil); !errors.Is(e, tools.ErrRateLimited) {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	b, _ := json.Marshal(a.events)
	if strings.Contains(string(b), "private query") || strings.Contains(string(b), "unit-secret") {
		t.Fatal("audit payload leak")
	}
	if len(a.events) == 0 || a.events[0].NodeID != "worker" || a.events[0].ExecutionID != "exec" {
		t.Fatal("missing attribution")
	}
}
func TestInvocationRechecksAfterApproval(t *testing.T) {
	for _, mode := range []string{"disabled", "refresh", "rejected", "approved"} {
		t.Run(mode, func(t *testing.T) {
			m, gw, p, _ := readyManager(t)
			calls := 0
			m.Call = func(context.Context, string, string, string, json.RawMessage) (json.RawMessage, error) {
				calls++
				return json.RawMessage(`{"content":[]}`), nil
			}
			p.Grants[0].RequiresApproval = true
			gw.Approver = approvalFunc(func(ctx context.Context, a *domain.Approval) (*domain.Approval, error) {
				if mode == "disabled" {
					v, _ := m.Get(ctx, "team")
					in := input()
					in.Enabled = false
					if _, e := m.Put(ctx, in, v.Revision, "admin"); e != nil {
						t.Fatal(e)
					}
				}
				if mode == "refresh" {
					if _, e := m.Refresh(ctx, "team", "admin"); e != nil {
						t.Fatal(e)
					}
				}
				a.Status = domain.ApprovalApproved
				if mode == "rejected" {
					a.Status = domain.ApprovalRejected
				}
				return a, nil
			})
			_, e := gw.Invoke(t.Context(), p, "mcp.team.search", nil)
			if mode == "approved" {
				if e != nil || calls != 1 {
					t.Fatal(e, calls)
				}
			} else if e == nil || calls != 0 {
				t.Fatal(e, calls)
			}
		})
	}
}
func TestInvocationResultsAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body      string
		fail, truncated bool
	}{
		{"remote error", `{"content":[{"type":"text","text":"missing"}],"isError":true}`, true, false},
		{"malformed", `{"content":null}`, true, false}, {"bad block", `{"content":[{}]}`, true, false},
		{"escaped secret", `{"content":[{"type":"text","text":"unit-\u0073ecret"}]}`, true, false},
		{"secret", `{"content":[{"type":"text","text":"unit-secret"}]}`, true, false},
		{"large", `{"content":[{"type":"text","text":"` + strings.Repeat("x", 40000) + `"}]}`, false, true},
		{"oversize", strings.Repeat("x", maxBody+1), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, gw, p, _ := readyManager(t)
			m.Call = func(context.Context, string, string, string, json.RawMessage) (json.RawMessage, error) {
				return json.RawMessage(tc.body), nil
			}
			r, e := gw.Invoke(t.Context(), p, "mcp.team.search", nil)
			if e != nil || r.IsError != tc.fail || r.Truncated != tc.truncated || strings.Contains(r.Content, "unit-secret") {
				t.Fatal(r, e)
			}
		})
	}
	m, gw, p, _ := readyManager(t)
	calls := 0
	m.Call = func(ctx context.Context, _, _, _ string, _ json.RawMessage) (json.RawMessage, error) {
		calls++
		return nil, errors.New("secret upstream detail")
	}
	r, e := gw.Invoke(t.Context(), p, "mcp.team.search", nil)
	if e != nil || !r.IsError || strings.Contains(r.Content, "secret upstream detail") || calls != 1 {
		t.Fatal(r, e, calls)
	}
	p.Grants[0].Hosts = []string{"another.example"}
	if _, e = gw.Invoke(t.Context(), p, "mcp.team.search", nil); !errors.Is(e, tools.ErrDenied) {
		t.Fatal(e)
	}
	p.Grants[0].Hosts = nil
	r, e = gw.Invoke(t.Context(), p, "mcp.team.search", json.RawMessage(`{"x":"`+strings.Repeat("a", 65536)+`"}`))
	if e != nil || !r.IsError || calls != 1 {
		t.Fatal("argument bound", e, calls)
	}
}
