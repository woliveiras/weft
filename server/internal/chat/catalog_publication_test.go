package chat

import (
	"errors"
	"testing"
	"time"
)

func TestCatalogPublicationBindsSharedInstructionsAndNativeRevision(t *testing.T) {
	t.Parallel()
	plan, err := decodePlan(`{"kind":"agent.publish","publishAgent":"Editorial reader","sharedInstructions":"Read the revised notes."}`)
	if err != nil || plan.Kind != PublishAgent || plan.Publication.Agent != "Editorial reader" {
		t.Fatalf("publication plan: %+v %v", plan, err)
	}
	now := time.Now()
	p := CatalogPublication{
		ID: "reader", Name: "Editorial reader", PreviousVersion: 1, Revision: 1,
		DefinitionDigest: "reviewed-definition", Before: "Read notes.", After: plan.Publication.Instructions,
	}
	review, err := NewCatalogPublicationReview("owner", "session", p, now)
	if err != nil {
		t.Fatal(err)
	}
	review.Publication.Revision++
	if err := review.ValidateConfirmation("owner", "session", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("changed catalog revision passed: %v", err)
	}
	p.Before = "password=fixture-text"
	if _, err := NewCatalogPublicationReview("owner", "session", p, now); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("unsafe instruction persisted in the preview: %v", err)
	}
	if _, err := decodePlan(`{"kind":"agent.publish","publishAgent":"Editorial reader","sharedInstructions":"Read.","instructionWorkflow":"workflow"}`); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("mixed publication and local edit passed: %v", err)
	}
}
