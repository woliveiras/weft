package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/trigger"
)

type failedLaunchStore struct {
	storage.Store
	writes int
}

func (s *failedLaunchStore) GetSetting(context.Context, string) (*storage.Setting, error) {
	return nil, errors.New("token=fixture-secret /private/store")
}
func (s *failedLaunchStore) PutSetting(context.Context, string, []byte, int64, string) (int64, error) {
	s.writes++
	return 0, nil
}
func TestTriggerLaunchDiagnosticPersistenceFailureIsSafe(t *testing.T) {
	s := &failedLaunchStore{}
	x := triggerTasks{a: &App{Store: s}}
	err := x.recordLaunchFailure(t.Context(), "tsk_test", "admin", trigger.NewLaunchError("start", errors.New("private cause")))
	if err == nil || !strings.Contains(err.Error(), "could not be persisted") || strings.Contains(err.Error(), "fixture-secret") || s.writes != 0 {
		t.Fatal(err, s.writes)
	}
}
