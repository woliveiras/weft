package chat

import (
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

const DecideApproval = "approval.decide"

type ApprovalDecisionDraft struct {
	Approval string
	Decision string
	Answer   string
	Comment  string
}

func (d ApprovalDecisionDraft) valid(task string) bool {
	if (d.Approval == "") == (task == "") || (d.Approval != "" && !shortLabel(d.Approval)) ||
		(task != "" && !shortLabel(task)) || !safeText(d.Answer, 1200) || !safeText(d.Comment, 600) {
		return false
	}
	return d.Decision == "approve" || (d.Decision == "reject" && d.Answer == "")
}

type ApprovalDecision struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	GraphID     string              `json:"graphId"`
	TaskID      string              `json:"taskId,omitempty"`
	NodeID      string              `json:"nodeId,omitempty"`
	ExecutionID string              `json:"executionId,omitempty"`
	Kind        domain.ApprovalKind `json:"kind"`
	Replica     int                 `json:"replica"`
	Decision    string              `json:"decision"`
	Answer      string              `json:"answer,omitempty"`
	Comment     string              `json:"comment,omitempty"`
	Fingerprint string              `json:"fingerprint"`
}

func NewApprovalDecisionReview(owner, session string, decision ApprovalDecision, nativeExpiry *time.Time, now time.Time) (*OperationReview, error) {
	input := ApprovalDecisionDraft{Approval: decision.ID, Decision: decision.Decision, Answer: decision.Answer, Comment: decision.Comment}
	if !input.valid("") || !safeText(decision.Title, 1200) || decision.Fingerprint == "" ||
		(nativeExpiry != nil && !now.Before(*nativeExpiry)) {
		return nil, ErrInvalidPlan
	}
	switch decision.Kind {
	case domain.ApprovalMessage, domain.ApprovalTurn:
		if decision.Answer != "" {
			return nil, ErrInvalidPlan
		}
	case domain.ApprovalQuestion:
		if decision.Decision == "approve" && decision.Answer == "" {
			return nil, ErrInvalidPlan
		}
	default:
		return nil, ErrInvalidPlan
	}
	r := OperationReview{Approval: &decision}
	if nativeExpiry != nil {
		r.ExpiresAt = *nativeExpiry
	}
	return newReview(owner, session, now, r)
}
