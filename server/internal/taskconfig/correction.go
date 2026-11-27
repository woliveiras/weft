package taskconfig

import (
	"context"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
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
	if !d.editableDraftInput() {
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

func (d Draft) editableDraftInput() bool {
	return d.Status == "draft" && d.ExecutionGraphID == "" && d.EnvironmentApprovalID == ""
}

// ReplacedPayloadInput replaces the complete message body while retaining the
// saved channel, message type and other input fields.
func (d Draft) ReplacedPayloadInput(payload string) (Input, error) {
	if !d.editableDraftInput() || d.ChannelID == "" {
		return Input{}, fmt.Errorf("%w: expected an editable draft with a saved input channel", ErrInvalid)
	}
	in := d.Input
	in.PayloadJSON = payload
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

// ReplacePayloadForOperation keeps the stored draft and replaces one field.
// Its receipt is persisted with that change under the task revision CAS.
func (m Manager) ReplacePayloadForOperation(ctx context.Context, id string, revision int64, payload, actor, operation string) (*Draft, error) {
	if operation == "" || revision < 1 {
		return nil, fmt.Errorf("%w: expected a saved revision and correction identity", ErrInvalid)
	}
	draft, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if draft.Revision != revision {
		return nil, storage.ErrConflict
	}
	in, err := draft.ReplacedPayloadInput(payload)
	if err != nil {
		return nil, err
	}
	if err = m.validateDraftBinding(ctx, in); err != nil {
		return nil, err
	}
	draft.Input = in
	draft.UpdatedAt, draft.UpdatedBy = time.Now().UTC(), actor
	draft.CorrectionOperation, draft.CorrectionRevision = operation, revision+1
	return m.persistDraft(ctx, draft, revision, actor)
}
