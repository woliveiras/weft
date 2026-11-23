package mcpconfig

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/mcpconfig/mcptest"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestRegistrationRecoveryValidation(t *testing.T) {
	for _, tc := range []struct {
		name, mode, status string
		enabled, ack       bool
		revision           int64
		want               error
	}{
		{"valid", "dcr", "uncertain", true, true, 3, nil},
		{"unacknowledged", "dcr", "uncertain", true, false, 3, ErrInvalid},
		{"stale", "dcr", "uncertain", true, true, 2, storage.ErrConflict},
		{"pending", "dcr", "pending", true, true, 3, ErrInvalid},
		{"ready", "dcr", "ready", true, true, 3, ErrInvalid},
		{"cimd", "cimd", "uncertain", true, true, 3, ErrInvalid},
		{"disabled", "dcr", "uncertain", false, true, 3, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := record{View: View{Enabled: tc.enabled, Revision: 3, OAuth: &OAuthView{OAuthConfig: OAuthConfig{RegistrationMode: tc.mode}}}, Registration: &clientRegistration{Mode: tc.mode, Status: tc.status}}
			err := validateRegistrationRecovery(r, tc.revision, tc.ack)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
		})
	}
	if err := validateRegistrationRecovery(record{}, 0, true); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestRegistrationRecoveryPersistsHistoryAndSerializesNewAttempt(t *testing.T) {
	m := testManager(t)
	provider := mcptest.NewOAuth("")
	defer provider.Close()
	m.OAuthClient = provider.Client()
	provider.FailRegistration.Store(true)
	in := input()
	in.Service = "generic"
	in.Endpoint = provider.URL + "/mcp"
	in.OAuth = &OAuthConfig{Issuer: provider.URL, RegistrationMode: "dcr", Scopes: []string{"read"}}
	if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StartOAuthReviewed(t.Context(), in.ID, "admin", registrationCallback, "dcr"); err == nil {
		t.Fatal("fixture failure accepted")
	}
	old, err := m.Get(t.Context(), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := m.RecoverOAuthRegistration(t.Context(), in.ID, old.Revision, true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != "untested" || provider.Registrations.Load() != 1 || recovered.OAuth.RegistrationStatus != "" || len(recovered.OAuth.RegistrationHistory) != 1 {
		t.Fatal("recovery called provider or lost history", recovered.OAuth)
	}
	history := recovered.OAuth.RegistrationHistory[0]
	if history.Revision != old.Revision || history.Diagnostic == nil || history.Diagnostic.Code != "http_rejected" || history.RecoveredAt.IsZero() {
		t.Fatal(history)
	}
	if _, err = m.RecoverOAuthRegistration(t.Context(), in.ID, old.Revision, true, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("stale recovery replay", err)
	}
	if _, err = m.RecoverOAuthRegistration(t.Context(), in.ID, recovered.Revision, true, "admin"); !errors.Is(err, ErrInvalid) {
		t.Fatal("recovery replay", err)
	}
	// Configuration edits must not erase the uncertain attempt's archive.
	in.Name = "Renamed"
	in.OAuth.Scopes = []string{"read", "search"}
	recovered, err = m.Put(t.Context(), in, recovered.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	restarted := New(m.Store, m.Box, nil)
	restarted.OAuthClient = provider.Client()
	recovered, err = restarted.Get(t.Context(), in.ID)
	if err != nil || len(recovered.OAuth.RegistrationHistory) != 1 {
		t.Fatal("restart/configuration lost history", err)
	}
	provider.FailRegistration.Store(false)
	var wg sync.WaitGroup
	var successes atomic.Int32
	for range 8 {
		wg.Go(func() {
			if _, err := restarted.StartOAuthReviewed(t.Context(), in.ID, "admin", registrationCallback, "dcr"); err == nil {
				successes.Add(1)
			}
		})
	}
	wg.Wait()
	if successes.Load() == 0 || provider.Registrations.Load() != 2 {
		t.Fatal("new attempt was not serialized", successes.Load(), provider.Registrations.Load())
	}
	final, err := restarted.Get(t.Context(), in.ID)
	if err != nil || len(final.OAuth.RegistrationHistory) != 1 || final.OAuth.RegistrationStatus != "ready" {
		t.Fatal("new attempt destroyed archive", err)
	}
	raw, _ := json.Marshal(final)
	if strings.Contains(string(raw), "synthetic-registration-secret") {
		t.Fatal("secret leaked")
	}
}

func TestRegistrationRecoveryFailureAndBound(t *testing.T) {
	m := testManager(t)
	in := input()
	in.Service = "generic"
	in.OAuth = &OAuthConfig{Issuer: "https://issuer.example", RegistrationMode: "dcr"}
	if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	r, err := m.load(t.Context(), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.Registration = &clientRegistration{Mode: "dcr", Status: "uncertain", Redirect: registrationCallback}
	before, err := m.save(t.Context(), r, r.Revision, "admin", "mcp.oauth.registration_failed")
	if err != nil {
		t.Fatal(err)
	}
	original := m.Store
	m.Store = failedWrites{original}
	if _, err = m.RecoverOAuthRegistration(t.Context(), in.ID, before.Revision, true, "admin"); err == nil {
		t.Fatal("storage failure ignored")
	}
	m.Store = original
	after, err := m.Get(t.Context(), in.ID)
	if err != nil || after.Revision != before.Revision || after.OAuth.RegistrationStatus != "uncertain" || len(after.OAuth.RegistrationHistory) != 0 {
		t.Fatal("failed recovery changed state", err)
	}
	r, err = m.load(t.Context(), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.RegistrationHistory = make([]RegistrationRecovery, 10)
	if err = validateRegistrationRecovery(r, r.Revision, true); !errors.Is(err, ErrInvalid) {
		t.Fatal("history bound not enforced", err)
	}
}

func TestRegistrationRecoveryConcurrentManagers(t *testing.T) {
	m := testManager(t)
	in := input()
	in.Service = "generic"
	in.OAuth = &OAuthConfig{Issuer: "https://issuer.example", RegistrationMode: "dcr"}
	if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	r, err := m.load(t.Context(), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.Registration = &clientRegistration{Mode: "dcr", Status: "uncertain", Redirect: registrationCallback}
	before, err := m.save(t.Context(), r, r.Revision, "admin", "mcp.oauth.registration_failed")
	if err != nil {
		t.Fatal(err)
	}
	other := New(m.Store, m.Box, nil)
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := range 12 {
		wg.Go(func() {
			manager := m
			if i%2 == 0 {
				manager = other
			}
			_, e := manager.RecoverOAuthRegistration(t.Context(), in.ID, before.Revision, true, "admin")
			if e == nil {
				successes.Add(1)
			} else if !errors.Is(e, storage.ErrConflict) {
				t.Errorf("unexpected conflict: %v", e)
			}
		})
	}
	wg.Wait()
	after, err := other.Get(t.Context(), in.ID)
	if err != nil || successes.Load() != 1 || len(after.OAuth.RegistrationHistory) != 1 {
		t.Fatal("concurrent recovery reset state", successes.Load(), err)
	}
}
