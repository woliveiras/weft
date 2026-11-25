package taskconfig

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

type deleteStore struct {
	storage.Store
	draft   Draft
	fail    string
	writes  int
	written Draft
	event   *domain.Event
}

func (s *deleteStore) GetSetting(context.Context, string) (*storage.Setting, error) {
	if s.fail == "read" {
		return nil, errors.New("store read unavailable")
	}
	b, _ := json.Marshal(s.draft)
	return &storage.Setting{Value: b, Revision: s.draft.Revision}, nil
}
func (s *deleteStore) AppendEvent(_ context.Context, e *domain.Event) error {
	if s.fail == "audit" {
		return errors.New("audit unavailable")
	}
	s.event = e
	return nil
}
func (s *deleteStore) PutSetting(_ context.Context, _ string, b []byte, rev int64, _ string) (int64, error) {
	s.writes++
	if s.fail == "write" {
		return 0, errors.New("store write unavailable")
	}
	if rev != s.draft.Revision {
		return 0, storage.ErrConflict
	}
	if err := json.Unmarshal(b, &s.written); err != nil {
		return 0, err
	}
	return rev + 1, nil
}
func TestDeleteDraftEligibilityAndMinimalTombstone(t *testing.T) {
	for _, tc := range []struct {
		name     string
		draft    Draft
		revision int64
		want     error
	}{
		{"ordinary", Draft{ID: "tsk_a", Status: "draft", Revision: 4, Input: good()}, 4, nil},
		{"invalid proposal", Draft{ID: "tsk_a", Status: "draft", Revision: 4, Submitted: true, Resolution: &Resolution{State: "invalid_proposal"}}, 4, nil},
		{"stale", Draft{ID: "tsk_a", Status: "draft", Revision: 4}, 3, storage.ErrConflict},
		{"no revision", Draft{ID: "tsk_a", Status: "draft", Revision: 4}, 0, storage.ErrConflict},
		{"running", Draft{ID: "tsk_a", Status: "running", Revision: 4}, 4, storage.ErrConflict},
		{"returned to draft with execution", Draft{ID: "tsk_a", Status: "draft", Revision: 4, ExecutionGraphID: "task-run-a"}, 4, storage.ErrConflict},
		{"environment approval", Draft{ID: "tsk_a", Status: "draft", Revision: 4, EnvironmentApprovalID: "apr_a"}, 4, storage.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &deleteStore{draft: tc.draft}
			err := (Manager{Store: s}).DeleteDraft(t.Context(), "tsk_a", tc.revision, "operator")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if tc.want != nil {
				if s.writes != 0 {
					t.Fatal("invalid deletion wrote")
				}
				return
			}
			if s.written.Status != "deleted" || s.written.ID != "tsk_a" || s.written.UpdatedBy != "operator" || s.written.UpdatedAt.IsZero() {
				t.Fatalf("missing tombstone: %+v", s.written)
			}
			if s.written.Request != "" || s.written.Title != "" || s.written.GraphID != "" || s.written.Resolution != nil {
				t.Fatalf("request retained in tombstone: %+v", s.written)
			}
			if s.event == nil || s.event.Type != "task.transition_requested" {
				t.Fatal("deletion intent not audited")
			}
		})
	}
}
func TestDeleteDraftPropagatesFailures(t *testing.T) {
	for _, fail := range []string{"read", "audit", "write"} {
		t.Run(fail, func(t *testing.T) {
			s := &deleteStore{draft: Draft{ID: "tsk_a", Status: "draft", Revision: 1}, fail: fail}
			if err := (Manager{Store: s}).DeleteDraft(t.Context(), "tsk_a", 1, "operator"); err == nil {
				t.Fatal("failure appeared successful")
			}
			if fail != "write" && s.writes != 0 {
				t.Fatal("mutation after failed prerequisite")
			}
		})
	}
}
