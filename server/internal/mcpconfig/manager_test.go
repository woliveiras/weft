package mcpconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"net/netip"
	"strings"
	"testing"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	store, e := fsstore.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	box, e := secrets.NewBox(bytes.Repeat([]byte{7}, 32))
	if e != nil {
		t.Fatal(e)
	}
	return New(store, box, nil)
}
func input() Input {
	return Input{ID: "team", Name: "Team", Service: "github", Endpoint: "https://example.com/mcp", Enabled: true}
}
func TestValidation(t *testing.T) {
	for _, endpoint := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com/?token=x", "https://example.com/#x", "file:///tmp/socket"} {
		in := input()
		in.Endpoint = endpoint
		if !errors.Is(validate(in), ErrInvalid) {
			t.Fatal(endpoint)
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.169.254", "::1", "::ffff:127.0.0.1", "64:ff9b::7f00:1", "fe80::1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Fatal(ip)
		}
	}
	if !publicIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address blocked")
	}
}
func TestCredentialsRevisionAndGrants(t *testing.T) {
	ctx := t.Context()
	m := testManager(t)
	in := input()
	in.Credential = &Credential{Value: "synthetic-secret"}
	v, e := m.Put(ctx, in, 0, "admin")
	if e != nil {
		t.Fatal(e)
	}
	settings, e := m.Store.GetSetting(ctx, prefix+in.ID)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(settings.Value, []byte("synthetic-secret")) {
		t.Fatal("plaintext stored")
	}
	safe, _ := json.Marshal(v)
	if bytes.Contains(safe, []byte("synthetic-secret")) || bytes.Contains(safe, []byte("sealed")) {
		t.Fatal("credential leaked")
	}
	if _, e = m.Put(ctx, in, 0, "admin"); !errors.Is(e, storage.ErrConflict) {
		t.Fatal(e)
	}
	m.Discover = func(_ context.Context, _ string, token string) ([]Tool, error) {
		if token != "synthetic-secret" {
			t.Fatal("credential not loaded")
		}
		return []Tool{{Name: "search", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
	}
	v, e = m.Refresh(ctx, in.ID, "operator")
	if e != nil || v.Status != "ready" {
		t.Fatal(v, e)
	}
	g := &domain.Graph{Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "agent", Tools: []domain.ToolGrant{{Name: "mcp.team.search"}}}}}}
	if e = m.ValidateGrants(ctx, g); e != nil {
		t.Fatal(e)
	}
	g.Spec.Nodes[0].Tools[0].Name = "mcp.team.*"
	if !errors.Is(m.ValidateGrants(ctx, g), ErrInvalid) {
		t.Fatal("wildcard accepted")
	}
	g.Spec.Nodes[0].Tools[0].Name = "mcp.other.search"
	if !errors.Is(m.ValidateGrants(ctx, g), ErrInvalid) {
		t.Fatal("other connection accepted")
	}
	in.Credential = nil
	in.Enabled = false
	v, e = m.Put(ctx, in, v.Revision, "admin")
	if e != nil || v.Status != "disabled" || len(v.Tools) > 0 {
		t.Fatal(v, e)
	}
	g.Spec.Nodes[0].Tools[0].Name = "mcp.team.search"
	if !errors.Is(m.ValidateGrants(ctx, g), ErrInvalid) {
		t.Fatal("disabled grant accepted")
	}
	if _, e = m.Refresh(ctx, in.ID, "operator"); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	in.Endpoint = "https://other.example/mcp"
	if _, e = m.Put(ctx, in, v.Revision, "admin"); !errors.Is(e, ErrInvalid) {
		t.Fatal("credential moved", e)
	}
	in.Credential = &Credential{Clear: true}
	v, e = m.Put(ctx, in, v.Revision, "admin")
	if e != nil || v.CredentialConfigured {
		t.Fatal(v, e)
	}
	reloaded := New(m.Store, m.Box, nil)
	again, e := reloaded.Get(ctx, in.ID)
	if e != nil || again.Revision != v.Revision {
		t.Fatal(again, e)
	}
}
func TestDiscoveryFailureAndStaleRefresh(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()
	in := input()
	_, e := m.Put(ctx, in, 0, "admin")
	if e != nil {
		t.Fatal(e)
	}
	m.Discover = func(context.Context, string, string) ([]Tool, error) {
		return nil, errors.New("upstream secret payload")
	}
	v, e := m.Refresh(ctx, in.ID, "operator")
	if e != nil || v.Status != "unavailable" || strings.Contains(v.Error, "secret") {
		t.Fatal(v, e)
	}
	m.Discover = func(context.Context, string, string) ([]Tool, error) {
		current, e := m.Get(ctx, in.ID)
		if e != nil {
			t.Fatal(e)
		}
		in.Enabled = false
		if _, e = m.Put(ctx, in, current.Revision, "admin"); e != nil {
			t.Fatal(e)
		}
		return []Tool{}, nil
	}
	if _, e = m.Refresh(ctx, in.ID, "operator"); !errors.Is(e, storage.ErrConflict) {
		t.Fatal("stale refresh committed", e)
	}
	current, _ := m.Get(ctx, in.ID)
	if current.Enabled {
		t.Fatal("disable overwritten")
	}
}
func TestMissingKeyAndCancelledDiscovery(t *testing.T) {
	m := testManager(t)
	m.Box = nil
	in := input()
	in.Credential = &Credential{Value: "synthetic"}
	if _, e := m.Put(t.Context(), in, 0, "admin"); !errors.Is(e, secrets.ErrNoKey) {
		t.Fatal(e)
	}
	in.Credential = nil
	if _, e := m.Put(t.Context(), in, 0, "admin"); e != nil {
		t.Fatal(e)
	}
	m.Discover = func(context.Context, string, string) ([]Tool, error) { return nil, context.DeadlineExceeded }
	v, e := m.Refresh(t.Context(), in.ID, "operator")
	if e != nil || v.Status != "unavailable" {
		t.Fatal(v, e)
	}
}

type failedWrites struct{ storage.Store }

func (s failedWrites) PutSetting(context.Context, string, []byte, int64, string) (int64, error) {
	return 0, errors.New("storage unavailable")
}
func TestDiscoveryStorageFailurePreservesState(t *testing.T) {
	m := testManager(t)
	v, e := m.Put(t.Context(), input(), 0, "admin")
	if e != nil {
		t.Fatal(e)
	}
	original := m.Store
	m.Store = failedWrites{original}
	m.Discover = func(context.Context, string, string) ([]Tool, error) { return []Tool{}, nil }
	if _, e = m.Refresh(t.Context(), v.ID, "operator"); e == nil {
		t.Fatal("persistence failure hidden")
	}
	m.Store = original
	after, e := m.Get(t.Context(), v.ID)
	if e != nil || after.Status != "untested" || after.Revision != v.Revision {
		t.Fatal(after, e)
	}
}
