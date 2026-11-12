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
	Actor      string    `json:"actor,omitempty"`
	Scope      string    `json:"scope,omitempty"`
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

func recordHoldResolution(t *Trigger, index int, reason, actor string, now time.Time) {
	h := t.Holds[index]
	t.Holds = slices.Delete(t.Holds, index, index+1)
	t.HoldResolutions = append(t.HoldResolutions, HoldResolution{Actor: actor, Scope: h.Scope, Key: h.Key, URL: h.URL, Round: h.Round, Reason: reason, ResolvedAt: now})
	if len(t.HoldResolutions) > 20 {
		t.HoldResolutions = t.HoldResolutions[len(t.HoldResolutions)-20:]
	}
	t.ReadinessBlock = ""
}

func holdHasPendingWrite(h Hold) bool {
	return h.Publication != "label_failed" && (h.UndoOwner != "" || h.Publication == "label_pending" || h.Publication == "label_pending_only")
}

const abandonTrackerWriteWarning = "The tracker keeps its current labels (including any selection, weft:wip, or weft:needs-attention labels) and assignment. Review them on the issue."

// ResolveHold serializes operator resolution with polling, including tracker
// write-back. The generation prevents a stale console action from resolving
// a later round or a replacement hold for the same issue.
func (m Manager) ResolveHold(ctx context.Context, id, key, generation, actor string) (*Trigger, error) {
	return m.resolveHold(ctx, id, key, generation, actor, false)
}

// ForceResolveHold records the operator's decision to abandon tracker write-back.
func (m Manager) ForceResolveHold(ctx context.Context, id, key, generation, actor string) (*Trigger, error) {
	return m.resolveHold(ctx, id, key, generation, actor, true)
}

func (m Manager) resolveHold(ctx context.Context, id, key, generation, actor string, force bool) (*Trigger, error) {
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
		if generation == "" || current.Holds[index].Generation != generation || current.Claiming == key {
			return fmt.Errorf("%w: the hold changed or has an interrupted claim", storage.ErrConflict)
		}
		h := current.Holds[index]
		reason := "An operator resolved the readiness hold."
		if holdHasPendingWrite(h) || h.Publication == "label_failed" {
			if !force {
				return fmt.Errorf("%w: force resolution to abandon the tracker update. %s", storage.ErrConflict, abandonTrackerWriteWarning)
			}
			reason = "An operator abandoned the tracker update and resolved the readiness hold. " + abandonTrackerWriteWarning
		}
		recordHoldResolution(current, index, reason, actor, m.now())
		return nil
	})
	if err == nil {
		m.auditHoldResolution(ctx, t)
	}
	return t, err
}

func (m Manager) auditHoldResolution(ctx context.Context, t *Trigger) {
	if m.Bus == nil {
		return
	}
	h := t.HoldResolutions[len(t.HoldResolutions)-1]
	m.Bus.Publish(context.WithoutCancel(ctx), domain.NewEvent(domain.EvTriggerHoldResolved, "", map[string]any{
		"trigger": t.ID, "key": h.Key, "reason": h.Reason, "round": h.Round,
	}).WithActor(h.Actor))
}

func (r *Runner) releaseHold(ctx context.Context, t *Trigger, h Hold, reason string, retainProgress bool) error {
	current, err := r.Triggers.Update(ctx, t.ID, "system:trigger", func(current *Trigger) error {
		index := current.holdIndex(h.Key)
		if current.State != Active || current.ReadinessDisabled || current.Consent.Scope != t.Consent.Scope ||
			current.Consent.RecordedAt != t.Consent.RecordedAt || index < 0 {
			return errTriggerChanged
		}
		if retainProgress {
			if current.RetainedReadiness == nil {
				current.RetainedReadiness = make(map[string]ReadinessProgress)
			}
			progress := ReadinessProgress{Round: h.Round, SnapshotEdits: h.SnapshotEdits, Intervention: h.Intervention}
			if requiresOperator(h) {
				progress.State, progress.Reason = h.State, h.Reason
			}
			current.RetainedReadiness[readinessProgressKey(current, h.Key)] = progress
		}
		recordHoldResolution(current, index, reason, "system:trigger", r.Triggers.now())
		return nil
	})
	if err == nil {
		t.Holds, t.ReadinessBlock, t.RetainedReadiness = current.Holds, current.ReadinessBlock, current.RetainedReadiness
		r.Triggers.auditHoldResolution(ctx, current)
	}
	return err
}

// ReadinessProgress preserves the allowances when a GitHub issue leaves a
// trigger query and is later selected again for the same workflow.
type ReadinessProgress struct {
	Round         int    `json:"round"`
	SnapshotEdits int    `json:"snapshotEdits,omitempty"`
	Intervention  bool   `json:"intervention,omitempty"`
	State         string `json:"state,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func readinessProgressKey(t *Trigger, key string) string {
	return DedupKey(t, Issue{Source: t.Source.Kind, Key: key})
}
