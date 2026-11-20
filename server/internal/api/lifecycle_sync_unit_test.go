package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/trigger"
)

type lifecycleDetailReadStore struct {
	storage.Store
	job     *storage.Setting
	core    *storage.Setting
	coreErr error
}

func (s lifecycleDetailReadStore) GetSetting(_ context.Context, key string) (*storage.Setting, error) {
	if key == s.job.Key {
		return s.job, nil
	}
	return s.core, s.coreErr
}

func TestGetLifecycleSyncOnlyTreatsMissingCoreAsAbsent(t *testing.T) {
	now := time.Now().UTC()
	identity, err := json.Marshal([]string{"trg_fixture", "tsk_fixture", "pr_delivered"})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(identity)
	id := hex.EncodeToString(sum[:])
	job := trigger.LifecycleSync{ID: id, TriggerID: "trg_fixture", TaskID: "tsk_fixture", Stage: "pr_delivered",
		Evidence: "event-pr-opened", IssueID: "10007", IssueTypeID: "10002", IssueKey: "APP-7",
		Source:             trigger.SourceConfig{Kind: "jira-dc", ConnectionID: "native", SiteURL: "https://jira.example", ProjectID: "10001"},
		ConnectionRevision: 1, Mapping: trigger.MappingPin{Name: "reviewing", Revision: 1}, ConsentScope: "fixture-consent",
		Actor: "operator", State: "unknown", CreatedAt: now, UpdatedAt: now}
	encoded, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	readFailure := errors.New("core store unavailable")
	for _, tc := range []struct {
		name string
		core *storage.Setting
		err  error
		want error
	}{
		{name: "missing", err: storage.ErrNotFound},
		{name: "storage failure", err: readFailure, want: readFailure},
		{name: "corrupt retained evidence", core: &storage.Setting{Value: []byte(`{}`)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := lifecycleDetailReadStore{job: &storage.Setting{Key: trigger.LifecycleSyncPrefix + id, Value: encoded, Revision: 1}, core: tc.core, coreErr: tc.err}
			s := &Server{Deps: Deps{Store: store, IssueConnections: jiradc.Connections{Store: store}}}
			r := httptest.NewRequest("GET", "/api/v1/lifecycle-syncs/"+id, nil)
			r.SetPathValue("id", id)
			w := httptest.NewRecorder()
			err := s.getLifecycleSync(w, r)
			if tc.core != nil {
				if err == nil || w.Body.Len() != 0 {
					t.Fatalf("invalid core evidence became an absent or successful transition: %v %s", err, w.Body.String())
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if w.Body.Len() != 0 {
					t.Fatal("dependency failure wrote a successful body")
				}
				return
			}
			var detail map[string]json.RawMessage
			if json.Unmarshal(w.Body.Bytes(), &detail) != nil || w.Code != 200 || len(detail["sync"]) == 0 || detail["transition"] != nil {
				t.Fatalf("expected job with absent transition: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
