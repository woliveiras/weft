package engine

import (
	"context"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

type idleUnitSandbox struct{ tools.ProcessSandbox }

func (*idleUnitSandbox) Suspend(context.Context) error { return nil }
func (*idleUnitSandbox) Start(context.Context) error   { return nil }

func TestSandboxIdleEligibility(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		state                      domain.ReplicaState
		timeout                    time.Duration
		suspended, retry, eligible bool
	}{
		{"idle", domain.ReplicaIdle, time.Second, false, false, true},
		{"paused", domain.ReplicaPaused, time.Second, false, false, true},
		{"running", domain.ReplicaRunning, time.Second, false, false, false},
		{"approval", domain.ReplicaAwaitingApproval, time.Second, false, false, false},
		{"disabled", domain.ReplicaIdle, 0, false, false, false},
		{"already stopped", domain.ReplicaIdle, time.Second, true, false, false},
		{"retry backoff", domain.ReplicaIdle, time.Second, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &replica{run_: &graphRun{e: &Engine{opt: Options{SandboxIdleTimeout: tc.timeout}}}, sandbox: &idleUnitSandbox{}, state: tc.state, idleSince: time.Now().Add(-time.Minute), sandboxSuspended: tc.suspended}
			if tc.retry {
				r.suspendRetry = time.Now().Add(time.Minute)
			}
			if got := r.sandboxIdleDelay() == 0; got != tc.eligible {
				t.Fatalf("eligible=%v want %v", got, tc.eligible)
			}
		})
	}
}
