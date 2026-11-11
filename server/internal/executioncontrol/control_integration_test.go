package executioncontrol

import (
	"context"
	"errors"
	"fmt"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
	"path/filepath"
	"testing"
	"time"
)

func manager(t *testing.T) Manager {
	t.Helper()
	s, e := sqlstore.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "tree.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	if e = s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	return Manager{s}
}
func put(t *testing.T, m Manager, id, parent string, status domain.ExecutionStatus) {
	t.Helper()
	if e := m.Store.PutExecution(context.Background(), &domain.Execution{ID: id, ParentExecutionID: parent, GraphID: "g", RunID: "r", Status: status, StartedAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
}
func TestPersistedTreeAndCancellation(t *testing.T) {
	ctx := context.Background()
	m := manager(t)
	put(t, m, "root", "", domain.ExecRunning)
	put(t, m, "child", "root", domain.ExecAwaitingApproval)
	for i := 0; i < 110; i++ {
		put(t, m, fmt.Sprint("unrelated", i), "", domain.ExecSucceeded)
	}
	tree, e := m.Tree(ctx, "child")
	if e != nil || tree.RootID != "root" || len(tree.Nodes) != 2 {
		t.Fatal(tree, e)
	}
	watched, stop := m.Watch(ctx, "child", "root")
	defer stop()
	result, e := m.Cancel(ctx, "child", "child", "operator")
	if e != nil || !result.Created {
		t.Fatal(result, e)
	}
	again, e := m.Cancel(ctx, "child", "child", "other")
	if e != nil || again.Created || again.Request.Actor != "operator" || !again.Request.At.Equal(result.Request.At) {
		t.Fatal(again, e)
	}
	select {
	case <-watched.Done():
		if !errors.Is(context.Cause(watched), ErrRequested) {
			t.Fatal(context.Cause(watched))
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not observe durable request")
	}
	if e = m.Check(ctx, "root"); e != nil {
		t.Fatal("child cancelled parent", e)
	}
	result, e = m.Cancel(ctx, "child", "tree", "admin")
	if e != nil || result.Request.ExecutionID != "root" {
		t.Fatal(result, e)
	}
	if !errors.Is(m.Check(ctx, "root"), ErrRequested) {
		t.Fatal("root still allowed")
	}
	x, _ := m.Store.GetExecution(ctx, "child")
	if x.Status != domain.ExecAwaitingApproval {
		t.Fatal("request pretended worker finished")
	}
	tree, e = m.Tree(ctx, "root")
	if e != nil || tree.Nodes[1].Cancellation.Scope != "tree" {
		t.Fatal(tree, e)
	}
	if e = m.Check(ctx, "unrelated0"); e != nil {
		t.Fatal("unrelated cancelled", e)
	}
	put(t, m, "child", "root", domain.ExecCancelled)
	finished, e := m.Cancel(ctx, "child", "child", "another-operator")
	if e != nil || !finished.AlreadyTerminal || finished.Created || finished.Request.Actor != "operator" {
		t.Fatal("completed child must retain its first request without reporting pending work", finished, e)
	}
	stillActive, e := m.Cancel(ctx, "root", "tree", "another-operator")
	if e != nil || stillActive.AlreadyTerminal || stillActive.Created {
		t.Fatal("root is still running", stillActive, e)
	}
	put(t, m, "root", "", domain.ExecCancelled)
	finished, e = m.Cancel(ctx, "root", "tree", "another-operator")
	if e != nil || !finished.AlreadyTerminal || finished.Created || finished.Request.Actor != "admin" {
		t.Fatal("completed tree must report terminal while retaining the original request", finished, e)
	}
}
func TestTreeRejectsInvalidAndTerminalRequests(t *testing.T) {
	ctx := context.Background()
	m := manager(t)
	put(t, m, "root", "", domain.ExecSucceeded)
	if r, e := m.Cancel(ctx, "root", "tree", "a"); e != nil || !r.AlreadyTerminal {
		t.Fatal(r, e)
	}
	if r, _ := m.Requested(ctx, "root"); r != nil {
		t.Fatal("terminal marker")
	}
	if _, e := m.Cancel(ctx, "root", "child", "a"); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if _, e := m.Tree(ctx, "missing"); !errors.Is(e, storage.ErrNotFound) {
		t.Fatal(e)
	}
	put(t, m, "child", "root", domain.ExecRunning)
	x, _ := m.Store.GetExecution(ctx, "child")
	x.GraphID = "foreign"
	m.Store.PutExecution(ctx, x)
	if _, e := m.Cancel(ctx, "root", "tree", "a"); !errors.Is(e, storage.ErrConflict) {
		t.Fatal("cross graph", e)
	}
	x.GraphID = "g"
	m.Store.PutExecution(ctx, x)
	put(t, m, "root", "child", domain.ExecRunning)
	if _, e := m.Tree(ctx, "child"); !errors.Is(e, storage.ErrConflict) {
		t.Fatal("cycle", e)
	}
}
func TestTreeRejectsOverflowAndCorruptRequests(t *testing.T) {
	ctx := context.Background()
	m := manager(t)
	put(t, m, "root", "", domain.ExecRunning)
	for i := 0; i < MaxNodes; i++ {
		put(t, m, fmt.Sprint("c", i), "root", domain.ExecRunning)
	}
	if _, e := m.Tree(ctx, "root"); !errors.Is(e, storage.ErrConflict) {
		t.Fatal("truncated tree accepted", e)
	}
	if _, e := m.Store.PutSetting(ctx, Prefix+"root", []byte(`{}`), 0, "a"); e != nil {
		t.Fatal(e)
	}
	if !errors.Is(m.Check(ctx, "root"), ErrUnavailable) {
		t.Fatal("corrupt request ignored")
	}
}

func TestChildCancellationIsVisibleOnDescendantsNotSiblings(t *testing.T) {
	m := manager(t)
	ctx := context.Background()
	put(t, m, "root", "", domain.ExecRunning)
	put(t, m, "child", "root", domain.ExecRunning)
	put(t, m, "sibling", "root", domain.ExecRunning)
	put(t, m, "grandchild", "child", domain.ExecRunning)
	if _, err := m.Cancel(ctx, "child", "child", "operator"); err != nil {
		t.Fatal(err)
	}
	tree, err := m.Tree(ctx, "grandchild")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range tree.Nodes {
		inherited := node.Execution.ID == "child" || node.Execution.ID == "grandchild"
		if inherited && (node.Cancellation == nil || node.Cancellation.ExecutionID != "child") {
			t.Fatalf("missing inherited cancellation %+v", node)
		}
		if !inherited && node.Cancellation != nil {
			t.Fatalf("unrelated cancellation %+v", node)
		}
	}
}
