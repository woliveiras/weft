package taskrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

// ValidateEnvironmentApproval binds a preparation decision to the exact saved
// task and authorization scope. It is also checked again before preparation.
func (m Manager) ValidateEnvironmentApproval(ctx context.Context, a *domain.Approval) error {
	var detail struct{ TaskID, Scope string }
	if a.Kind != domain.ApprovalTaskEnvironment || json.Unmarshal(a.Detail, &detail) != nil {
		return errors.New("invalid task environment approval")
	}
	d, err := (taskconfig.Manager{Store: m.Store}).Get(ctx, detail.TaskID)
	if err != nil {
		return err
	}
	if d.Status != "draft" || d.EnvironmentApprovalID != a.ID || a.GraphID != d.GraphID || (a.ExpiresAt != nil && !time.Now().Before(*a.ExpiresAt)) {
		return storage.ErrConflict
	}
	review, err := (taskauth.Manager{Store: m.Store}).Review(ctx, d.ID)
	if err != nil {
		return err
	}
	if review.Scope != detail.Scope || review.Record == nil || review.Record.Scope != detail.Scope {
		return storage.ErrConflict
	}
	return nil
}

func (m Manager) checkEnvironmentApproval(ctx context.Context, d *taskconfig.Draft) error {
	if d.EnvironmentApprovalID == "" {
		return nil
	}
	a, err := m.Store.GetApproval(ctx, d.EnvironmentApprovalID)
	if err != nil {
		return err
	}
	if err := m.ValidateEnvironmentApproval(ctx, a); err != nil {
		return err
	}
	if a.Status != domain.ApprovalApproved || a.DecidedBy == "system:timeout" {
		return fmt.Errorf("%w: task environment preparation requires approval", taskconfig.ErrInvalid)
	}
	return nil
}
