package trigger

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

type HoldResolution struct {
	Key        string    `json:"key"`
	URL        string    `json:"url,omitempty"`
	Reason     string    `json:"reason"`
	Round      int       `json:"round"`
	ResolvedAt time.Time `json:"resolvedAt"`
}

func holdReleaseReason(t *Trigger, h Hold, i Issue) string {
	if reason := issueReason(t, i); reason != "" {
		return "The issue " + reason + "."
	}
	if i.HasLabel(t.Label) || i.HasLabel(LabelTodo) || i.HasLabel(LabelFailed) ||
		(h.Publication == "label_pending" && slices.Contains(h.Removals, LabelInProgress) && i.HasLabel(LabelInProgress)) {
		return ""
	}
	return "The issue no longer has a selection or waiting label."
}

func recordHoldResolution(t *Trigger, index int, reason string, now time.Time) {
	h := t.Holds[index]
	t.Holds = slices.Delete(t.Holds, index, index+1)
	t.HoldResolutions = append(t.HoldResolutions, HoldResolution{Key: h.Key, URL: h.URL, Round: h.Round, Reason: reason, ResolvedAt: now})
	if len(t.HoldResolutions) > 20 {
		t.HoldResolutions = t.HoldResolutions[len(t.HoldResolutions)-20:]
	}
	t.ReadinessBlock = ""
}

// ResolveHold serializes operator resolution with polling, including tracker
// write-back. The opening timestamp prevents a stale console action from
// resolving a later hold for the same issue.
func (m Manager) ResolveHold(ctx context.Context, id, key string, opened time.Time, actor string) (*Trigger, error) {
	leaseKey, holder := "trigger/"+id, domain.NewID("resolve")
	ok, err := m.Store.AcquireLease(ctx, leaseKey, holder, time.Minute)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: the trigger is polling; retry after it finishes", storage.ErrConflict)
	}
	defer func() { _ = m.Store.ReleaseLease(context.WithoutCancel(ctx), leaseKey, holder) }()
	t, err := m.Update(ctx, id, actor, func(current *Trigger) error {
		index := current.holdIndex(key)
		if index < 0 {
			return fmt.Errorf("%w: readiness hold", storage.ErrNotFound)
		}
		if !current.Holds[index].OpenedAt.Equal(opened) || current.Claiming == key {
			return fmt.Errorf("%w: the hold changed or has an interrupted claim", storage.ErrConflict)
		}
		recordHoldResolution(current, index, "An operator resolved the readiness hold.", m.now())
		return nil
	})
	if err == nil {
		m.audit(ctx, t, "hold_resolved", actor)
	}
	return t, err
}

func (r *Runner) releaseHold(ctx context.Context, t *Trigger, h Hold, reason string) error {
	current, err := r.Triggers.Update(ctx, t.ID, "system:trigger", func(current *Trigger) error {
		index := current.holdIndex(h.Key)
		if current.State != Active || current.ReadinessDisabled || current.Consent.Scope != t.Consent.Scope ||
			current.Consent.RecordedAt != t.Consent.RecordedAt || index < 0 {
			return errTriggerChanged
		}
		recordHoldResolution(current, index, reason, r.Triggers.now())
		return nil
	})
	if err == nil {
		t.Holds, t.ReadinessBlock = current.Holds, current.ReadinessBlock
	}
	return err
}
