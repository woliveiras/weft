package trigger

import (
	"context"
	"strings"

	"github.com/woliveiras/weft/server/internal/tools"
)

// DedupKey identifies an issue for a workflow: an issue starts at most one
// task per workflow, however often it is relabelled.
func DedupKey(t *Trigger, i Issue) string {
	return i.Source + ":" + i.Key + "@" + t.Workflow
}

// Reason says why an issue cannot start a task now, or "" when it can.
func Reason(t *Trigger, i Issue) string {
	switch {
	case !i.HasLabel(t.Label):
		return "does not have the label " + t.Label
	case !i.Open:
		return "is not open (To Do)"
	case i.Assignee != "" && !strings.EqualFold(i.Assignee, t.Owner):
		return "is assigned to someone else"
	case i.Blocked:
		return "is blocked"
	case t.Processed[DedupKey(t, i)] != "":
		return "already ran with this workflow (task " + t.Processed[DedupKey(t, i)] + ")"
	}
	return ""
}

// Matches is what the next run would do with the issues the source has now.
type Matches struct {
	Next    *Issue  `json:"next,omitempty"`
	Waiting []Issue `json:"waiting"`
	Skipped []Skip  `json:"skipped"`
	// Busy names the task the trigger waits for before taking another issue.
	Busy string `json:"busy,omitempty"`
}

// Principal is the identity a trigger's tracker calls run as: only the
// source's tools are granted.
func Principal(t *Trigger, s Source) *tools.Principal {
	return &tools.Principal{GraphID: "trigger-" + t.ID, NodeID: "trigger", ExecutionID: t.ID, Grants: Grants(s)}
}

// Match searches the source and sorts issues into next, waiting and
// skipped, in the source's order (oldest first).
func (r *Runner) Match(ctx context.Context, t *Trigger) (Matches, Source, Invoker, error) {
	src, err := t.Source.Open()
	if err != nil {
		return Matches{}, nil, nil, err
	}
	call := GatewayInvoker(r.Gateway, Principal(t, src))
	issues, err := src.Search(ctx, call, t.Label)
	if err != nil {
		return Matches{}, nil, nil, err
	}
	m := Matches{Waiting: []Issue{}, Skipped: []Skip{}, Busy: t.CurrentTask}
	for _, i := range issues {
		if reason := Reason(t, i); reason != "" {
			m.Skipped = append(m.Skipped, Skip{Key: i.Key, Title: i.Title, Reason: reason})
			continue
		}
		if m.Next == nil && m.Busy == "" {
			next := i
			m.Next = &next
			continue
		}
		m.Waiting = append(m.Waiting, i)
	}
	return m, src, call, nil
}
