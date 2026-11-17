// Package executioncontrol provides persisted execution trees and cooperative
// cancellation scoped to immutable execution IDs, never to future agent turns.
package executioncontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

const Prefix = "execution-control/cancel/"
const MaxNodes = 256
const MaxDepth = 16

var ErrRequested = errors.New("execution cancellation requested by operator")
var ErrUnavailable = errors.New("execution cancellation state unavailable")
var ErrInvalid = errors.New("invalid execution cancellation scope")

type Request struct {
	ExecutionID string    `json:"executionId"`
	RootID      string    `json:"rootExecutionId"`
	Scope       string    `json:"scope"`
	Actor       string    `json:"actor"`
	At          time.Time `json:"requestedAt"`
}
type Node struct {
	Execution    *domain.Execution `json:"execution"`
	Cancellation *Request          `json:"cancellation,omitempty"`
}
type Tree struct {
	RootID     string `json:"rootExecutionId"`
	SelectedID string `json:"selectedExecutionId"`
	Nodes      []Node `json:"nodes"`
}
type Result struct {
	Created         bool     `json:"-"`
	Request         *Request `json:"request,omitempty"`
	AlreadyTerminal bool     `json:"alreadyTerminal"`
}
type Manager struct{ Store storage.Store }

func (m Manager) Requested(ctx context.Context, id string) (*Request, error) {
	r, e := m.Store.GetSetting(ctx, Prefix+id)
	if errors.Is(e, storage.ErrNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var v Request
	if e = json.Unmarshal(r.Value, &v); e != nil || v.ExecutionID != id || (v.Scope != "child" && v.Scope != "tree") || v.RootID == "" {
		return nil, errors.New("invalid stored cancellation request")
	}
	return &v, nil
}
func family(a, b *domain.Execution) bool { return a.GraphID == b.GraphID && a.RunID == b.RunID }
func (m Manager) Tree(ctx context.Context, id string) (Tree, error) {
	selected, e := m.Store.GetExecution(ctx, id)
	if e != nil {
		return Tree{}, e
	}
	root := selected
	seen := map[string]bool{id: true}
	for root.ParentExecutionID != "" {
		if len(seen) >= MaxDepth || seen[root.ParentExecutionID] {
			return Tree{}, fmt.Errorf("%w: invalid execution ancestry", storage.ErrConflict)
		}
		p, e := m.Store.GetExecution(ctx, root.ParentExecutionID)
		if e != nil {
			return Tree{}, e
		}
		if !family(selected, p) {
			return Tree{}, fmt.Errorf("%w: inconsistent execution ancestry", storage.ErrConflict)
		}
		root = p
		seen[root.ID] = true
	}
	out := Tree{RootID: root.ID, SelectedID: id, Nodes: []Node{}}
	queue := []*domain.Execution{root}
	depths := map[string]int{root.ID: 0}
	seen = map[string]bool{root.ID: true}
	rootRequest, e := m.Requested(ctx, root.ID)
	if e != nil {
		return Tree{}, e
	}
	inherited := map[string]*Request{root.ID: rootRequest}
	for len(queue) > 0 {
		x := queue[0]
		queue = queue[1:]
		request := inherited[x.ID]
		if request == nil {
			request, e = m.Requested(ctx, x.ID)
			if e != nil {
				return Tree{}, e
			}
		}
		out.Nodes = append(out.Nodes, Node{x, request})
		children, e := m.Store.ListExecutions(ctx, storage.ExecutionFilter{ParentExecutionID: x.ID, Limit: MaxNodes + 1})
		if e != nil {
			return Tree{}, e
		}
		sort.Slice(children, func(i, j int) bool {
			if !children[i].StartedAt.Equal(children[j].StartedAt) {
				return children[i].StartedAt.Before(children[j].StartedAt)
			}
			return children[i].ID < children[j].ID
		})
		for _, c := range children {
			if seen[c.ID] || !family(root, c) || c.ParentExecutionID != x.ID || len(seen) >= MaxNodes || depths[x.ID]+1 >= MaxDepth {
				return Tree{}, fmt.Errorf("%w: execution tree is inconsistent or exceeds bounds", storage.ErrConflict)
			}
			seen[c.ID] = true
			depths[c.ID] = depths[x.ID] + 1
			inherited[c.ID] = request
			queue = append(queue, c)
		}
	}
	if !seen[id] {
		return Tree{}, fmt.Errorf("%w: execution is absent from its tree", storage.ErrConflict)
	}
	return out, nil
}

// Cancel only acknowledges a durable request. Workers own terminal outcomes.
func (m Manager) Cancel(ctx context.Context, id, scope, actor string) (Result, error) {
	if scope != "child" && scope != "tree" {
		return Result{}, ErrInvalid
	}
	tree, e := m.Tree(ctx, id)
	if e != nil {
		return Result{}, e
	}
	target := tree.RootID
	active := false
	for _, n := range tree.Nodes {
		if scope == "tree" {
			active = active || !n.Execution.Status.Terminal()
		} else if n.Execution.ID == id {
			if n.Execution.ParentExecutionID == "" {
				return Result{}, ErrInvalid
			}
			target = id
			active = !n.Execution.Status.Terminal()
		}
	}
	if old, e := m.Requested(ctx, target); e != nil {
		return Result{}, e
	} else if old != nil {
		return Result{Request: old, AlreadyTerminal: !active}, nil
	}
	if !active {
		return Result{AlreadyTerminal: true}, nil
	}
	r := &Request{target, tree.RootID, scope, actor, time.Now().UTC()}
	b, e := json.Marshal(r)
	if e != nil {
		return Result{}, e
	}
	if _, e = m.Store.PutSetting(ctx, Prefix+target, b, 0, actor); errors.Is(e, storage.ErrConflict) {
		old, readErr := m.Requested(ctx, target)
		return Result{Request: old}, readErr
	} else if e != nil {
		return Result{}, e
	}
	return Result{Request: r, Created: true}, nil
}

// Check is used at boundaries as well as by the poller; a storage failure must
// not look like permission to keep executing after cancellation.
func (m Manager) Check(ctx context.Context, ids ...string) error {
	for _, id := range ids {
		r, e := m.Requested(ctx, id)
		if e != nil {
			return fmt.Errorf("%w: %v", ErrUnavailable, e)
		}
		if r != nil {
			return ErrRequested
		}
	}
	return nil
}
func (m Manager) Watch(parent context.Context, ids ...string) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	check := func() {
		if e := m.Check(ctx, ids...); e != nil {
			cancel(e)
		}
	}
	check()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				check()
			}
		}
	}()
	return ctx, func() { cancel(context.Canceled); <-done }
}
