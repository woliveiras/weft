package mcpconfig

import (
	"context"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

// RegistrationRecovery retains locally authored evidence, never an upstream
// response, client identifier or credential. Configuration edits retain it.
type RegistrationRecovery struct {
	Revision    int64                   `json:"revision"`
	RecoveredAt time.Time               `json:"recoveredAt"`
	Diagnostic  *RegistrationDiagnostic `json:"diagnostic,omitempty"`
}

func validateRegistrationRecovery(r record, revision int64, acknowledged bool) error {
	if revision <= 0 || !acknowledged {
		return fmt.Errorf("%w: current revision and duplicate/orphaned client risk acknowledgement required", ErrInvalid)
	}
	if r.Revision != revision {
		return storage.ErrConflict
	}
	if !r.Enabled || r.OAuth == nil || manualOAuth(&r.OAuth.OAuthConfig) || r.Registration == nil || r.Registration.Mode != "dcr" || r.Registration.Status != "uncertain" {
		return fmt.Errorf("%w: only an enabled uncertain DCR registration can be recovered", ErrInvalid)
	}
	// Keep the complete evidence instead of silently evicting old attempts.
	if len(r.RegistrationHistory) >= 10 {
		return fmt.Errorf("%w: registration recovery history limit reached; reconcile existing clients before further recovery", ErrInvalid)
	}
	if r.Sealed != "" || r.OAuthClientSealed != "" || r.OAuthRefreshSealed != "" || r.Registration.ClientID != "" {
		return fmt.Errorf("%w: registration with retained credentials or client identity requires manual reconciliation", ErrInvalid)
	}
	return nil
}

// RecoverOAuthRegistration archives an uncertain DCR attempt and permits a
// separately reviewed fresh start. It performs no external request and never
// claims that the provider removed or failed to create the previous client.
func (m *Manager) RecoverOAuthRegistration(ctx context.Context, id string, revision int64, acknowledged bool, actor string) (View, error) {
	r, err := m.load(ctx, id)
	if err != nil {
		return View{}, err
	}
	if err = validateRegistrationRecovery(r, revision, acknowledged); err != nil {
		return View{}, err
	}
	r.RegistrationHistory = append(r.RegistrationHistory, RegistrationRecovery{Revision: revision, RecoveredAt: time.Now().UTC(), Diagnostic: r.Registration.Diagnostic})
	r.Registration = nil
	r.OAuth.RegistrationStatus = ""
	r.OAuth.RegistrationDiagnostic = nil
	r.OAuth.EffectiveRegistrationMode = ""
	r.OAuth.Status = "authorization_required"
	r.OAuthMeta = oauthMetadata{}
	r.Status = "untested"
	r.Error = ""
	r.Tools = nil
	r.ToolsRevision = 0
	// CAS prevents stale/concurrent recovery from resetting another attempt.
	return m.save(ctx, r, revision, actor, "mcp.oauth.registration_recovered")
}
