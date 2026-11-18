package taskconfig

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

func good() Input {
	return Input{Title: "Read", Request: "Explain architecture", Outcome: "understand", GraphID: "factory", GraphVersion: 1, NodeID: "reader"}
}
func TestValidate(t *testing.T) {
	if err := Validate(good()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Input){func(i *Input) { i.ExecutionMode = "unsafe" }, func(i *Input) { i.Title = " " }, func(i *Input) { i.Request = strings.Repeat("x", 16385) }, func(i *Input) { i.Request = "\x00" }, func(i *Input) { i.Request = string([]byte{255}) }, func(i *Input) { i.Outcome = "execute-anything" }, func(i *Input) { i.GraphVersion = 0 }, func(i *Input) { i.NodeID = "" }} {
		in := good()
		change(&in)
		if !errors.Is(Validate(in), ErrInvalid) {
			t.Fatal("invalid accepted")
		}
	}
}

type broken struct {
	storage.Store
	fail   string
	writes int
}

func (b *broken) GetGraph(context.Context, string, int) (*domain.Graph, error) {
	if b.fail == "graph" {
		return nil, errors.New("graph offline")
	}
	return &domain.Graph{Metadata: domain.GraphMetadata{Version: 1}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reader"}}}}, nil
}
func (b *broken) AppendEvent(context.Context, *domain.Event) error {
	if b.fail == "audit" {
		return errors.New("audit offline")
	}
	return nil
}
func (b *broken) PutSetting(context.Context, string, []byte, int64, string) (int64, error) {
	b.writes++
	return 0, errors.New("store offline")
}
func TestFailuresDoNotAppearSuccessful(t *testing.T) {
	for _, failure := range []string{"graph", "audit", "write"} {
		b := &broken{fail: failure}
		if _, err := (Manager{Store: b}).Save(t.Context(), "", good(), 0, "operator"); err == nil {
			t.Fatal("ignored failure")
		}
		if failure != "write" && b.writes != 0 {
			t.Fatal("mutation after failed dependency")
		}
	}
}

func TestChannelInputValidation(t *testing.T) {
	in := good()
	in.ChannelID = "requests"
	in.MessageType = "feature.request"
	in.PayloadJSON = `{"title":"test"}`
	if err := Validate(in); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Input){func(i *Input) { i.PayloadJSON = "invalid" }, func(i *Input) { i.MessageType = "" }, func(i *Input) { i.ChannelID = "" }, func(i *Input) { i.PayloadJSON = strings.Repeat(" ", 16385) + "{}" }} {
		bad := in
		change(&bad)
		if Validate(bad) == nil {
			t.Fatal("invalid channel input accepted")
		}
	}
}

func TestDecodeLegacyResultDefault(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"nodeId":"entry"}`, "entry"},
		{`{"nodeId":"entry","resultNodeId":"result"}`, "result"},
		{`{"nodeId":"entry","resolution":{"state":"needs_input"}}`, ""},
	} {
		d, err := decode(&storage.Setting{Value: []byte(tc.raw), Revision: 3})
		if err != nil || d.ResultNodeID != tc.want || d.Revision != 3 {
			t.Fatalf("decode %s: %+v %v", tc.raw, d, err)
		}
	}
}

func TestUpdatedTaskOrder(t *testing.T) {
	now := time.Now()
	older := &Draft{ID: "tsk_z", UpdatedAt: now.Add(-time.Second)}
	newer := &Draft{ID: "tsk_a", UpdatedAt: now}
	tied := &Draft{ID: "tsk_b", UpdatedAt: now}
	if compareUpdatedTasks(newer, older) >= 0 || compareUpdatedTasks(older, newer) <= 0 ||
		compareUpdatedTasks(tied, newer) >= 0 || compareUpdatedTasks(tied, tied) != 0 {
		t.Fatal("expected descending timestamp followed by descending ID")
	}
}

func TestListRecentlyUpdatedRejectsInvalidPagination(t *testing.T) {
	manager := Manager{}
	for _, after := range []string{"tsk_legacy", "bad~tsk_x", "2026-10-16T10:00:00Z~bad"} {
		if _, err := manager.ListRecentlyUpdated(t.Context(), after, 1); !errors.Is(err, ErrInvalid) {
			t.Fatalf("cursor %q: %v", after, err)
		}
	}
	for _, limit := range []int{0, 101} {
		if _, err := manager.ListRecentlyUpdated(t.Context(), "", limit); !errors.Is(err, ErrInvalid) {
			t.Fatalf("limit %d: %v", limit, err)
		}
	}
}
