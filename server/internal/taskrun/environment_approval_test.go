package taskrun

import (
	"context"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type unavailableApprovalStore struct {
	storage.Store
	calls int
}

func (s *unavailableApprovalStore) GetApproval(context.Context, string) (*domain.Approval, error) {
	s.calls++
	return nil, errors.New("approval store unavailable")
}
func TestEnvironmentPreparationApprovalFailsClosed(t *testing.T) {
	s := &unavailableApprovalStore{}
	m := Manager{Store: s}
	if err := m.checkEnvironmentApproval(t.Context(), &taskconfig.Draft{}); err != nil || s.calls != 0 {
		t.Fatal(err, s.calls)
	}
	if err := m.checkEnvironmentApproval(t.Context(), &taskconfig.Draft{EnvironmentApprovalID: "apr_missing"}); err == nil || s.calls != 1 {
		t.Fatal(err, s.calls)
	}
	for _, a := range []*domain.Approval{{Kind: domain.ApprovalTool}, {Kind: domain.ApprovalTaskEnvironment, Detail: []byte("{")}} {
		if err := m.ValidateEnvironmentApproval(t.Context(), a); err == nil {
			t.Fatal("invalid decision accepted")
		}
	}
}
