package chat

import (
	"context"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

const PrepareTaskEnvironment = "task.prepare_environment"

type EnvironmentPreparation struct {
	Task           string   `json:"task"`
	Revision       int64    `json:"revision"`
	Workflow       string   `json:"workflow"`
	Version        int      `json:"workflowVersion"`
	WorkflowDigest string   `json:"workflowDigest"`
	DefinitionHash string   `json:"definitionHash"`
	Definition     string   `json:"definition"`
	Repositories   []string `json:"repositories,omitempty"`
}

func NewEnvironmentReview(owner, session string, p EnvironmentPreparation, now time.Time) (*OperationReview, error) {
	if !shortLabel(p.Task) || !shortLabel(p.Workflow) || p.Revision < 1 || p.Version < 1 || p.WorkflowDigest == "" || p.DefinitionHash == "" || p.Definition == "" || !safeText(p.Definition, 3000) {
		return nil, ErrInvalidPlan
	}
	for _, repo := range p.Repositories {
		if !safeText(repo, 300) {
			return nil, ErrInvalidPlan
		}
	}
	return newReview(owner, session, now, OperationReview{Environment: &p})
}

func (s Sessions) RecordEnvironmentState(ctx context.Context, owner, id string, prior *OperationReview, next string) (*Session, error) {
	return s.update(ctx, owner, id, func(v *Session) (bool, error) {
		for i := range v.Messages {
			m := &v.Messages[i]
			if m.Review == nil || (m.Review.Environment == nil && m.Review.WorkflowEnvironment == nil) || m.Review.ID != prior.ID {
				continue
			}
			if m.Review.State != prior.State || prior.State == next {
				return false, nil
			}
			switch prior.State {
			case ReviewApplying, ReviewUnknown, ReviewQueued:
			default:
				return false, nil
			}
			m.Review.State = next
			if next != ReviewQueued {
				m.Unread = true
			}
			v.LastActivity = time.Now().UTC()
			return true, nil
		}
		return false, storage.ErrNotFound
	})
}
