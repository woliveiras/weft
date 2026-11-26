package executioncontrol

import (
	"context"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

type CancelScope struct {
	Selected string `json:"selected"`
	Root     string `json:"root"`
	Graph    string `json:"graph"`
	Run      string `json:"run"`
	Mode     string `json:"mode"`
}

func (s CancelScope) Target() string {
	if s.Mode == "tree" {
		return s.Root
	}
	return s.Selected
}

func (s CancelScope) Matches(tree Tree) bool {
	if s.Selected != tree.SelectedID || s.Root != tree.RootID || s.Selected == "" || s.Root == "" || s.Graph == "" || s.Run == "" || (s.Mode != "child" && s.Mode != "tree") || (s.Mode == "child" && s.Selected == s.Root) {
		return false
	}
	for _, node := range tree.Nodes {
		if x := node.Execution; x != nil && x.ID == s.Selected {
			return x.GraphID == s.Graph && x.RunID == s.Run
		}
	}
	return false
}
func (m Manager) CancelReviewed(ctx context.Context, scope CancelScope, actor, operation string) (Result, error) {
	if operation == "" {
		return Result{}, ErrInvalid
	}
	return m.cancel(ctx, scope.Selected, scope.Mode, actor, operation, &scope)
}

func (m Manager) ObserveReview(ctx context.Context, scope CancelScope, operation string) (found, settled, cancelled bool, err error) {
	tree, e := m.Tree(ctx, scope.Selected)
	if e != nil {
		return false, false, false, e
	}
	if !scope.Matches(tree) {
		return false, false, false, storage.ErrConflict
	}
	req, e := m.Requested(ctx, scope.Target())
	if e != nil {
		return false, false, false, e
	}
	if req == nil {
		return false, false, false, nil
	}
	if req.Operation != operation || req.Scope != scope.Mode {
		return false, false, false, storage.ErrConflict
	}
	settled = true
	for _, n := range tree.Nodes {
		if scope.Mode == "child" && n.Execution.ID != scope.Selected {
			continue
		}
		if !n.Execution.Status.Terminal() {
			settled = false
		}
		if n.Execution.Status == domain.ExecCancelled {
			cancelled = true
		}
	}
	return true, settled, cancelled, nil
}
