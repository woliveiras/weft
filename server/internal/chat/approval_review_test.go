package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestApprovalReviewBindsTheAnswerAndTheEarlierNativeExpiry(t *testing.T) {
	t.Parallel()
	plan, err := decodePlan(`{"kind":"approval.decide","approvalId":"apr_question","approvalDecision":"approve","approvalAnswer":"Use the current template."}`)
	if err != nil || plan.Kind != DecideApproval || plan.Approval.Answer != "Use the current template." {
		t.Fatalf("approval plan = %+v %v", plan, err)
	}
	now := time.Now()
	expires := now.Add(time.Minute)
	d := ApprovalDecision{
		ID: "apr_question", Title: "Which template should I use?", Kind: domain.ApprovalQuestion,
		Decision: "approve", Answer: plan.Approval.Answer, Fingerprint: "native-request",
	}
	review, err := NewApprovalDecisionReview("owner", "session", d, &expires, now)
	if err != nil || !review.ExpiresAt.Equal(expires) {
		t.Fatalf("review effective expiry = %+v %v", review, err)
	}
	if err := review.ValidateConfirmation("owner", "session", review.Digest, expires); !errors.Is(err, ErrExpiredReview) {
		t.Fatalf("native expiry did not close confirmation: %v", err)
	}
	review.Approval.Answer = "Use another template."
	if err := review.ValidateConfirmation("owner", "session", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("answer changed after review: %v", err)
	}
	d.Kind = domain.ApprovalMessage
	if _, err := NewApprovalDecisionReview("owner", "session", d, nil, now); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("review allowed an edited held payload: %v", err)
	}
	if _, err := decodePlan(`{"kind":"approval.decide","task":"tsk_one","approvalId":"apr_another","approvalDecision":"approve"}`); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("two targets were accepted: %v", err)
	}
}
