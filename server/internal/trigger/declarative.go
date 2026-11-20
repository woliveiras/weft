package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

func NormalizeInput(in *Input) error { return validate(in) }
func InputOf(t *Trigger) Input       { return triggerInput(t) }
func (m Manager) PreviewConsent(ctx context.Context, in Input, actor string) (Consent, error) {
	if err := validate(&in); err != nil {
		return Consent{}, err
	}
	return m.consent(ctx, in, actor)
}

// ApplyConfiguration writes consent and state atomically. It never briefly activates
// a trigger whose declarative request did not explicitly record consent.
func (m Manager) ApplyConfiguration(ctx context.Context, id string, in Input, actor string, record bool) (*Trigger, error) {
	return m.ApplyConfigurationAt(ctx, id, in, actor, record, "")
}

// ConfigurationRevision excludes transient poll counters but covers reviewed configuration and consent.
func ConfigurationRevision(t *Trigger) string {
	return triggerFingerprint(struct {
		Input         Input
		Consent       Consent
		State, Reason string
	}{triggerInput(t), t.Consent, t.State, t.PausedReason})
}
func (m Manager) ApplyConfigurationAt(ctx context.Context, id string, in Input, actor string, record bool, expected string) (*Trigger, error) {
	if in.Authorization == "" {
		return nil, fmt.Errorf("authorizationMode: required")
	}
	if err := validate(&in); err != nil {
		return nil, err
	}
	reviewed, err := m.consent(ctx, in, actor)
	if err != nil {
		return nil, err
	}
	change := func(t *Trigger) error {
		if t.CurrentTask != "" && t.LifecycleMappings != in.LifecycleMappings {
			return fmt.Errorf("%w: let the current task finish before changing lifecycle mappings", storage.ErrConflict)
		}
		if expected != "" && ConfigurationRevision(t) != expected {
			return storage.ErrConflict
		}
		if t.Claiming != "" {
			return fmt.Errorf("%w: trigger has an admission in progress", storage.ErrConflict)
		}
		apply(t, in)
		t.State, t.PausedReason = Paused, "consent required"
		t.Consent = Consent{}
		if record {
			t.Consent, t.State, t.PausedReason = reviewed, Active, ""
		}
		return nil
	}
	if id != "" {
		t, err := m.Update(ctx, id, actor, change)
		if err == nil {
			m.audit(ctx, t, "updated", actor)
		}
		return t, err
	}
	now := m.now()
	t := &Trigger{ID: domain.NewID("trg"), Processed: map[string]string{}, CreatedAt: now, UpdatedAt: now, UpdatedBy: actor}
	if err = change(t); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	if _, err = m.Store.PutSetting(ctx, Prefix+t.ID, raw, 0, actor); err != nil {
		return nil, err
	}
	m.audit(ctx, t, "created", actor)
	return t, nil
}

// FindByName refuses ambiguous console-created names instead of choosing a trigger.
func (m Manager) FindByName(ctx context.Context, name string) (*Trigger, error) {
	ts, err := m.List(ctx, "")
	if err != nil {
		return nil, err
	}
	var found *Trigger
	for _, t := range ts {
		if t.Name == name {
			if found != nil {
				return nil, errors.New("ambiguous trigger name")
			}
			found = t
		}
	}
	return found, nil
}
