package mcpconfig

import (
	"context"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

type trackerRevisionReadFailure struct {
	storage.Store
	failure error
}

func (s trackerRevisionReadFailure) GetSetting(context.Context, string) (*storage.Setting, error) {
	return nil, s.failure
}

func TestTrackerRevisionPreservesStorageFailureAndClassifiesCompatibility(t *testing.T) {
	failure := errors.New("fixture setting read unavailable")
	api := TrackerAPI{Connections: New(trackerRevisionReadFailure{failure: failure}, nil, nil)}
	if _, err := api.Revision(t.Context(), "jira", "jira"); !errors.Is(err, failure) || errors.Is(err, ErrTrackerCompatibility) {
		t.Fatal("transient dependency failure became an incompatible credential", err)
	}
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager := New(store, nil, nil)
	api.Connections = manager
	if _, err := api.Revision(t.Context(), "missing", "jira"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("missing connection lost its storage identity", err)
	}
	if _, err := manager.Put(t.Context(), Input{ID: "disabled", Name: "Disabled Jira", Service: "jira", Endpoint: atlassianMCPEndpoint, Enabled: false}, 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Revision(t.Context(), "disabled", "jira"); !errors.Is(err, ErrTrackerCompatibility) {
		t.Fatal("known incompatible input lacked its dedicated classification", err)
	}
}
