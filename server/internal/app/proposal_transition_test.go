package app

import (
	"errors"
	"fmt"
	"testing"

	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestRetryProposalGraphTransition(t *testing.T) {
	busy := fmt.Errorf("graph fixture: %w", engine.ErrGraphTransitionBusy)
	if err := retryProposalGraphTransition(busy); !errors.Is(err, storage.ErrConflict) || !errors.Is(err, busy) {
		t.Fatalf("graph lease conflict was not translated: %v", err)
	}
	if err := retryProposalGraphTransition(engine.ErrConflict); !errors.Is(err, engine.ErrConflict) || errors.Is(err, storage.ErrConflict) {
		t.Fatalf("permanent graph conflict was classified as retryable: %v", err)
	}
}
