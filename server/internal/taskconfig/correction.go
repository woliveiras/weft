package taskconfig

import (
	"context"
	"fmt"
)

// DraftText is the editable description of a task. All execution and repository
// bindings come from the saved draft, including when editing a submitted task.
type DraftText struct {
	Title   string `json:"title"`
	Request string `json:"request"`
	Outcome string `json:"outcome"`
}

func (d Draft) Text() DraftText {
	return DraftText{Title: d.Title, Request: d.Request, Outcome: d.Outcome}
}

func (d Draft) CorrectedInput(next DraftText) (Input, error) {
	if d.Status != "draft" || d.ExecutionGraphID != "" || d.EnvironmentApprovalID != "" {
		return Input{}, fmt.Errorf("%w: task is outside the editable draft state", ErrInvalid)
	}
	in := d.Input
	if in.ChannelID != "" && (in.Request != next.Request || in.Title != next.Title) {
		if in.PayloadJSON != taskRequestPayload(d.Title, d.Request) {
			return Input{}, fmt.Errorf("%w: review the custom channel payload in Tasks", ErrInvalid)
		}
		in.PayloadJSON = taskRequestPayload(next.Title, next.Request)
	}
	in.Title, in.Request, in.Outcome = next.Title, next.Request, next.Outcome
	return in, Validate(in)
}

// CorrectForOperation uses the same audit and revision guard as a manual edit.
// A receipt written in the task CAS can recover a lost Chat response.
func (m Manager) CorrectForOperation(ctx context.Context, draft *Draft, next DraftText, actor, operation string) (*Draft, error) {
	if operation == "" || draft == nil || draft.ID == "" || draft.Revision < 1 {
		return nil, fmt.Errorf("%w: expected a saved draft and correction identity", ErrInvalid)
	}
	in, err := draft.CorrectedInput(next)
	if err != nil {
		return nil, err
	}
	return m.saveDraft(ctx, draft.ID, in, draft.Revision, actor, "", nil, operation)
}
