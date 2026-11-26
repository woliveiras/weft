package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

const (
	ReviewAwaiting = "awaiting_confirmation"
	ReviewApplying = "applying"
	ReviewCreated  = "created"
	ReviewConflict = "conflict"
	ReviewUnknown  = "unknown"
	ReviewDeclined = "declined"
)

// ExplicitReviewDirective recognizes the confirmation text shown on the
// review card. Other messages continue through normal model interpretation.
func ExplicitReviewDirective(text string) string {
	switch strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "."))) {
	case "confirm this proposal", "confirme esta proposta":
		return "confirm"
	case "decline this proposal", "recuse esta proposta":
		return "decline"
	default:
		return ""
	}
}

// SinglePendingAgentReview returns a candidate only when an explicit textual
// action has exactly one currently actionable target in the conversation.
func (s *Session) SinglePendingAgentReview(now time.Time) *AgentReview {
	var pending *AgentReview
	for _, message := range s.Messages {
		review := message.Review
		if review == nil || review.State != ReviewAwaiting {
			continue
		}
		if pending != nil {
			return nil
		}
		pending = review
	}
	if pending != nil && !now.Before(pending.ExpiresAt) {
		return nil
	}
	return pending
}

var (
	ErrReviewMismatch = errors.New("the confirmed review does not match the saved action")
	ErrExpiredReview  = errors.New("the confirmation expired; review the action again")
	agentDraftID      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

// AgentDraft is the narrow catalog creation intent a model may propose.
// Runtime, model settings, grants and other execution fields are selected by
// the control plane and shown explicitly before the user confirms.
type AgentDraft struct {
	ID           string
	Name         string
	Instructions string
}

func (d AgentDraft) validate() error {
	if !agentDraftID.MatchString(d.ID) || d.ID == "new" || d.ID == "source" || strings.HasPrefix(d.ID, "workflow-agent-") ||
		strings.TrimSpace(d.Name) == "" || !safeText(d.Name, 200) || strings.ContainsAny(d.Name, "\r\n\t") ||
		strings.TrimSpace(d.Instructions) == "" || !safeText(d.Instructions, 3000) {
		return ErrInvalidPlan
	}
	return nil
}

func (d AgentDraft) Definition(model domain.ModelRef) (domain.AgentDefinition, error) {
	if d.validate() != nil || model.Provider == "" || model.Name == "" {
		return domain.AgentDefinition{}, ErrInvalidPlan
	}
	return domain.AgentDefinition{
		Name: strings.TrimSpace(d.Name), Prompt: strings.TrimSpace(d.Instructions),
		Model:   domain.ModelRef{Provider: model.Provider, Name: model.Name},
		Runtime: domain.RuntimeRef{Kind: "native"}, Isolation: domain.IsolationSpec{Mode: domain.IsolationProcess}, ReadOnly: true,
	}, nil
}

// AgentReview persists the completely rendered action. Only state and receipt
// may change after the review was issued. The digest is scoped to the owner
// and session and fences the model-selection revision.
type AgentReview struct {
	ID            string                 `json:"id"`
	AgentID       string                 `json:"agentId"`
	Definition    domain.AgentDefinition `json:"definition"`
	ModelRevision int64                  `json:"modelRevision"`
	ExpiresAt     time.Time              `json:"expiresAt"`
	Digest        string                 `json:"digest"`
	State         string                 `json:"state"`
	ClaimedAt     time.Time              `json:"claimedAt,omitempty"`
	ResultVersion int                    `json:"resultVersion,omitempty"`
}

func NewAgentReview(owner, session, agent string, definition domain.AgentDefinition, modelRevision int64, now time.Time) (*AgentReview, error) {
	review := &AgentReview{
		ID: domain.NewID("chatop"), AgentID: agent, Definition: definition, ModelRevision: modelRevision,
		ExpiresAt: now.Add(10 * time.Minute).UTC(), State: ReviewAwaiting,
	}
	digest, err := review.digest(owner, session)
	if err != nil {
		return nil, err
	}
	review.Digest = digest
	return review, nil
}

func (r AgentReview) digest(owner, session string) (string, error) {
	raw, err := json.Marshal(struct {
		Owner, Session, Operation, ID, Agent   string
		Definition                             domain.AgentDefinition
		ExpectedCatalogRevision, ModelRevision int64
		ExpiresAt                              time.Time
	}{
		Owner: owner, Session: session, Operation: "agent.create", ID: r.ID, Agent: r.AgentID,
		Definition: r.Definition, ExpectedCatalogRevision: 0, ModelRevision: r.ModelRevision, ExpiresAt: r.ExpiresAt,
	})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

func (r AgentReview) ValidateConfirmation(owner, session, digest string, now time.Time) error {
	if err := r.validateDigest(owner, session, digest); err != nil {
		return err
	}
	if !now.Before(r.ExpiresAt) {
		return ErrExpiredReview
	}
	return nil
}

func (r AgentReview) validateDigest(owner, session, digest string) error {
	expected, err := r.digest(owner, session)
	if err != nil {
		return err
	}
	if expected != r.Digest || expected != digest {
		return ErrReviewMismatch
	}
	return nil
}

func (s Sessions) ClaimAgentReview(ctx context.Context, owner, sessionID, reviewID, digest string) (*Session, *AgentReview, bool, error) {
	var review *AgentReview
	var claimed bool
	session, err := s.update(ctx, owner, sessionID, func(session *Session) (bool, error) {
		claimed = false
		review = nil
		for _, message := range session.Messages {
			if message.Review == nil || message.Review.ID != reviewID {
				continue
			}
			review = message.Review
			if review.State != ReviewAwaiting {
				return false, review.validateDigest(owner, sessionID, digest)
			}
			if err := review.ValidateConfirmation(owner, sessionID, digest, time.Now()); err != nil {
				return false, err
			}
			review.State, review.ClaimedAt = ReviewApplying, time.Now().UTC()
			claimed = true
			return true, nil
		}
		return false, storage.ErrNotFound
	})
	return session, review, claimed && err == nil, err
}

func (s Sessions) FinishAgentReview(ctx context.Context, owner, sessionID, reviewID, state string, version int) (*Session, error) {
	return s.update(ctx, owner, sessionID, func(session *Session) (bool, error) {
		for _, message := range session.Messages {
			if review := message.Review; review != nil && review.ID == reviewID {
				if review.State != ReviewApplying && !(review.State == ReviewUnknown && state == ReviewCreated) {
					return false, nil
				}
				review.State, review.ResultVersion = state, version
				session.LastActivity = time.Now().UTC()
				return true, nil
			}
		}
		return false, storage.ErrNotFound
	})
}

func (s Sessions) DeclineAgentReview(ctx context.Context, owner, sessionID, reviewID, digest string) (*Session, error) {
	return s.update(ctx, owner, sessionID, func(session *Session) (bool, error) {
		for _, message := range session.Messages {
			if review := message.Review; review != nil && review.ID == reviewID {
				if review.State != ReviewAwaiting {
					return false, review.validateDigest(owner, sessionID, digest)
				}
				if err := review.ValidateConfirmation(owner, sessionID, digest, time.Now()); err != nil {
					return false, err
				}
				review.State = ReviewDeclined
				session.LastActivity = time.Now().UTC()
				return true, nil
			}
		}
		return false, storage.ErrNotFound
	})
}
