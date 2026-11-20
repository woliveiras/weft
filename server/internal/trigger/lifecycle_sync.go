package trigger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

const LifecycleSyncPrefix = "tracker/lifecycle-sync/"

// LifecycleSync is independent of CurrentTask: delivery can complete while an
// optional tracker transition needs attention. Evidence identifies a persisted
// runtime dispatch or PR-opened event, never issue content or a guessed PR URL.
type LifecycleSync struct {
	ID                 string       `json:"id"`
	TriggerID          string       `json:"triggerId"`
	TaskID             string       `json:"taskId"`
	Stage              string       `json:"stage"`
	Evidence           string       `json:"evidence"`
	IssueID            string       `json:"issueId"`
	IssueTypeID        string       `json:"issueTypeId"`
	IssueKey           string       `json:"issueKey"`
	Source             SourceConfig `json:"source"`
	ConnectionRevision int64        `json:"connectionRevision"`
	Mapping            MappingPin   `json:"mapping"`
	ConsentScope       string       `json:"consentScope"`
	Actor              string       `json:"actor"`
	State              string       `json:"state"` // queued, failed, unknown, already_target, target_observed
	Error              string       `json:"error,omitempty"`
	CreatedAt          time.Time    `json:"createdAt"`
	UpdatedAt          time.Time    `json:"updatedAt"`
	Revision           int64        `json:"revision"`
	RetryRevision      int64        `json:"retryRevision,omitempty"`
}

func lifecycleID(triggerID, taskID, stage string) string {
	raw, _ := json.Marshal([]string{triggerID, taskID, stage})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

var providerID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func (v LifecycleSync) valid() bool {
	if v.ID != lifecycleID(v.TriggerID, v.TaskID, v.Stage) || v.TriggerID == "" || v.TaskID == "" || v.Evidence == "" || len(v.Evidence) > 200 || v.Actor == "" || v.ConsentScope == "" || v.ConnectionRevision < 1 || (v.Source.Kind != "jira-dc" && (v.Source.Kind != "jira" || v.Source.ProfileID == "" || v.ConnectionRevision != v.Source.ProfileRevision)) {
		return false
	}
	if _, err := v.Source.Open(); err != nil {
		return false
	}
	pins := LifecycleMappings{}
	switch v.Stage {
	case "work_started":
		pins.WorkStarted = v.Mapping
	case "pr_delivered":
		pins.PRDelivered = v.Mapping
	default:
		return false
	}
	if pins.empty() || pins.validate(v.Source) != nil || !trackerKey.MatchString(v.IssueKey) || v.CreatedAt.IsZero() || v.UpdatedAt.Before(v.CreatedAt) {
		return false
	}
	switch v.State {
	case "queued", "failed", "unknown", "already_target", "target_observed", "superseded":
	default:
		return false
	}
	if (!providerID.MatchString(v.IssueID) || !providerID.MatchString(v.IssueTypeID)) && v.State != "failed" {
		return false
	}
	return true
}
func (m Manager) GetLifecycleSync(ctx context.Context, id string) (LifecycleSync, error) {
	if len(id) != 64 {
		return LifecycleSync{}, tools.ErrDenied
	}
	row, err := m.Store.GetSetting(ctx, LifecycleSyncPrefix+id)
	if err != nil {
		return LifecycleSync{}, err
	}
	var v LifecycleSync
	if json.Unmarshal(row.Value, &v) != nil || v.ID != id || !v.valid() {
		return v, errors.New("stored lifecycle synchronization is corrupt")
	}
	v.Revision = row.Revision
	return v, nil
}

// LifecycleSyncPage bounds storage reads and response size without a global
// history ceiling that could stop pending work. Cursor is an opaque sync ID.
func (m Manager) LifecycleSyncPage(ctx context.Context, after string, limit int) ([]LifecycleSync, string, error) {
	if after != "" && (len(after) != 64 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(after)) {
		return nil, "", jiradc.ErrInvalid
	}
	cursor := ""
	if after != "" {
		cursor = LifecycleSyncPrefix + after
	}
	rows, err := m.Store.ListSettingsPage(ctx, LifecycleSyncPrefix, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	out := make([]LifecycleSync, 0, len(rows))
	next := ""
	for _, row := range rows {
		var v LifecycleSync
		if json.Unmarshal(row.Value, &v) != nil || row.Key != LifecycleSyncPrefix+v.ID || !v.valid() {
			return nil, "", errors.New("stored lifecycle synchronization is corrupt")
		}
		v.Revision = row.Revision
		out = append(out, v)
		next = v.ID
	}
	if len(rows) < limit {
		next = ""
	}
	return out, next, nil
}
func (m Manager) ListLifecycleSyncs(ctx context.Context) ([]LifecycleSync, error) {
	out := []LifecycleSync{}
	cursor := ""
	for {
		page, next, err := m.LifecycleSyncPage(ctx, cursor, 100)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if next == "" {
			break
		}
		cursor = next
	}
	slices.SortFunc(out, func(a, b LifecycleSync) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}
func (m Manager) saveLifecycleSync(ctx context.Context, v LifecycleSync, expected int64, guards ...storage.SettingRevision) (LifecycleSync, error) {
	v.UpdatedAt = m.now()
	if v.UpdatedAt.Before(v.CreatedAt) {
		v.UpdatedAt = v.CreatedAt
	}
	v.Revision = 0
	if !v.valid() {
		return v, errors.New("invalid lifecycle synchronization")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	rev, err := m.Store.PutSettingIf(ctx, LifecycleSyncPrefix+v.ID, raw, expected, "system:trigger", guards)
	v.Revision = rev
	return v, err
}
func (r *Runner) queueLifecycle(ctx context.Context, t *Trigger, stage, evidence string) error {
	pin := t.CurrentMappings.pin(stage)
	if pin == (MappingPin{}) || evidence == "" {
		return nil
	}
	holder := r.Instance + ":" + domain.NewID("sync")
	ok, err := r.Store.AcquireLease(ctx, "lifecycle-task/"+t.CurrentTask, holder, 2*time.Minute)
	if err != nil {
		return err
	}
	if !ok {
		return storage.ErrConflict
	}
	defer r.Store.ReleaseLease(context.WithoutCancel(ctx), "lifecycle-task/"+t.CurrentTask, holder)
	if t.CurrentIssue == nil {
		return errors.New("current issue identity is unavailable")
	}
	current, revision, err := r.Triggers.read(ctx, t.ID)
	if err != nil {
		return err
	}
	if current.CurrentTask != t.CurrentTask || current.CurrentMappings != t.CurrentMappings || current.Source != t.Source || current.Consent.Scope != t.Consent.Scope {
		return errTriggerChanged
	}
	id := lifecycleID(t.ID, t.CurrentTask, stage)
	existing, err := r.Triggers.GetLifecycleSync(ctx, id)
	if err == nil {
		if existing.TaskID != t.CurrentTask || existing.Mapping != pin || existing.Source != t.CurrentSource || existing.IssueID != t.CurrentIssue.ID || existing.IssueTypeID != t.CurrentIssue.IssueTypeID || existing.IssueKey != t.CurrentIssue.Key {
			return storage.ErrConflict
		}
		return nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	now := r.Triggers.now()
	v := LifecycleSync{ID: id, TriggerID: t.ID, TaskID: t.CurrentTask, Stage: stage, Evidence: evidence, IssueID: t.CurrentIssue.ID, IssueTypeID: t.CurrentIssue.IssueTypeID, IssueKey: t.CurrentIssue.Key, Source: t.CurrentSource, ConnectionRevision: workflowRevision(t.CurrentSource, t.Consent.TrackerRevision), Mapping: pin, ConsentScope: t.Consent.Scope, Actor: t.Consent.Actor, State: "queued", CreatedAt: now, UpdatedAt: now}
	if stage == "work_started" {
		delivered, err := r.Triggers.GetLifecycleSync(ctx, lifecycleID(t.ID, t.CurrentTask, "pr_delivered"))
		if err == nil && delivered.State != "queued" {
			v.State = "superseded"
			v.Error = "Runtime admission was observed after delivery synchronization; no late start transition was sent."
		}
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return err
		}
	}
	if !providerID.MatchString(v.IssueID) || !providerID.MatchString(v.IssueTypeID) {
		v.State = "failed"
		v.Error = "The admitted task has no retained numeric issue identity; review it manually. No transition was sent."
	}
	_, err = r.Triggers.saveLifecycleSync(ctx, v, 0, storage.SettingRevision{Key: Prefix + t.ID, Revision: revision})
	return err
}

// processLifecycleSync runs only through the trigger-only gateway. No exposed
// agent grant can construct the private tracker scope. Once journaled by the
// adapter, an uncertain operation is read/reconciled instead of dispatched again.
func (m Manager) processLifecycleSync(ctx context.Context, t *Trigger, id string) (LifecycleSync, error) {
	v, err := m.GetLifecycleSync(ctx, id)
	if err != nil {
		return v, err
	}
	if v.TriggerID != t.ID || v.ConsentScope != t.Consent.Scope || v.Actor != t.Consent.Actor || v.Source != t.Source || v.ConnectionRevision != workflowRevision(t.Source, t.Consent.TrackerRevision) || v.Mapping != t.LifecycleMappings.pin(v.Stage) {
		return v, tools.ErrDenied
	}
	if v.State != "queued" {
		return v, nil
	}
	mappings, syncer, err := m.workflowServices(v.Source)
	if err != nil {
		return v, err
	}
	// The stable adapter operation is namespaced separately from untrusted keys.
	operation := "lifecycle:" + v.ID
	retained, getErr := m.LifecycleTransition(ctx, v)
	var result jiradc.SyncRecord
	if getErr == nil {
		if v.RetryRevision > 0 {
			result, err = syncer.RetryFailed(ctx, operation, v.RetryRevision, "system:trigger")
		} else {
			result, err = syncer.Apply(ctx, retained.Intent, "system:trigger")
		}
	} else if !errors.Is(getErr, storage.ErrNotFound) {
		err = getErr
	} else {
		mapping, lookupErr := mappings.Get(ctx, v.Mapping.Name)
		if lookupErr != nil {
			err = lookupErr
		} else {
			result, err = syncer.Apply(ctx, jiradc.TransitionIntent{OperationID: operation, MappingName: v.Mapping.Name, MappingRevision: v.Mapping.Revision, Connection: workflowConnection(v.Source), ConnectionRevision: v.ConnectionRevision, IssueKey: v.IssueKey, IssueID: v.IssueID, IssueTypeID: v.IssueTypeID, FromStatusID: mapping.FromStatusID, TransitionID: mapping.TransitionID, TargetStatusID: mapping.TargetStatusID}, "system:trigger")
		}
	}
	v.RetryRevision = 0
	v.State = result.State
	if v.State == "" {
		v.State = "failed"
	}
	v.Error = ""
	if err != nil {
		v.Error = "Optional tracker synchronization needs review. Inspect the retained transition evidence before recovery."
	}
	if v.State == "pending" {
		v.State = "unknown"
	}
	saved, saveErr := m.saveLifecycleSync(ctx, v, v.Revision)
	return saved, errors.Join(err, saveErr)
}

// drainLifecycleSyncs is independent of task outcome. Bounded work and per-job
// leases avoid duplicate dispatch and allow an interrupted queued job to resume.
func (r *Runner) drainLifecycleSyncs(ctx context.Context) {
	const scanKey = "tracker/lifecycle-scan-cursor"
	after := ""
	revision := int64(0)
	scan, err := r.Store.GetSetting(ctx, scanKey)
	if err == nil {
		if json.Unmarshal(scan.Value, &after) != nil {
			r.log().ErrorContext(ctx, "lifecycle scan cursor is corrupt")
			return
		}
		revision = scan.Revision
	} else if !errors.Is(err, storage.ErrNotFound) {
		r.log().ErrorContext(ctx, "lifecycle scan cursor is unavailable", "err", err)
		return
	}
	rows, next, err := r.Triggers.LifecycleSyncPage(ctx, after, 20)
	if err != nil {
		r.log().ErrorContext(ctx, "lifecycle synchronization list failed", "err", err)
		return
	}
	slices.SortStableFunc(rows, func(a, b LifecycleSync) int {
		if a.TaskID != b.TaskID {
			return strings.Compare(a.TaskID, b.TaskID)
		}
		if a.Stage == b.Stage {
			return 0
		}
		if a.Stage == "work_started" {
			return -1
		}
		return 1
	})
	defer func() {
		raw, _ := json.Marshal(next)
		if _, err := r.Store.PutSetting(context.WithoutCancel(ctx), scanKey, raw, revision, "system:trigger"); err != nil && !errors.Is(err, storage.ErrConflict) {
			r.log().ErrorContext(ctx, "lifecycle scan cursor could not be saved", "err", err)
		}
	}()
	remaining := 20
	for _, v := range rows {
		if v.State != "queued" || remaining == 0 {
			continue
		}
		remaining--
		func() {
			holder := r.Instance + ":" + domain.NewID("sync")
			ok, err := r.Store.AcquireLease(ctx, "lifecycle-task/"+v.TaskID, holder, 2*time.Minute)
			if err != nil || !ok {
				return
			}
			defer r.Store.ReleaseLease(context.WithoutCancel(ctx), "lifecycle-task/"+v.TaskID, holder)
			if v.Stage == "work_started" {
				later, laterErr := r.Triggers.GetLifecycleSync(ctx, lifecycleID(v.TriggerID, v.TaskID, "pr_delivered"))
				if laterErr != nil && !errors.Is(laterErr, storage.ErrNotFound) {
					return
				}
				if laterErr == nil && later.State != "queued" {
					latest, readErr := r.Triggers.GetLifecycleSync(ctx, v.ID)
					if readErr == nil && latest.State == "queued" {
						latest.State = "superseded"
						latest.Error = "A delivery synchronization already ran; no late start transition was sent."
						if _, err := r.Triggers.saveLifecycleSync(ctx, latest, latest.Revision); err != nil {
							r.log().ErrorContext(ctx, "superseded start could not be recorded", "sync", v.ID, "err", err)
						}
					}
					return
				}
			}
			if v.Stage == "pr_delivered" {
				prior, priorErr := r.Triggers.GetLifecycleSync(ctx, lifecycleID(v.TriggerID, v.TaskID, "work_started"))
				if priorErr != nil && !errors.Is(priorErr, storage.ErrNotFound) {
					return
				}
				if priorErr == nil && (prior.State == "queued" || prior.State == "unknown") {
					return
				}
			}
			t, err := r.Triggers.Get(ctx, v.TriggerID)
			if err == nil {
				_, err = r.checkedInvoker(t)(ctx, trackerTransition, trackerArgs{SyncID: v.ID})
			}
			if err != nil {
				latest, readErr := r.Triggers.GetLifecycleSync(ctx, v.ID)
				if readErr == nil && latest.State == "queued" {
					latest.State = "unknown"
					// The adapter reserves before POST. Absence or a retained failed
					// preflight proves this operation never crossed dispatch.
					core, coreErr := r.Triggers.LifecycleTransition(ctx, latest)
					if errors.Is(coreErr, storage.ErrNotFound) || (coreErr == nil && core.State == "failed") {
						latest.State = "failed"
					}
					latest.Error = "The original trigger authorization is unavailable or the synchronization was interrupted. Review before recovery."
					if _, saveErr := r.Triggers.saveLifecycleSync(ctx, latest, latest.Revision); saveErr != nil {
						r.log().ErrorContext(ctx, "lifecycle synchronization failure could not be retained", "sync", v.ID, "err", saveErr)
					}
				}
				r.log().WarnContext(ctx, "optional lifecycle synchronization needs review", "sync", v.ID, "task", v.TaskID, "stage", v.Stage, "err", err)
			}
		}()
	}
}

// ReconcileLifecycleSync only reads the exact retained adapter intent. Disabling
// a mapping cannot erase outcome evidence or grant a fresh transition attempt.
func (m Manager) ReconcileLifecycleSync(ctx context.Context, id string, expected int64, actor string) (LifecycleSync, error) {
	v, err := m.GetLifecycleSync(ctx, id)
	if err != nil {
		return v, err
	}
	if expected < 1 || v.Revision != expected {
		return v, storage.ErrConflict
	}
	_, syncer, err := m.workflowServices(v.Source)
	if err != nil {
		return v, err
	}
	core, err := m.LifecycleTransition(ctx, v)
	if err != nil {
		return v, err
	}
	if core.State == "failed" {
		return v, errors.New("transition failed before dispatch; review an explicit preflight retry")
	}
	core, readErr := syncer.Apply(ctx, core.Intent, actor)
	if core.State == "" || core.State == "pending" {
		return v, readErr
	}
	v.State = core.State
	v.Error = ""
	if readErr != nil {
		v.Error = "The transition outcome remains uncertain. Reconcile by reading; do not repeat the POST."
	}
	saved, saveErr := m.saveLifecycleSync(ctx, v, expected)
	return saved, errors.Join(readErr, saveErr)
}

// RetryLifecycleSync queues only a known pre-dispatch failure under the same
// reviewed scope. Neither unknown writes nor changed pins can authorize replay.
func (m Manager) RetryLifecycleSync(ctx context.Context, id string, expected int64, actor string) (LifecycleSync, error) {
	v, err := m.GetLifecycleSync(ctx, id)
	if err != nil {
		return v, err
	}
	if expected < 1 || v.Revision != expected || v.State != "failed" {
		return v, storage.ErrConflict
	}
	holder := "recovery:" + domain.NewID("sync")
	acquired, err := m.Store.AcquireLease(ctx, "lifecycle-task/"+v.TaskID, holder, 2*time.Minute)
	if err != nil {
		return v, err
	}
	if !acquired {
		return v, storage.ErrConflict
	}
	defer m.Store.ReleaseLease(context.WithoutCancel(ctx), "lifecycle-task/"+v.TaskID, holder)
	v, err = m.GetLifecycleSync(ctx, id)
	if err != nil {
		return v, err
	}
	if v.Revision != expected || v.State != "failed" {
		return v, storage.ErrConflict
	}

	if v.Stage == "work_started" {
		delivered, readErr := m.GetLifecycleSync(ctx, lifecycleID(v.TriggerID, v.TaskID, "pr_delivered"))
		if readErr != nil && !errors.Is(readErr, storage.ErrNotFound) {
			return v, readErr
		}
		if readErr == nil && delivered.State != "queued" {
			return v, storage.ErrConflict
		}
	}
	t, revision, err := m.read(ctx, v.TriggerID)
	if err != nil {
		return v, err
	}
	if t.State != Active || t.Source != v.Source || t.Consent.Scope != v.ConsentScope || t.Consent.Actor != v.Actor || workflowRevision(t.Source, t.Consent.TrackerRevision) != v.ConnectionRevision || t.LifecycleMappings.pin(v.Stage) != v.Mapping {
		return v, storage.ErrConflict
	}
	problem, err := m.ConsentProblem(ctx, t)
	if err != nil {
		return v, err
	}
	if problem != "" {
		return v, storage.ErrConflict
	}
	core, err := m.LifecycleTransition(ctx, v)
	if err == nil {
		if core.State != "failed" {
			return v, storage.ErrConflict
		}
		v.RetryRevision = core.Revision
	} else if !errors.Is(err, storage.ErrNotFound) {
		return v, err
	}
	ev := domain.NewEvent("tracker.lifecycle_sync_recovery_requested", "", map[string]any{"sync": v.ID, "task": v.TaskID, "stage": v.Stage, "expectedRevision": expected}).WithActor(actor)
	if err = m.Store.AppendEvent(ctx, ev); err != nil {
		return v, err
	}
	v.State = "queued"
	v.Error = ""
	return m.saveLifecycleSync(ctx, v, expected, storage.SettingRevision{Key: Prefix + t.ID, Revision: revision})
}
