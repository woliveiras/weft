package storage

import (
	"fmt"
	"reflect"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

// ValidateReviewedApproval compares the complete native request, including
// its pending state and expiry, immediately before a guarded decision.
func ValidateReviewedApproval(current, expected *domain.Approval, now time.Time) error {
	if current == nil || current.Status != domain.ApprovalPending || !reflect.DeepEqual(current, expected) ||
		(current.ExpiresAt != nil && !now.Before(*current.ExpiresAt)) {
		return fmt.Errorf("%w: approval changed or expired", ErrConflict)
	}
	return nil
}
