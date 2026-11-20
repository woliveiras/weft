package jiradc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

type workflowClientStub struct {
	issue                           Issue
	discovery                       TransitionDiscovery
	authErr, issueErr, discoveryErr error
	sends                           int
}

func (c *workflowClientStub) Issue(context.Context, string) (Issue, error) {
	return c.issue, c.issueErr
}
func (c *workflowClientStub) DiscoverTransitions(context.Context, string) (TransitionDiscovery, error) {
	return c.discovery, c.discoveryErr
}
func (c *workflowClientStub) AuthorizeWrite(context.Context) error { return c.authErr }
func (c *workflowClientStub) SendTransition(context.Context, TransitionIntent) (bool, error) {
	c.sends++
	return true, nil
}

func workflowStub(t *testing.T) *workflowClientStub {
	t.Helper()
	c := &workflowClientStub{}
	if err := json.Unmarshal([]byte(fixtureIssue), &c.issue); err != nil {
		t.Fatal(err)
	}
	c.issue.Fields.Updated = "2026-11-20T12:00:00.000+0000"
	c.discovery = TransitionDiscovery{IssueID: c.issue.ID, IssueTypeID: c.issue.Fields.IssueType.ID, Current: Status{ID: c.issue.Fields.Status.ID}, Updated: c.issue.Fields.Updated,
		Transitions: []Transition{{ID: "31", Name: "Review", To: Status{ID: "20", Name: "Review"}, Fields: map[string]TransitionField{}}}}
	return c
}

func TestSharedPreparationAndSnapshotRecheckUseWorkflowClientContract(t *testing.T) {
	for _, kind := range []string{"success", "authority", "read", "type", "discovery", "metadata", "required fields", "snapshot"} {
		t.Run(kind, func(t *testing.T) {
			c := workflowStub(t)
			switch kind {
			case "authority":
				c.authErr = ErrWriteDisabled
			case "read":
				c.issueErr = errors.New("fixture read failed")
			case "type":
				c.issue.Fields.IssueType.ID = "999"
			case "discovery":
				c.discoveryErr = errors.New("fixture discovery failed")
			case "metadata":
				c.discovery.Transitions[0].Fields = nil
			case "required fields":
				required := true
				c.discovery.Transitions[0].Fields["resolution"] = TransitionField{Required: &required}
			}
			p, err := prepareTransition(t.Context(), c, intent())
			if kind != "success" && kind != "snapshot" {
				if err == nil || c.sends != 0 {
					t.Fatal("invalid preparation accepted", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind == "snapshot" {
				c.issue.Fields.Updated = "2026-11-20T12:01:00Z"
			}
			dispatched, err := sendPreparedTransition(t.Context(), c, intent(), p)
			if kind == "snapshot" {
				if err == nil || dispatched || c.sends != 0 {
					t.Fatal("changed snapshot sent", dispatched, err)
				}
			} else if err != nil || !dispatched || c.sends != 1 {
				t.Fatal(dispatched, err, c.sends)
			}
		})
	}
}

// This adapter deliberately reuses the real DC HTTP client under a distinct
// namespace and a second local authority. It tests the seam, not a Cloud API.
type namespacedWorkflowBackend struct {
	dcWorkflowBackend
	guardErr error
}

const testMappingPrefix = "fixture/workflow/mappings/"
const testSyncPrefix = "fixture/workflow/sync/"
const testAuthorityKey = "fixture/workflow/authority"

func (b namespacedWorkflowBackend) MappingPrefix() string { return testMappingPrefix }
func (b namespacedWorkflowBackend) SyncPrefix() string    { return testSyncPrefix }
func (b namespacedWorkflowBackend) SettingGuards(ctx context.Context, name string, revision int64) ([]storage.SettingRevision, error) {
	if b.guardErr != nil {
		return nil, b.guardErr
	}
	guards, err := b.dcWorkflowBackend.SettingGuards(ctx, name, revision)
	return append(guards, storage.SettingRevision{Key: testAuthorityKey, Revision: 1}), err
}
func namespacedBackend(t *testing.T, f *syncFixture) namespacedWorkflowBackend {
	t.Helper()
	if _, err := f.syncer.Connections.Store.PutSetting(t.Context(), testAuthorityKey, []byte(`{"revision":1}`), 0, "admin"); err != nil {
		t.Fatal(err)
	}
	return namespacedWorkflowBackend{dcWorkflowBackend: dcWorkflowBackend{f.syncer.Connections}}
}

func TestSharedBackendPreservesSeparateMappingAndSyncJournalsAcrossRestart(t *testing.T) {
	f := transitionFixture(t)
	b := namespacedBackend(t, f)
	mappings := WorkflowMappings{Backend: b}
	mapping, err := mappings.Put(t.Context(), mappingInput(), 0, "admin")
	if err != nil || mapping.Revision != 1 {
		t.Fatal(mapping, err)
	}
	if _, err = (WorkflowMappings{Connections: f.syncer.Connections}).Get(t.Context(), mapping.Name); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("backend mapping leaked into DC namespace", err)
	}
	v := intent()
	v.MappingName, v.MappingRevision = mapping.Name, mapping.Revision
	f.syncer.Backend = b
	f.mode.Store(1)
	record, err := f.syncer.Apply(t.Context(), v, "system:test")
	if err == nil || record.State != "unknown" || f.posts.Load() != 1 {
		t.Fatal(record, err)
	}
	if _, err = f.syncer.Connections.Store.GetSetting(t.Context(), syncKey(v.OperationID)); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("backend sync leaked into DC namespace", err)
	}
	// The same shared journal survives a real close/reopen and remains read-only
	// during reconciliation after its mapping has been disabled.
	if err = f.syncer.Connections.Store.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := fsstore.Open(f.root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	f.syncer.Connections.Store = st
	b.Connections.Store = st
	f.syncer.Backend = b
	mappings.Backend = b
	disabled := mapping.MappingInput
	disabled.Enabled = false
	if _, err = mappings.Put(t.Context(), disabled, mapping.Revision, "admin"); err != nil {
		t.Fatal(err)
	}
	f.mode.Store(0)
	record, err = f.syncer.Apply(t.Context(), v, "operator")
	if err != nil || record.State != "target_observed" || f.posts.Load() != 1 {
		t.Fatal("uncertain write was repeated", record, err)
	}
	items, err := mappings.List(t.Context())
	if err != nil || len(items) != 1 || items[0].Enabled {
		t.Fatal(items, err)
	}
}

type workflowAuthorityRaceStore struct {
	storage.Store
	prefix  string
	changed bool
}

func (s *workflowAuthorityRaceStore) PutSettingIf(ctx context.Context, key string, data []byte, expected int64, actor string, guards []storage.SettingRevision) (int64, error) {
	if !s.changed && strings.HasPrefix(key, s.prefix) {
		s.changed = true
		if _, err := s.Store.PutSetting(ctx, testAuthorityKey, []byte(`{"revision":2}`), 1, "admin"); err != nil {
			return 0, err
		}
	}
	return s.Store.PutSettingIf(ctx, key, data, expected, actor, guards)
}

func TestSharedBackendGuardsEveryAuthorityBeforePersistingMappingsOrDispatchReservations(t *testing.T) {
	for _, mode := range []string{"mapping race", "reservation race", "guard error"} {
		t.Run(mode, func(t *testing.T) {
			f := transitionFixture(t)
			b := namespacedBackend(t, f)
			prefix := testMappingPrefix
			if mode == "reservation race" {
				prefix = testSyncPrefix
			}
			if mode == "guard error" {
				b.guardErr = errors.New("fixture authority unavailable")
			} else {
				b.Connections.Store = &workflowAuthorityRaceStore{Store: f.syncer.Connections.Store, prefix: prefix}
			}
			f.syncer.Backend = b
			var err error
			if mode == "reservation race" {
				_, err = f.syncer.Apply(t.Context(), intent(), "system:test")
				if _, getErr := f.syncer.Get(t.Context(), intent().OperationID); !errors.Is(getErr, storage.ErrNotFound) {
					t.Fatal("unguarded intent persisted", getErr)
				}
			} else {
				m := WorkflowMappings{Backend: b}
				_, err = m.Put(t.Context(), mappingInput(), 0, "admin")
				if _, getErr := m.Get(t.Context(), mappingInput().Name); !errors.Is(getErr, storage.ErrNotFound) {
					t.Fatal("unguarded mapping persisted", getErr)
				}
			}
			if err == nil || (mode != "guard error" && !errors.Is(err, storage.ErrConflict)) || f.posts.Load() != 0 {
				t.Fatal("authority change bypassed CAS", err, f.posts.Load())
			}
		})
	}
}

func TestDefaultBackendRetainsLiteralDCKeysAndWriteChecks(t *testing.T) {
	f := transitionFixture(t)
	b := f.syncer.backend()
	if b.MappingPrefix() != MappingPrefix || b.SyncPrefix() != SyncPrefix || f.syncer.syncKey("existing") == "" || f.syncer.syncKey("existing") != syncKey("existing") {
		t.Fatal("legacy storage identity changed")
	}
	guards, err := b.SettingGuards(t.Context(), "jira-team", 1)
	if err != nil || len(guards) != 1 || guards[0] != (storage.SettingRevision{Key: ConnectionPrefix + "jira-team", Revision: 1}) {
		t.Fatal(guards, err)
	}
	c, err := b.Open(t.Context(), "jira-team", 1, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sent, err := c.SendTransition(t.Context(), intent()); !errors.Is(err, ErrWriteDisabled) || sent || f.posts.Load() != 0 {
		t.Fatal("read-only client wrote", sent, err)
	}
	denied := errors.New("fixture pinned mapping revoked")
	c, err = b.Open(t.Context(), "jira-team", 1, true, func(context.Context) error { return denied })
	if err != nil {
		t.Fatal(err)
	}
	if sent, err := c.SendTransition(t.Context(), intent()); !errors.Is(err, denied) || sent || f.posts.Load() != 0 {
		t.Fatal("write callback was bypassed", sent, err)
	}
}
