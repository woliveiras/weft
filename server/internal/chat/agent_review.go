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
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/storage"
)

const (
	ReviewAwaiting = "awaiting_confirmation"
	ReviewApplying = "applying"
	ReviewCreated  = "created"
	ReviewConflict = "conflict"
	ReviewUnknown  = "unknown"
	ReviewDeclined = "declined"
	ReviewAccepted = "accepted"
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

// SinglePendingReview returns a candidate only when an explicit textual
// action has exactly one currently actionable target in the conversation.
func (s *Session) SinglePendingReview(now time.Time) *OperationReview {
	var pending *OperationReview
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

type AgentCreation struct {
	AgentID       string                 `json:"agentId"`
	Definition    domain.AgentDefinition `json:"definition"`
	ModelRevision int64                  `json:"modelRevision"`
}

// OperationReview persists the reviewed action. The digest binds the payload
// to the owner and session. Claim state and the result are recorded separately.
type OperationReview struct {
	TaskCorrection *TaskCorrection  `json:"taskCorrection,omitempty"`
	Skill          *SkillChange     `json:"skill,omitempty"`
	PR             *PRAnalysis      `json:"pr,omitempty"`
	PRSummary      string           `json:"prSummary,omitempty"`
	Instructions   *InstructionEdit `json:"instructions,omitempty"`
	Guidance       *Guidance        `json:"guidance,omitempty"`
	ConsumedAt     *time.Time       `json:"consumedAt,omitempty"`
	*AgentCreation
	Publication     *CatalogPublication                `json:"publication,omitempty"`
	Adoption        *AgentAdoption                     `json:"adoption,omitempty"`
	Configuration   *ConfigReview                      `json:"configuration,omitempty"`
	ConfigReceipts  []ConfigReceipt                    `json:"configReceipts,omitempty"`
	Workflow        *WorkflowCreation                  `json:"workflow,omitempty"`
	TaskDraft       *TaskCreation                      `json:"taskDraft,omitempty"`
	TaskStart       *TaskAdmission                     `json:"taskStart,omitempty"`
	TaskCancel      *TaskCancellation                  `json:"taskCancel,omitempty"`
	TaskRetry       *TaskRetry                         `json:"taskRetry,omitempty"`
	Approval        *ApprovalDecision                  `json:"approval,omitempty"`
	WorkflowControl *WorkflowControl                   `json:"workflowControl,omitempty"`
	ControlResult   *engine.WorkflowControlObservation `json:"controlResult,omitempty"`
	ID              string                             `json:"id"`
	ExpiresAt       time.Time                          `json:"expiresAt"`
	Digest          string                             `json:"digest"`
	State           string                             `json:"state"`
	ClaimedAt       time.Time                          `json:"claimedAt,omitempty"`
	ResultVersion   int                                `json:"resultVersion,omitempty"`
	ResultID        string                             `json:"resultId,omitempty"`
}

func NewAgentReview(owner, session, agent string, definition domain.AgentDefinition, modelRevision int64, now time.Time) (*OperationReview, error) {
	return newReview(owner, session, now, OperationReview{
		AgentCreation: &AgentCreation{AgentID: agent, Definition: definition, ModelRevision: modelRevision},
	})
}

func newReview(owner, session string, now time.Time, action OperationReview) (*OperationReview, error) {
	review := action
	review.ID = domain.NewID("chatop")
	expiry := now.Add(10 * time.Minute)
	if !review.ExpiresAt.IsZero() && review.ExpiresAt.Before(expiry) {
		expiry = review.ExpiresAt
	}
	review.ExpiresAt, review.State = expiry.UTC(), ReviewAwaiting
	digest, err := review.digest(owner, session)
	if err != nil {
		return nil, err
	}
	review.Digest = digest
	return &review, nil
}

func (r OperationReview) digest(owner, session string) (string, error) {
	choices := 0
	for _, exists := range []bool{r.TaskCorrection != nil, r.Skill != nil, r.PR != nil, r.AgentCreation != nil, r.Publication != nil, r.Adoption != nil, r.Workflow != nil, r.Configuration != nil, r.TaskDraft != nil, r.TaskStart != nil, r.TaskCancel != nil, r.TaskRetry != nil, r.Approval != nil, r.Guidance != nil, r.Instructions != nil, r.WorkflowControl != nil} {
		if exists {
			choices++
		}
	}
	if choices != 1 {
		return "", ErrReviewMismatch
	}
	var payload any
	switch {
	case r.TaskCancel != nil:
		payload = struct {
			Owner, Session, ID, Operation string
			ExpiresAt                     time.Time
			TaskCancel                    *TaskCancellation
		}{owner, session, r.ID, CancelTask, r.ExpiresAt, r.TaskCancel}
	case r.Approval != nil:
		payload = struct {
			Owner, Session, ID, Operation string
			ExpiresAt                     time.Time
			Approval                      *ApprovalDecision
		}{owner, session, r.ID, DecideApproval, r.ExpiresAt, r.Approval}
	case r.Adoption != nil:
		payload = struct {
			Owner, Session, ID, Operation string
			ExpiresAt                     time.Time
			Adoption                      *AgentAdoption
		}{owner, session, r.ID, AdoptAgentVersion, r.ExpiresAt, r.Adoption}
	case r.Publication != nil:
		payload = struct {
			Owner, Session, ID, Operation string
			ExpiresAt                     time.Time
			Publication                   *CatalogPublication
		}{owner, session, r.ID, PublishAgent, r.ExpiresAt, r.Publication}
	case r.WorkflowControl != nil:
		payload = struct {
			Owner, Session, ID, Operation string
			ExpiresAt                     time.Time
			WorkflowControl               *WorkflowControl
		}{owner, session, r.ID, ControlWorkflow, r.ExpiresAt, r.WorkflowControl}
	case r.TaskCorrection != nil:
		payload = struct {
			Owner, Session, ID, Operation string
			ExpiresAt                     time.Time
			Correction                    *TaskCorrection
		}{owner, session, r.ID, CorrectTask, r.ExpiresAt, r.TaskCorrection}
	case r.Skill != nil:
		payload = struct {
			Owner, Session, ID, Operation string
			ExpiresAt                     time.Time
			Skill                         *SkillChange
		}{owner, session, r.ID, SaveSkill, r.ExpiresAt, r.Skill}
	case r.PR != nil:
		payload = struct {
			Owner, Session, ID, Operation string
			ExpiresAt                     time.Time
			PR                            *PRAnalysis
		}{owner, session, r.ID, AnalyzePR, r.ExpiresAt, r.PR}
	case r.Instructions != nil:
		payload = struct {
			Owner, Session, ID, Operation string
			ExpiresAt                     time.Time
			Instructions                  *InstructionEdit
		}{owner, session, r.ID, EditInstructions, r.ExpiresAt, r.Instructions}
	case r.Guidance != nil:
		payload = struct {
			Owner, Session, ID, Operation string
			ExpiresAt                     time.Time
			Guidance                      *Guidance
		}{owner, session, r.ID, GuideNode, r.ExpiresAt, r.Guidance}
	case r.Configuration != nil:
		payload = struct {
			Owner, Session, ID, Operation string
			ExpiresAt                     time.Time
			Configuration                 *ConfigReview
		}{owner, session, r.ID, PlanInstallation, r.ExpiresAt, r.Configuration}
	case r.Workflow != nil:
		payload = struct {
			Owner, Session, Operation, ID string
			Workflow                      *WorkflowCreation
			ExpectedGraphVersion          int
			ExpiresAt                     time.Time
		}{
			Owner: owner, Session: session, Operation: CreateWorkflow, ID: r.ID,
			Workflow: r.Workflow, ExpectedGraphVersion: 0, ExpiresAt: r.ExpiresAt,
		}
	case r.AgentCreation != nil:
		// Keep the exact digest of already persisted catalog agent reviews.
		payload = struct {
			Owner, Session, Operation, ID, Agent   string
			Definition                             domain.AgentDefinition
			ExpectedCatalogRevision, ModelRevision int64
			ExpiresAt                              time.Time
		}{
			Owner: owner, Session: session, Operation: CreateAgent, ID: r.ID, Agent: r.AgentID,
			Definition: r.Definition, ExpectedCatalogRevision: 0, ModelRevision: r.ModelRevision, ExpiresAt: r.ExpiresAt,
		}
	case r.TaskDraft != nil:
		payload = struct {
			Owner, Session, Operation, ID string
			TaskDraft                     *TaskCreation
			ExpiresAt                     time.Time
		}{owner, session, CreateTask, r.ID, r.TaskDraft, r.ExpiresAt}
	case r.TaskStart != nil:
		payload = struct {
			Owner, Session, Operation, ID string
			TaskStart                     *TaskAdmission
			ExpiresAt                     time.Time
		}{owner, session, StartTask, r.ID, r.TaskStart, r.ExpiresAt}
	case r.TaskRetry != nil:
		payload = struct {
			Owner, Session, Operation, ID string
			TaskRetry                     *TaskRetry
			ExpiresAt                     time.Time
		}{owner, session, RetryTask, r.ID, r.TaskRetry, r.ExpiresAt}
	default:
		return "", ErrReviewMismatch
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

func (r OperationReview) ValidateConfirmation(owner, session, digest string, now time.Time) error {
	if err := r.validateDigest(owner, session, digest); err != nil {
		return err
	}
	if !now.Before(r.ExpiresAt) {
		return ErrExpiredReview
	}
	return nil
}

func (r OperationReview) validateDigest(owner, session, digest string) error {
	expected, err := r.digest(owner, session)
	if err != nil {
		return err
	}
	if expected != r.Digest || expected != digest {
		return ErrReviewMismatch
	}
	return nil
}

func (s Sessions) ClaimReview(ctx context.Context, owner, sessionID, reviewID, digest string) (*Session, *OperationReview, bool, error) {
	var review *OperationReview
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

func (s Sessions) FinishReview(ctx context.Context, owner, sessionID, reviewID, state string, version int) (*Session, error) {
	return s.finishReview(ctx, owner, sessionID, reviewID, state, version, "", nil)
}

func (s Sessions) FinishTaskReview(ctx context.Context, owner, sessionID, reviewID, taskID string) (*Session, error) {
	return s.finishReview(ctx, owner, sessionID, reviewID, ReviewCreated, 1, taskID, nil)
}

func (s Sessions) FinishConfigReview(ctx context.Context, owner, sessionID, reviewID string, receipts []ConfigReceipt, ok bool) (*Session, error) {
	state := ReviewApplied
	if !ok {
		state = ReviewIncomplete
	}
	return s.finishReview(ctx, owner, sessionID, reviewID, state, 0, "", receipts)
}

func (s Sessions) finishReview(ctx context.Context, owner, sessionID, reviewID, state string, version int, resultID string, receipts []ConfigReceipt) (*Session, error) {
	return s.update(ctx, owner, sessionID, func(session *Session) (bool, error) {
		for i := range session.Messages {
			message := &session.Messages[i]
			if review := message.Review; review != nil && review.ID == reviewID {
				recovered := state == ReviewCreated || (review.Skill != nil || review.Approval != nil || review.TaskCorrection != nil) && state == ReviewApplied
				if review.State != ReviewApplying && !(review.State == ReviewUnknown && recovered) {
					return false, nil
				}
				review.State, review.ResultVersion, review.ResultID = state, version, resultID
				review.ConfigReceipts = receipts
				message.Unread = true
				session.LastActivity = time.Now().UTC()
				return true, nil
			}
		}
		return false, storage.ErrNotFound
	})
}

func (s Sessions) DeclineReview(ctx context.Context, owner, sessionID, reviewID, digest string) (*Session, error) {
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
