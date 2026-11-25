package taskconfig

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

type retryStore struct {
	storage.Store
	rows   map[string]*storage.Setting
	fail   string
	writes int
	events []*domain.Event
}

func retryFixture() *retryStore {
	d := Draft{ID: "tsk_source", Revision: 7, Status: "failed", Input: good(), Submitted: true, Resolution: &Resolution{State: "resolved"}, ExecutionGraphID: "task-run-source", EnvironmentApprovalID: "apr_old", LaunchError: "prior error"}
	d.ResultNodeID = "reviewer"
	d.ChannelID, d.MessageType, d.PayloadJSON = "requests", "work.request", `{"text":"request"}`
	d.ProjectID, d.BaseCommit, d.SCMConnection, d.ExecutionMode = "prj_fixture", strings.Repeat("a", 40), "repo", "sandbox"
	d.Source = &IssueSource{Kind: "github", ConnectionID: "issues", Reference: "owner/repo#1"}
	b, _ := json.Marshal(d)
	return &retryStore{rows: map[string]*storage.Setting{prefix + d.ID: {Value: b, Revision: d.Revision}}}
}

func (s *retryStore) GetSetting(_ context.Context, key string) (*storage.Setting, error) {
	if s.fail == "read" {
		return nil, errors.New("read unavailable")
	}
	row, ok := s.rows[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *retryStore) PutSetting(_ context.Context, key string, value []byte, revision int64, _ string) (int64, error) {
	s.writes++
	if s.fail == "write" {
		return 0, errors.New("write unavailable")
	}
	if _, exists := s.rows[key]; exists || revision != 0 {
		return 0, storage.ErrConflict
	}
	s.rows[key] = &storage.Setting{Value: value, Revision: 1}
	if s.fail == "lost response" {
		return 0, errors.New("response lost after commit")
	}
	return 1, nil
}
func (s *retryStore) AppendEvent(_ context.Context, event *domain.Event) error {
	if s.fail == "audit" {
		return errors.New("audit unavailable")
	}
	s.events = append(s.events, event)
	return nil
}

func TestRetryFailedCopiesOnlyInputAndServerProvenance(t *testing.T) {
	s := retryFixture()
	m := Manager{Store: s}
	source, _ := m.Get(t.Context(), "tsk_source")
	before := string(s.rows[prefix+source.ID].Value)
	d, created, err := m.RetryFailed(t.Context(), source.ID, source.Revision, "operator")
	if err != nil || !created {
		t.Fatal(d, created, err)
	}
	if !reflect.DeepEqual(d.Input, source.Input) || d.ID == source.ID || !validID.MatchString(d.ID) || d.Status != "draft" || d.Revision != 1 || d.RetryOfTaskID != source.ID || d.RetryOfRevision != source.Revision {
		t.Fatalf("unexpected retry snapshot: %+v", d)
	}
	if d.Submitted || d.Resolution != nil || d.TriggerIssueOrigin != nil || d.TriggerGitHubIssue != "" || d.ExecutionGraphID != "" || d.EnvironmentApprovalID != "" || d.LaunchError != "" || d.ObservedState != "" || d.CreatedBy != "operator" || d.CreatedAt.IsZero() {
		t.Fatalf("retry inherited lifecycle or authority: %+v", d)
	}
	if before != string(s.rows[prefix+source.ID].Value) || len(s.events) != 1 || s.events[0].Type != "task.retry_requested" || s.events[0].Actor != "operator" {
		t.Fatal("source changed or audit missing", s.events)
	}
	if strings.Contains(string(s.events[0].Data), source.Request) {
		t.Fatal("request content leaked to retry audit")
	}
	d.Source.Reference = "owner/repo#2"
	if source.Source.Reference != "owner/repo#1" {
		t.Fatal("input pointers aliased")
	}
}

func TestRetryFailedEligibilityAndRevision(t *testing.T) {
	for _, status := range []string{"draft", "running", "succeeded", "blocked", "cancelled", "interrupted", "environment_pending"} {
		t.Run(status, func(t *testing.T) {
			s := retryFixture()
			m := Manager{Store: s}
			d, _ := m.Get(t.Context(), "tsk_source")
			d.Status = status
			s.rows[prefix+d.ID].Value, _ = json.Marshal(d)
			if _, _, err := m.RetryFailed(t.Context(), d.ID, 7, "operator"); !errors.Is(err, storage.ErrConflict) || s.writes != 0 {
				t.Fatal("ineligible retry", err)
			}
		})
	}
	for _, revision := range []int64{0, 6, 8} {
		s := retryFixture()
		if _, _, err := (Manager{Store: s}).RetryFailed(t.Context(), "tsk_source", revision, "operator"); !errors.Is(err, storage.ErrConflict) || s.writes != 0 {
			t.Fatal("stale retry", err)
		}
	}
	for _, legacy := range []bool{false, true} {
		s := retryFixture()
		m := Manager{Store: s}
		d, _ := m.Get(t.Context(), "tsk_source")
		if legacy {
			d.TriggerGitHubIssue = "owner/repo#1"
		} else {
			d.TriggerIssueOrigin = &IssueOrigin{Kind: "jira", Key: "SCRUM-5"}
		}
		s.rows[prefix+d.ID].Value, _ = json.Marshal(d)
		if _, _, err := m.RetryFailed(t.Context(), d.ID, 7, "operator"); !errors.Is(err, storage.ErrConflict) || s.writes != 0 {
			t.Fatal("trigger retry", err)
		}
	}
	if _, _, err := (Manager{Store: retryFixture()}).RetryFailed(t.Context(), "tsk_unknown", 7, "operator"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRetryFailedReplayAndFailures(t *testing.T) {
	for _, failure := range []string{"read", "audit", "write", "lost response"} {
		t.Run(failure, func(t *testing.T) {
			s := retryFixture()
			s.fail = failure
			m := Manager{Store: s}
			if _, created, err := m.RetryFailed(t.Context(), "tsk_source", 7, "operator"); err == nil || created {
				t.Fatal("failure appeared successful", created, err)
			}
			if (failure == "read" || failure == "audit") && s.writes != 0 {
				t.Fatal("write after dependency failure")
			}
			s.fail = ""
			d, created, err := m.RetryFailed(t.Context(), "tsk_source", 7, "operator")
			if err != nil || created != (failure != "lost response") {
				t.Fatal(d, created, err)
			}
			d.Status = "running"
			d.Title = "Edited child"
			s.rows[prefix+d.ID].Value, _ = json.Marshal(d)
			replayed, created, err := m.RetryFailed(t.Context(), "tsk_source", 7, "another-operator")
			if err != nil || created || replayed.ID != d.ID || replayed.Title != d.Title || replayed.Status != "running" {
				t.Fatal("replay reset child", replayed, err)
			}
			d.Status = "failed"
			s.rows[prefix+d.ID].Value, _ = json.Marshal(d)
			grandchild, created, err := m.RetryFailed(t.Context(), d.ID, d.Revision, "operator")
			if err != nil || !created || grandchild.ID == d.ID || grandchild.RetryOfTaskID != d.ID || grandchild.RetryOfRevision != d.Revision {
				t.Fatal("failed retry did not form a fresh linked attempt", grandchild, err)
			}
			d.Status = "deleted"
			d.RetryOfTaskID = ""
			s.rows[prefix+d.ID].Value, _ = json.Marshal(d)
			if _, _, err := m.RetryFailed(t.Context(), "tsk_source", 7, "operator"); !errors.Is(err, storage.ErrConflict) {
				t.Fatal("deleted retry resurrected", err)
			}
		})
	}
}
