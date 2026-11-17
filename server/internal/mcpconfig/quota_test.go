package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuotaWindowPersistenceBoundaries(t *testing.T) {
	m := testManager(t)
	r := record{View: View{ID: "team", CallsPerMinute: 2}}
	now := time.Unix(1200, 0)
	for i := 0; i < 2; i++ {
		if err := m.admitCallAt(t.Context(), r, now); err != nil {
			t.Fatal(err)
		}
	}
	restart := New(m.Store, m.Box, nil)
	if err := restart.admitCallAt(t.Context(), r, now); !errors.Is(err, tools.ErrRateLimited) {
		t.Fatal(err)
	}
	if err := restart.admitCallAt(t.Context(), r, now.Add(-time.Minute)); !errors.Is(err, tools.ErrRateLimited) {
		t.Fatal("clock rollback reset quota", err)
	}
	if err := restart.admitCallAt(t.Context(), r, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	r.ID = "other"
	if err := restart.admitCallAt(t.Context(), r, now); err != nil {
		t.Fatal(err)
	}
	r.CallsPerMinute = 0
	restart.Store = failedWrites{m.Store}
	if err := restart.admitCallAt(t.Context(), r, now); err != nil {
		t.Fatal(err)
	}
	r.CallsPerMinute = 3
	if err := restart.admitCallAt(t.Context(), r, now); err == nil {
		t.Fatal("quota storage failed open")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := m.admitCallAt(ctx, r, now); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, limit := range []int{-1, 60001} {
		in := input()
		in.CallsPerMinute = limit
		if _, err := m.Put(t.Context(), in, 0, "admin"); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
}
func TestQuotaConcurrentControllers(t *testing.T) {
	m := testManager(t)
	other := New(m.Store, m.Box, nil)
	r := record{View: View{ID: "team", CallsPerMinute: 7}}
	now := time.Unix(1200, 0)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			manager := m
			if i%2 == 0 {
				manager = other
			}
			err := manager.admitCallAt(t.Context(), r, now)
			if err == nil {
				allowed.Add(1)
			} else if !errors.Is(err, tools.ErrRateLimited) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if allowed.Load() != 7 {
		t.Fatal(allowed.Load())
	}
}
func TestQuotaGatewayAndApprovalAccounting(t *testing.T) {
	m, gw, p, a := readyManager(t)
	r, err := m.load(t.Context(), "team")
	if err != nil {
		t.Fatal(err)
	}
	r.CallsPerMinute = 1
	if _, err = m.save(t.Context(), r, r.Revision, "admin", "test"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.Call = func(context.Context, string, string, string, json.RawMessage) (json.RawMessage, error) {
		calls++
		return nil, errors.New("remote failed")
	}
	p.Grants[0].RequiresApproval = true
	gw.Approver = approvalFunc(func(context.Context, *domain.Approval) (*domain.Approval, error) {
		return &domain.Approval{Status: domain.ApprovalRejected, Comment: "synthetic-private-comment"}, nil
	})
	if _, err = gw.Invoke(t.Context(), p, "mcp.team.search", json.RawMessage(`{}`)); !errors.Is(err, tools.ErrRejected) {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal(calls)
	}
	raw, _ := json.Marshal(a.events)
	if containsCredential(raw, "synthetic-private-comment") {
		t.Fatal("approval comment leaked to MCP audit")
	}
	p.Grants[0].RequiresApproval = false
	result, err := gw.Invoke(t.Context(), p, "mcp.team.search", json.RawMessage(`{}`))
	if err != nil || !result.IsError {
		t.Fatal(result, err)
	}
	p.NodeID = "another-agent"
	if _, err = gw.Invoke(t.Context(), p, "mcp.team.search", json.RawMessage(`{}`)); !errors.Is(err, tools.ErrRateLimited) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("over quota dispatched", calls)
	}
	last := a.events[len(a.events)-1]
	if last.Type != domain.EvToolDenied || !containsCredential(last.Data, "rate_limited") {
		t.Fatal(last)
	}
}
