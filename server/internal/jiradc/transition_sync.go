package jiradc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

const SyncPrefix = "tracker/jira-dc/sync/"

var operationID = regexp.MustCompile(`^[A-Za-z0-9:_-]{1,200}$`)
var ErrSyncUnconfirmed = errors.New("Jira transition outcome is unconfirmed; reconcile by reading before any new action")

// TransitionIntent is an internal, reviewed configuration snapshot. It is not an
// agent tool or HTTP request schema. Lifecycle callers must derive OperationID
// from durable task/delivery evidence, never from an untrusted issue instruction.
// Reusing an operation with different scope or mapping is a conflict, not a new
// attempt. Names and board columns deliberately do not identify transitions.
type TransitionIntent struct {
	OperationID        string `json:"operationId"`
	MappingName        string `json:"mappingName,omitempty"`
	MappingRevision    int64  `json:"mappingRevision,omitempty"`
	Connection         string `json:"connection"`
	ConnectionRevision int64  `json:"connectionRevision"`
	IssueKey           string `json:"issueKey"`
	IssueID            string `json:"issueId"`
	IssueTypeID        string `json:"issueTypeId"`
	FromStatusID       string `json:"fromStatusId"`
	TransitionID       string `json:"transitionId"`
	TargetStatusID     string `json:"targetStatusId"`
}

func (v TransitionIntent) validate() error {
	if (v.MappingName == "" && v.MappingRevision != 0) || (v.MappingName != "" && (!connectionName.MatchString(v.MappingName) || v.MappingRevision < 1)) {
		return fmt.Errorf("%w: mapping name and positive revision must be reviewed together", ErrInvalid)
	}
	if !operationID.MatchString(v.OperationID) || !connectionName.MatchString(v.Connection) || v.ConnectionRevision < 1 || ValidateIssueKey(v.IssueKey) != nil {
		return fmt.Errorf("%w: invalid transition operation or connection", ErrInvalid)
	}
	for _, id := range []string{v.IssueID, v.IssueTypeID, v.FromStatusID, v.TransitionID, v.TargetStatusID} {
		if !numericID.MatchString(id) {
			return fmt.Errorf("%w: transition mappings require numeric scoped identifiers", ErrInvalid)
		}
	}
	if v.FromStatusID == v.TargetStatusID {
		return fmt.Errorf("%w: same-status actions are not lifecycle status transitions", ErrInvalid)
	}
	return nil
}
func syncKey(operation string) string {
	sum := sha256.Sum256([]byte(operation))
	return SyncPrefix + hex.EncodeToString(sum[:])
}

// SyncRecord is durable evidence, separate from task status. TargetObserved means
// the desired state was read; it does not attribute that change to this account.
// An uncertain POST is never automatically repeated, including after restart.
type SyncRecord struct {
	Intent           TransitionIntent `json:"intent"`
	SiteURL          string           `json:"siteUrl"`
	ProjectID        string           `json:"projectId"`
	State            string           `json:"state"` // pending, failed, unknown, already_target, target_observed
	ObservedStatusID string           `json:"observedStatusId,omitempty"`
	Error            string           `json:"error,omitempty"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
	Revision         int64            `json:"revision"`
}
type TransitionSync struct{ Connections Connections }

type preparedTransition struct {
	issue         Issue
	alreadyTarget bool
}

func validIssueUpdated(value string) bool {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999Z0700"} {
		if at, err := time.Parse(layout, value); err == nil && !at.IsZero() {
			return true
		}
	}
	return false
}

func (c *Client) prepareTransition(ctx context.Context, v TransitionIntent) (preparedTransition, error) {
	if err := c.authorizeWrite(ctx); err != nil {
		return preparedTransition{}, err
	}
	i, err := c.Issue(ctx, v.IssueKey)
	if err != nil {
		return preparedTransition{}, err
	}
	if i.ID != v.IssueID || i.Fields.IssueType.ID != v.IssueTypeID {
		return preparedTransition{}, errors.New("issue identity or type changed since mapping review")
	}
	if i.Fields.Status.ID == v.TargetStatusID {
		return preparedTransition{issue: i, alreadyTarget: true}, nil
	}
	if i.Fields.Status.ID != v.FromStatusID {
		return preparedTransition{}, errors.New("issue is outside the reviewed source status; rediscover its workflow")
	}
	if !validIssueUpdated(i.Fields.Updated) {
		return preparedTransition{}, errors.New("issue update metadata is unavailable")
	}
	d, err := c.DiscoverTransitions(ctx, v.IssueKey)
	if err != nil {
		return preparedTransition{}, err
	}
	if d.IssueID != i.ID || d.IssueTypeID != v.IssueTypeID || d.Current.ID != v.FromStatusID || d.Updated != i.Fields.Updated {
		return preparedTransition{}, errors.New("issue changed while preparing its transition")
	}
	for _, tr := range d.Transitions {
		if tr.ID != v.TransitionID {
			continue
		}
		if tr.To.ID != v.TargetStatusID {
			return preparedTransition{}, errors.New("transition destination changed; review the mapping")
		}
		if tr.Fields == nil {
			return preparedTransition{}, errors.New("transition field metadata is unavailable")
		}
		for _, f := range tr.Fields {
			if f.Required == nil || *f.Required {
				return preparedTransition{}, errors.New("required transition fields need explicit configuration; no fields were written")
			}
		}
		return preparedTransition{issue: i}, nil
	}
	return preparedTransition{}, errors.New("mapped transition is unavailable for the current issue and account")
}
func sameIssueSnapshot(a, b Issue) bool {
	return a.ID == b.ID && a.Fields.Project.ID == b.Fields.Project.ID && a.Fields.IssueType.ID == b.Fields.IssueType.ID && a.Fields.Status.ID == b.Fields.Status.ID && a.Fields.Updated == b.Fields.Updated
}
func (c *Client) sendPreparedTransition(ctx context.Context, v TransitionIntent, p preparedTransition) (bool, error) {
	// The journal is already reserved. Recheck the issue after that local boundary;
	// Jira REST has no status CAS, so this is a preflight, not an atomic remote fence.
	i, err := c.Issue(ctx, v.IssueKey)
	if err != nil {
		return false, err
	}
	if !sameIssueSnapshot(p.issue, i) {
		return false, errors.New("issue changed before transition dispatch")
	}
	if err = c.authorizeWrite(ctx); err != nil {
		return false, err
	}
	payload := map[string]any{"transition": map[string]string{"id": v.TransitionID}}
	// Once this boundary is reached, failures are conservatively uncertain. Even
	// HTTP rejection can pass through a proxy with an ambiguous remote outcome.
	return true, c.do(ctx, "POST", "/issue/"+v.IssueID+"/transitions", payload, nil)
}
func (m TransitionSync) Get(ctx context.Context, operation string) (SyncRecord, error) {
	if !operationID.MatchString(operation) {
		return SyncRecord{}, ErrInvalid
	}
	row, err := m.Connections.Store.GetSetting(ctx, syncKey(operation))
	if err != nil {
		return SyncRecord{}, err
	}
	var out SyncRecord
	if json.Unmarshal(row.Value, &out) != nil || out.Intent.OperationID != operation || out.Intent.validate() != nil || ValidateScope(out.SiteURL, out.ProjectID) != nil {
		return SyncRecord{}, errors.New("stored transition synchronization is corrupt")
	}
	switch out.State {
	case "pending", "failed", "unknown", "already_target", "target_observed":
	default:
		return SyncRecord{}, errors.New("stored transition state is invalid")
	}
	if out.CreatedAt.IsZero() || out.UpdatedAt.Before(out.CreatedAt) || (out.ObservedStatusID != "" && !numericID.MatchString(out.ObservedStatusID)) {
		return SyncRecord{}, errors.New("stored transition evidence is invalid")
	}
	if (out.State == "already_target" || out.State == "target_observed") && (out.ObservedStatusID != out.Intent.TargetStatusID || out.Error != "") {
		return SyncRecord{}, errors.New("stored transition confirmation is invalid")
	}
	out.Revision = row.Revision
	return out, nil
}
func (m TransitionSync) store(ctx context.Context, v SyncRecord, actor string, expected int64) (SyncRecord, error) {
	v.UpdatedAt = time.Now().UTC()
	v.Revision = 0
	raw, err := json.Marshal(v)
	if err != nil {
		return SyncRecord{}, err
	}
	guards := []storage.SettingRevision{{Key: ConnectionPrefix + v.Intent.Connection, Revision: v.Intent.ConnectionRevision}}
	// Only reservation authorizes dispatch. Historical reconciliation remains a
	// read even after the mapping is disabled or edited.
	if v.State == "pending" && v.Intent.MappingName != "" {
		guards = append(guards, storage.SettingRevision{Key: MappingPrefix + v.Intent.MappingName, Revision: v.Intent.MappingRevision})
	}
	revision, err := m.Connections.Store.PutSettingIf(ctx, syncKey(v.Intent.OperationID), raw, expected, actor, guards)
	if err != nil {
		return SyncRecord{}, err
	}
	v.Revision = revision
	return v, nil
}

// Apply reserves an intent before dispatch. An existing pending/unknown record
// is reconciled with reads only; preflight failures remain retained until an
// explicit recovery is reviewed. Storage errors never become remote success.
func (m TransitionSync) Apply(ctx context.Context, v TransitionIntent, actor string) (SyncRecord, error) {
	return m.apply(ctx, v, actor, 0)
}

// RetryFailed is an explicit CAS-reviewed recovery of a known pre-dispatch
// failure. Unknown/pending operations can only be reconciled, never retried.
func (m TransitionSync) RetryFailed(ctx context.Context, operation string, expected int64, actor string) (SyncRecord, error) {
	old, err := m.Get(ctx, operation)
	if err != nil {
		return old, err
	}
	if expected < 1 || old.Revision != expected || old.State != "failed" {
		return old, storage.ErrConflict
	}
	return m.apply(ctx, old.Intent, actor, expected)
}
func (m TransitionSync) apply(ctx context.Context, v TransitionIntent, actor string, retryRevision int64) (SyncRecord, error) {
	if err := v.validate(); err != nil {
		return SyncRecord{}, err
	}
	old, err := m.Get(ctx, v.OperationID)
	fresh := errors.Is(err, storage.ErrNotFound)
	if err != nil && !fresh {
		return SyncRecord{}, err
	}
	if !fresh && old.Intent != v {
		return old, storage.ErrConflict
	}
	connection, err := m.Connections.Get(ctx, v.Connection)
	if err != nil {
		return old, err
	}
	if connection.Revision != v.ConnectionRevision || !connection.Enabled || (!fresh && (connection.SiteURL != old.SiteURL || connection.ProjectID != old.ProjectID)) {
		return old, storage.ErrConflict
	}
	if !fresh && (old.State == "already_target" || old.State == "target_observed") {
		return old, nil
	}
	if !fresh && (old.State == "pending" || old.State == "unknown") {
		return m.reconcile(ctx, old, actor)
	}
	// Failed preflight records remain read-only on replay as well. Configuration or
	// operator recovery must be explicitly reviewed by the lifecycle layer.
	if !fresh && (retryRevision < 1 || old.Revision != retryRevision || old.State != "failed") {
		return old, errors.New("transition preflight failed; review the retained synchronization before recovery")
	}
	if fresh && retryRevision != 0 {
		return old, storage.ErrConflict
	}
	if err := m.checkMapping(ctx, v, connection); err != nil {
		return SyncRecord{}, err
	}
	client, err := m.Connections.OpenForWrite(ctx, v.Connection, v.ConnectionRevision)
	if err != nil {
		return SyncRecord{}, err
	}
	connectionCheck := client.writeCheck
	client.writeCheck = func(ctx context.Context) error {
		if err := connectionCheck(ctx); err != nil {
			return err
		}
		return m.checkMapping(ctx, v, connection)
	}
	record := SyncRecord{Intent: v, SiteURL: connection.SiteURL, ProjectID: connection.ProjectID, State: "pending", CreatedAt: time.Now().UTC()}
	p, preflightErr := client.prepareTransition(ctx, v)
	if preflightErr != nil {
		record.State = "failed"
		record.Error = preflightErr.Error()
	} else {
		record.ObservedStatusID = p.issue.Fields.Status.ID
		if p.alreadyTarget {
			record.State = "already_target"
		}
	}
	ev := domain.NewEvent("tracker.transition_sync_requested", "", map[string]any{"connection": v.Connection, "operation": v.OperationID, "state": record.State}).WithActor(actor)
	if err = m.Connections.Store.AppendEvent(ctx, ev); err != nil {
		return SyncRecord{}, err
	}
	expected := int64(0)
	if !fresh {
		expected = old.Revision
		record.CreatedAt = old.CreatedAt
	}
	record, err = m.store(ctx, record, actor, expected)
	if err != nil {
		return SyncRecord{}, err
	}
	if preflightErr != nil {
		return record, preflightErr
	}
	if p.alreadyTarget {
		return record, nil
	}
	dispatched, sendErr := client.sendPreparedTransition(ctx, v, p)
	if sendErr != nil {
		record.State = "failed"
		if dispatched {
			record.State = "unknown"
		}
		record.Error = sendErr.Error()
		saved, storeErr := m.store(ctx, record, actor, record.Revision)
		if storeErr != nil {
			return record, errors.Join(sendErr, storeErr)
		}
		return saved, sendErr
	}
	return m.reconcile(ctx, record, actor)
}
func (m TransitionSync) reconcile(ctx context.Context, record SyncRecord, actor string) (SyncRecord, error) {
	client, err := m.Connections.Open(ctx, record.Intent.Connection, record.Intent.ConnectionRevision)
	if err != nil {
		return record, err
	}
	issue, readErr := client.Issue(ctx, record.Intent.IssueKey)
	record.State = "unknown"
	record.Error = ErrSyncUnconfirmed.Error()
	if readErr == nil && issue.ID == record.Intent.IssueID && issue.Fields.IssueType.ID == record.Intent.IssueTypeID {
		record.ObservedStatusID = issue.Fields.Status.ID
		if issue.Fields.Status.ID == record.Intent.TargetStatusID {
			record.State = "target_observed"
			record.Error = ""
		}
	}
	// Keep generic uncertainty, not remote content or personal issue text. Revision
	// guard refuses publishing reconciliation under a changed connection.
	saved, err := m.store(ctx, record, actor, record.Revision)
	if err != nil {
		return record, err
	}
	if saved.State != "target_observed" {
		return saved, ErrSyncUnconfirmed
	}
	return saved, nil
}

// checkMapping binds dispatch to the reviewed definition, not its display name.
// Unmapped internal intents remain supported for the low-level conformance tests.
func (m TransitionSync) checkMapping(ctx context.Context, v TransitionIntent, c Connection) error {
	if v.MappingName == "" {
		return nil
	}
	mapping, err := (WorkflowMappings{Connections: m.Connections}).Get(ctx, v.MappingName)
	if err != nil {
		return err
	}
	if !mapping.Enabled || mapping.Revision != v.MappingRevision || mapping.Connection != v.Connection || mapping.ConnectionRevision != v.ConnectionRevision || mapping.SiteURL != c.SiteURL || mapping.ProjectID != c.ProjectID || mapping.IssueTypeID != v.IssueTypeID || mapping.FromStatusID != v.FromStatusID || mapping.TransitionID != v.TransitionID || mapping.TargetStatusID != v.TargetStatusID {
		return fmt.Errorf("%w: workflow mapping changed; review its pinned definition", storage.ErrConflict)
	}
	return nil
}
