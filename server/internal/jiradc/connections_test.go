package jiradc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func connectionManager(t *testing.T) (Connections, storage.Store) {
	t.Helper()
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, err := secrets.NewBox(bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return Connections{Store: st, Box: box}, st
}
func TestNativeConnectionPersistenceCASAndAudience(t *testing.T) {
	m, st := connectionManager(t)
	token := "native-fixture-token"
	in := ConnectionInput{Name: "jira-team", Kind: "jira-dc", SiteURL: "https://jira.example/context/", ProjectID: "10001", Enabled: true, Token: &token}
	v, err := m.Put(t.Context(), in, 0, "admin")
	if err != nil || v.Revision != 1 || v.SiteURL != "https://jira.example/context" || !v.CredentialConfigured {
		t.Fatal(v, err)
	}
	raw, err := st.GetSetting(t.Context(), ConnectionPrefix+in.Name)
	if err != nil || strings.Contains(string(raw.Value), token) {
		t.Fatal("credential not sealed", err)
	}
	list, err := m.List(t.Context())
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	safe, _ := json.Marshal(list)
	if strings.Contains(string(safe), token) || strings.Contains(string(safe), "sealedToken") {
		t.Fatal("credential in views")
	}
	client, err := m.Open(t.Context(), in.Name, v.Revision)
	if err != nil || client.project != "10001" || client.token != token {
		t.Fatal("native open failed", err)
	}
	in.Token = nil
	in.SiteURL = "https://other.example"
	if _, err = m.Put(t.Context(), in, 1, "admin"); !errors.Is(err, ErrInvalid) {
		t.Fatal("scope change reused credential", err)
	}
	in.SiteURL = v.SiteURL
	in.Enabled = false
	next, err := m.Put(t.Context(), in, 1, "admin")
	if err != nil || next.Revision != 2 {
		t.Fatal(next, err)
	}
	if _, err = m.Open(t.Context(), in.Name, 1); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("stale accepted", err)
	}
	if _, err = m.Open(t.Context(), in.Name, 2); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("disabled accepted", err)
	}
	if _, err = m.Put(t.Context(), in, 1, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("stale overwrite", err)
	}
	evs, err := st.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{"tracker.connection_change_requested"}})
	if err != nil || len(evs) != 2 {
		t.Fatal(len(evs), err)
	}
	rawEvents, _ := json.Marshal(evs)
	if strings.Contains(string(rawEvents), token) || strings.Contains(string(rawEvents), "jira.example") {
		t.Fatal("sensitive audit payload")
	}
	// Ciphertext moved to a different reviewed scope fails its associated-data check.
	row, _ := st.GetSetting(t.Context(), ConnectionPrefix+in.Name)
	var rec connectionRecord
	_ = json.Unmarshal(row.Value, &rec)
	rec.ProjectID = "10002"
	rec.Enabled = true
	tampered, _ := json.Marshal(rec)
	if _, err = st.PutSetting(t.Context(), ConnectionPrefix+in.Name, tampered, row.Revision, "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Open(t.Context(), in.Name, 3); err == nil {
		t.Fatal("credential accepted under a different project")
	}
}
func TestNativeConnectionRejectsInvalidInputsWithoutMutation(t *testing.T) {
	m, st := connectionManager(t)
	token := "fixture"
	for _, in := range []ConnectionInput{{Name: "jira", Kind: "jira", SiteURL: "https://jira.example", ProjectID: "10001", Token: &token}, {Name: "../jira", Kind: "jira-dc", SiteURL: "https://jira.example", ProjectID: "10001", Token: &token}, {Name: "jira", Kind: "jira-dc", SiteURL: "http://jira.example", ProjectID: "10001", Token: &token}, {Name: "jira", Kind: "jira-dc", SiteURL: "https://jira.example", ProjectID: "APP", Token: &token}, {Name: "jira", Kind: "jira-dc", SiteURL: "https://jira.example", ProjectID: "10001"}} {
		if _, err := m.Put(t.Context(), in, 0, "admin"); !errors.Is(err, ErrInvalid) {
			t.Fatal(in.Name, err)
		}
	}
	rows, err := st.ListSettings(t.Context(), ConnectionPrefix)
	if err != nil || len(rows) != 0 {
		t.Fatal(len(rows), err)
	}
}

type connectionFaultStore struct {
	storage.Store
	failAudit bool
	failCAS   bool
}

func (s connectionFaultStore) AppendEvent(ctx context.Context, e *domain.Event) error {
	if s.failAudit {
		return errors.New("fixture audit failure")
	}
	return s.Store.AppendEvent(ctx, e)
}
func (s connectionFaultStore) PutSetting(ctx context.Context, key string, value []byte, rev int64, actor string) (int64, error) {
	if s.failCAS {
		return 0, storage.ErrConflict
	}
	return s.Store.PutSetting(ctx, key, value, rev, actor)
}
func TestNativeConnectionStorageFailuresRetainState(t *testing.T) {
	for _, audit := range []bool{true, false} {
		t.Run(fmt.Sprint(audit), func(t *testing.T) {
			m, st := connectionManager(t)
			m.Store = connectionFaultStore{Store: st, failAudit: audit, failCAS: !audit}
			token := "fixture-token"
			_, err := m.Put(t.Context(), ConnectionInput{Name: "jira", Kind: "jira-dc", SiteURL: "https://jira.example", ProjectID: "10001", Enabled: true, Token: &token}, 0, "admin")
			if err == nil {
				t.Fatal("storage failure appeared successful")
			}
			rows, err := st.ListSettings(t.Context(), ConnectionPrefix)
			if err != nil || len(rows) != 0 {
				t.Fatal("storage changed", len(rows), err)
			}
		})
	}
}

func TestNativeConnectionPreviewHasNoEffectsAndChecksCredentialAudience(t *testing.T) {
	m, st := connectionManager(t)
	token := "preview-fixture-token"
	in := ConnectionInput{Name: "jira-preview", Kind: "jira-dc", SiteURL: "https://jira.example/context/", ProjectID: "10001", Enabled: true, Token: &token}
	normalized, err := m.Preview(t.Context(), in)
	if err != nil || normalized.SiteURL != "https://jira.example/context" {
		t.Fatal(normalized.Name, err)
	}
	rows, err := st.ListSettings(t.Context(), ConnectionPrefix)
	if err != nil || len(rows) != 0 {
		t.Fatal("preview changed settings", err)
	}
	evs, err := st.ListEvents(t.Context(), storage.EventFilter{})
	if err != nil || len(evs) != 0 {
		t.Fatal("preview audited a mutation", err)
	}
	if _, err = m.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	for value, want := range map[string]bool{token: true, "different": false} {
		match, err := m.CredentialMatches(t.Context(), in.Name, value)
		if err != nil || match != want {
			t.Fatal("credential comparison", match, err)
		}
	}
	in.Token = nil
	if _, err = m.Preview(t.Context(), in); err != nil {
		t.Fatal("same scope preserve", err)
	}
	in.ProjectID = "10002"
	if _, err = m.Preview(t.Context(), in); !errors.Is(err, ErrInvalid) {
		t.Fatal("cross-project preserve", err)
	}
	in.Token = &token
	if _, err = m.Preview(t.Context(), in); err != nil {
		t.Fatal("explicit replacement rejected", err)
	}
}
