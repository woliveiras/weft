package chat

import "github.com/woliveiras/weft/server/internal/executioncontrol"

type ExecutionSummary struct {
	ID                    string `json:"id"`
	Root                  string `json:"root"`
	Graph                 string `json:"graph"`
	State                 string `json:"state"`
	Nodes                 int    `json:"nodes"`
	Active                int    `json:"active"`
	CancellationRequested bool   `json:"cancellationRequested"`
}

func SummarizeExecution(tree executioncontrol.Tree) (*ExecutionSummary, bool) {
	if !validReferenceID(tree.SelectedID) || !validReferenceID(tree.RootID) {
		return nil, false
	}
	s := &ExecutionSummary{ID: tree.SelectedID, Root: tree.RootID, Nodes: len(tree.Nodes)}
	for _, node := range tree.Nodes {
		e := node.Execution
		if e == nil {
			return nil, false
		}
		if !e.Status.Terminal() {
			s.Active++
		}
		if e.ID == tree.SelectedID {
			s.Graph = e.GraphID
			s.State = string(e.Status)
		}
		if node.Cancellation != nil && (e.ID == tree.SelectedID || e.ID == tree.RootID && node.Cancellation.Scope == "tree") {
			s.CancellationRequested = true
		}
	}
	if !validReferenceID(s.Graph) || !validReferenceID(s.State) {
		return nil, false
	}
	return s, true
}
