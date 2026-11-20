package jiradc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func mappingInput() MappingInput {
	return MappingInput{Name: "review-step", Connection: "jira-team", ConnectionRevision: 1, SampleIssueKey: "APP-7", SampleIssueID: "10007", IssueTypeID: "10002", FromStatusID: "10", TransitionID: "31", TargetStatusID: "20", IssueUpdated: "2026-11-20T12:00:00.000+0000", Enabled: true}
}
func TestWorkflowMappingInputRequiresActualIdentifiersAndDiscoveryRevision(t *testing.T) {
	if err := mappingInput().validate(); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*MappingInput){func(v *MappingInput) { v.Name = "../mapping" }, func(v *MappingInput) { v.ConnectionRevision = 0 }, func(v *MappingInput) { v.IssueUpdated = "" }, func(v *MappingInput) { v.TransitionID = "In Review" }, func(v *MappingInput) { v.TargetStatusID = v.FromStatusID }} {
		v := mappingInput()
		edit(&v)
		if !errors.Is(v.validate(), ErrInvalid) {
			t.Fatal(v)
		}
	}
}
func TestWorkflowMappingUsesScopedDiscoveryAndCASWithoutTransitionWrites(t *testing.T) {
	f := transitionFixture(t)
	m := WorkflowMappings{Connections: f.syncer.Connections}
	v, err := m.Put(t.Context(), mappingInput(), 0, "admin")
	if err != nil || v.Revision != 1 || v.TransitionName != "Revisão" || v.TargetStatusName != "Review" || v.ProjectID != "10001" || f.posts.Load() != 0 {
		t.Fatal(v, err)
	}
	// Names are presentation only; mapping contains source and destination IDs.
	if v.FromStatusID != "10" || v.TargetStatusID != "20" || v.TransitionID != "31" {
		t.Fatal(v)
	}
	if _, err = m.Put(t.Context(), mappingInput(), 0, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("mapping overwritten without revision", err)
	}
	list, err := m.List(t.Context())
	if err != nil || len(list) != 1 || list[0].Revision != 1 {
		t.Fatal(list, err)
	}
	// A changed sample snapshot cannot be adopted silently.
	changed := mappingInput()
	changed.IssueUpdated = "2026-11-20T12:01:00.000+0000"
	if _, err = m.Put(t.Context(), changed, 1, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("stale discovery accepted", err)
	}
	c, _ := m.Connections.Get(t.Context(), v.Connection)
	if _, err = m.Connections.Put(t.Context(), ConnectionInput{Name: c.Name, Kind: c.Kind, SiteURL: c.SiteURL, ProjectID: c.ProjectID, Enabled: false, ProjectRestricted: true}, c.Revision, "admin"); err != nil {
		t.Fatal(err)
	}
	before := f.issueReads.Load()
	disable := v.MappingInput
	disable.Enabled = false
	v, err = m.Put(t.Context(), disable, 1, "admin")
	if err != nil || v.Enabled || v.Revision != 2 || f.issueReads.Load() != before {
		t.Fatal("disable required remote access", v, err)
	}
	disable.Enabled = true
	if _, err = m.Put(t.Context(), disable, 2, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("re-enabled obsolete connection revision", err)
	}
	if f.posts.Load() != 0 {
		t.Fatal("mapping save mutated Jira")
	}
}
func TestWorkflowMappingRejectsMissingTransitionChangedTargetAndUnsupportedRequiredFields(t *testing.T) {
	for _, mode := range []int32{3, 4, 5, 7, 8, 9, 10} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			f := transitionFixture(t)
			f.mode.Store(mode)
			m := WorkflowMappings{Connections: f.syncer.Connections}
			_, err := m.Put(t.Context(), mappingInput(), 0, "admin")
			if err == nil || strings.Contains(err.Error(), "sync-fixture-token") || f.posts.Load() != 0 {
				t.Fatal("invalid mapping accepted", err)
			}
			if _, err = m.Get(t.Context(), "review-step"); !errors.Is(err, storage.ErrNotFound) {
				t.Fatal("failed mapping persisted", err)
			}
		})
	}
}

type mappingFaultStore struct {
	storage.Store
	failAudit, failSave bool
	afterRead           func()
	readOnce            atomic.Bool
}

func (s *mappingFaultStore) AppendEvent(ctx context.Context, v *domain.Event) error {
	if s.failAudit && v.Type == "tracker.workflow_mapping_change_requested" {
		return errors.New("fixture audit unavailable")
	}
	return s.Store.AppendEvent(ctx, v)
}
func (s *mappingFaultStore) PutSettingIf(ctx context.Context, key string, value []byte, revision int64, actor string, guards []storage.SettingRevision) (int64, error) {
	if s.failSave && strings.HasPrefix(key, MappingPrefix) {
		return 0, errors.New("fixture storage unavailable")
	}
	return s.Store.PutSettingIf(ctx, key, value, revision, actor, guards)
}
func (s *mappingFaultStore) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	row, err := s.Store.GetSetting(ctx, key)
	if err == nil && key == ConnectionPrefix+"jira-team" && s.afterRead != nil && s.readOnce.CompareAndSwap(false, true) {
		s.afterRead()
	}
	return row, err
}
func TestWorkflowMappingStorageFailuresNeverReportASavedDefinition(t *testing.T) {
	for _, audit := range []bool{false, true} {
		t.Run(fmt.Sprint(audit), func(t *testing.T) {
			f := transitionFixture(t)
			fault := &mappingFaultStore{Store: f.syncer.Connections.Store, failAudit: audit, failSave: !audit}
			c := f.syncer.Connections
			c.Store = fault
			m := WorkflowMappings{Connections: c}
			if _, err := m.Put(t.Context(), mappingInput(), 0, "admin"); err == nil {
				t.Fatal("failed save appeared successful")
			}
			if _, err := m.Get(t.Context(), "review-step"); !errors.Is(err, storage.ErrNotFound) || f.posts.Load() != 0 {
				t.Fatal("failed save wrote a mapping or Jira", err)
			}
		})
	}
}
func TestWorkflowMappingConnectionRotationBetweenReadsCannotMixAudiences(t *testing.T) {
	f := transitionFixture(t)
	base := f.syncer.Connections
	fault := &mappingFaultStore{Store: base.Store, afterRead: func() {
		c, err := base.Get(t.Context(), "jira-team")
		if err != nil {
			t.Error(err)
			return
		}
		token := "replacement-fixture-token"
		_, err = base.Put(t.Context(), ConnectionInput{Name: c.Name, Kind: c.Kind, SiteURL: c.SiteURL, ProjectID: c.ProjectID, Enabled: true, ProjectRestricted: true, Token: &token}, c.Revision, "admin")
		if err != nil {
			t.Error(err)
		}
	}}
	c := base
	c.Store = fault
	m := WorkflowMappings{Connections: c}
	in := mappingInput()
	in.ConnectionRevision = 2
	if _, err := m.Put(t.Context(), in, 0, "admin"); !errors.Is(err, storage.ErrConflict) || f.issueReads.Load() != 0 {
		t.Fatal("mixed connection revisions", err)
	}
	actual, err := base.Get(t.Context(), "jira-team")
	if err != nil || actual.Revision != 2 {
		t.Fatal("rotation fixture did not run", err)
	}
}
func TestWorkflowMappingConnectionChangeDuringDiscoveryFailsAtomicSave(t *testing.T) {
	f := transitionFixture(t)
	base := f.syncer.Connections
	f.beforeMetadata = func() {
		c, err := base.Get(t.Context(), "jira-team")
		if err != nil {
			t.Error(err)
			return
		}
		_, err = base.Put(t.Context(), ConnectionInput{Name: c.Name, Kind: c.Kind, SiteURL: c.SiteURL, ProjectID: c.ProjectID, Enabled: false, ProjectRestricted: true}, c.Revision, "admin")
		if err != nil {
			t.Error(err)
		}
	}
	m := WorkflowMappings{Connections: base}
	if _, err := m.Put(t.Context(), mappingInput(), 0, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("changed connection saved mapping", err)
	}
	if _, err := m.Get(t.Context(), "review-step"); !errors.Is(err, storage.ErrNotFound) || f.posts.Load() != 0 {
		t.Fatal(err)
	}
}
func TestWorkflowMappingPersistenceAndCorruptRowsFailClosed(t *testing.T) {
	for _, mode := range []string{"reopen", "json", "name", "scope", "id"} {
		t.Run(mode, func(t *testing.T) {
			f := transitionFixture(t)
			m := WorkflowMappings{Connections: f.syncer.Connections}
			v, err := m.Put(t.Context(), mappingInput(), 0, "admin")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "reopen" {
				if err = m.Connections.Store.Close(); err != nil {
					t.Fatal(err)
				}
				st, err := fsstore.Open(f.root)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { st.Close() })
				m.Connections.Store = st
				found, err := m.Get(t.Context(), v.Name)
				if err != nil || found.Revision != 1 || found.MappingInput != v.MappingInput {
					t.Fatal(found, err)
				}
				return
			}
			if mode == "name" {
				in := mappingInput()
				in.Name = "other-step"
				if _, err = m.Put(t.Context(), in, 0, "admin"); err != nil {
					t.Fatal(err)
				}
				v.Name = "other-step"
			}
			if mode == "scope" {
				v.SiteURL = "http://jira.example"
			}
			if mode == "id" {
				v.TransitionID = "Review"
			}
			raw, _ := json.Marshal(v)
			if mode == "json" {
				raw = []byte("invalid")
			}
			if _, err = m.Connections.Store.PutSetting(t.Context(), MappingPrefix+"review-step", raw, 1, "fixture"); err != nil {
				t.Fatal(err)
			}
			if _, err = m.Get(t.Context(), "review-step"); err == nil {
				t.Fatal("corrupt mapping read")
			}
			if _, err = m.List(t.Context()); err == nil {
				t.Fatal("corrupt mapping hidden in list")
			}
		})
	}
}
