package taskconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

func operationDraftID(operation string) string {
	hash := sha256.Sum256([]byte(operation))
	return "tsk_chat" + hex.EncodeToString(hash[:16])
}

// CreateForOperation commits the creation receipt with the new draft using
// the regular validation and audit boundary. Neither the ID nor the receipt
// comes from the task input.
func (m Manager) CreateForOperation(ctx context.Context, in Input, actor, operation string) (*Draft, error) {
	if operation == "" {
		return nil, fmt.Errorf("%w: creation operation is required", ErrInvalid)
	}
	return m.saveDraft(ctx, "", in, 0, actor, "", nil, operation)
}

// CreationForOperation proves that the operation created a task, including
// after ordinary subsequent edits. It never creates or updates anything.
func (m Manager) CreationForOperation(ctx context.Context, operation string) (*Draft, error) {
	draft, err := m.Get(ctx, operationDraftID(operation))
	if err != nil {
		return nil, err
	}
	if draft.CreationOperation != operation {
		return nil, storage.ErrNotFound
	}
	return draft, nil
}

// BindWorkflowDraft selects the native entry and result binding of a local
// task draft. It uses saved defaults where present and can infer a binding
// only when the graph gives one structurally unambiguous choice.
func BindWorkflowDraft(in Input, g *domain.Graph) (Input, error) {
	draft := Draft{Input: in}
	bindPrompt(&draft, g)
	if defaults := g.Spec.Task; defaults != nil {
		if len(defaults.ProjectIDs) != 0 || defaults.ProjectID != "" || defaults.SCMConnection != "" {
			return Input{}, fmt.Errorf("%w: choose project or delivery settings before drafting this task", ErrInvalid)
		}
		if defaults.NodeID != "" {
			draft.NodeID = defaults.NodeID
		}
		if defaults.ResultNodeID != "" {
			draft.ResultNodeID = defaults.ResultNodeID
		}
		if defaults.ChannelID != "" {
			draft.ChannelID, draft.MessageType = defaults.ChannelID, defaults.MessageType
			draft.PayloadJSON = taskRequestPayload(draft.Title, draft.Request)
		}
	}
	entry, result := g.Node(draft.NodeID), g.Node(draft.ResultNodeID)
	if entry == nil || entry.Disabled || result == nil || result.Disabled {
		return Input{}, fmt.Errorf("%w: choose enabled entry and result agents", ErrInvalid)
	}
	if draft.ChannelID != "" {
		channel := g.Channel(draft.ChannelID)
		if channel == nil || len(channel.To) != 1 || channel.To[0] != entry.ID {
			return Input{}, fmt.Errorf("%w: choose an input channel for the entry agent", ErrInvalid)
		}
	}
	if err := Validate(draft.Input); err != nil {
		return Input{}, err
	}
	return draft.Input, nil
}
