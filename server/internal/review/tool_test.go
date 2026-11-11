package review

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

type deniedCommandApproval struct{ called bool }

func (a *deniedCommandApproval) RequestApproval(_ context.Context, request *domain.Approval) (*domain.Approval, error) {
	a.called = true
	request.Status = domain.ApprovalRejected
	return request, nil
}

func TestPreviewCommandCannotBypassShellPolicy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		grants   []domain.ToolGrant
		approval bool
	}{
		{name: "no shell grant"},
		{name: "command not allowed", grants: []domain.ToolGrant{{Name: "shell.exec", Commands: []string{"go"}}}},
		{name: "missing approver", grants: []domain.ToolGrant{{Name: "shell.exec", RequiresApproval: true}}},
		{name: "human rejection", grants: []domain.ToolGrant{{Name: "shell.exec", RequiresApproval: true}}, approval: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := New(Options{}) // authorization must fail before any storage access
			invocation := &tools.Invocation{Principal: &tools.Principal{Grants: tc.grants}, Args: json.RawMessage(`{"title":"Review","instructions":"Inspect","command":["npm","run","dev"],"port":5173}`)}
			approver := &deniedCommandApproval{}
			if tc.approval {
				invocation.Approver = approver
			}
			_, err := manager.Tool().Invoke(context.Background(), invocation)
			want := tools.ErrDenied
			if tc.approval {
				want = tools.ErrRejected
			}
			if !errors.Is(err, want) {
				t.Fatal(err)
			}
			if approver.called != tc.approval {
				t.Fatal("incorrect approval handling")
			}
		})
	}
}
