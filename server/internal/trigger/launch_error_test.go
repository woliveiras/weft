package trigger

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
)

func TestLaunchDiagnosticRetainsOnlyTrustedCause(t *testing.T) {
	secret := errors.New("token=fixture-secret at /private/workspaces/secret https://user:password@example.test")
	for _, stage := range []string{"project", "draft", "authorization", "review", "preparation", "build", "start", "approval", secret.Error()} {
		e := NewLaunchError(stage, secret, secret.Error(), "Select an enabled result agent.")
		if strings.Contains(e.Error(), "fixture-secret") || strings.Contains(e.Error(), "/private") || !strings.Contains(e.Error(), "Select an enabled result agent.") || !errors.Is(e, secret) {
			t.Fatalf("unsafe diagnostic: %s", e)
		}
	}
	for _, tc := range []struct {
		err  error
		want string
	}{{storage.ErrConflict, "changed"}, {storage.ErrNotFound, "unavailable"}, {context.DeadlineExceeded, "timed out"}, {context.Canceled, "cancelled"}} {
		if got := launchReason(tc.err); !strings.Contains(got, tc.want) {
			t.Fatalf("%v: %s", tc.err, got)
		}
	}
}
