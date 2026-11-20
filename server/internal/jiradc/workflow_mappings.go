package jiradc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

var ErrMappingDiscovery = errors.New("Jira workflow discovery failed")

const MappingPrefix = "tracker/jira-dc/workflow-mappings/"

// MappingInput identifies one reviewed issue-type/source-status transition. A
// sample issue proves current availability; each lifecycle use must validate the
// actual issue again because issue type is not a provider workflow identifier.
type MappingInput struct {
	Name               string `json:"name"`
	Connection         string `json:"connection"`
	ConnectionRevision int64  `json:"connectionRevision"`
	SampleIssueKey     string `json:"sampleIssueKey"`
	SampleIssueID      string `json:"sampleIssueId"`
	IssueTypeID        string `json:"issueTypeId"`
	FromStatusID       string `json:"fromStatusId"`
	TransitionID       string `json:"transitionId"`
	TargetStatusID     string `json:"targetStatusId"`
	IssueUpdated       string `json:"issueUpdated"`
	Enabled            bool   `json:"enabled"`
}
type WorkflowMapping struct {
	MappingInput
	SiteURL          string    `json:"siteUrl"`
	ProjectID        string    `json:"projectId"`
	TransitionName   string    `json:"transitionName"`
	TargetStatusName string    `json:"targetStatusName"`
	Revision         int64     `json:"revision"`
	UpdatedAt        time.Time `json:"updatedAt"`
	UpdatedBy        string    `json:"updatedBy"`
}
type WorkflowMappings struct {
	Connections Connections
	Backend     WorkflowBackend
}

// Validate checks the input shape without opening a connection or discovering Jira.
// Saving still revalidates the reviewed sample through WorkflowMappings.Put.
func (v MappingInput) Validate() error { return v.validate() }

func (v MappingInput) validate() error {
	if !connectionName.MatchString(v.Name) || !ValidIssueUpdated(v.IssueUpdated) {
		return fmt.Errorf("%w: name a mapping and review valid issue update metadata", ErrInvalid)
	}
	return (TransitionIntent{OperationID: "mapping", Connection: v.Connection, ConnectionRevision: v.ConnectionRevision, IssueKey: v.SampleIssueKey, IssueID: v.SampleIssueID, IssueTypeID: v.IssueTypeID, FromStatusID: v.FromStatusID, TransitionID: v.TransitionID, TargetStatusID: v.TargetStatusID}).validate()
}
func (m WorkflowMappings) Get(ctx context.Context, name string) (WorkflowMapping, error) {
	if !connectionName.MatchString(name) {
		return WorkflowMapping{}, ErrInvalid
	}
	row, err := m.backend().Store().GetSetting(ctx, m.backend().MappingPrefix()+name)
	if err != nil {
		return WorkflowMapping{}, err
	}
	var out WorkflowMapping
	if json.Unmarshal(row.Value, &out) != nil || out.Name != name || out.MappingInput.validate() != nil || m.backend().ValidateScope(out.SiteURL, out.ProjectID) != nil || out.TransitionName == "" || out.TargetStatusName == "" || out.UpdatedAt.IsZero() {
		return WorkflowMapping{}, errors.New("stored Jira workflow mapping is corrupt")
	}
	out.Revision = row.Revision
	return out, nil
}
func (m WorkflowMappings) List(ctx context.Context) ([]WorkflowMapping, error) {
	rows, err := m.backend().Store().ListSettings(ctx, m.backend().MappingPrefix())
	if err != nil {
		return nil, err
	}
	if len(rows) > 1000 {
		return nil, errors.New("Jira workflow mapping catalogue exceeds its bound")
	}
	out := make([]WorkflowMapping, 0, len(rows))
	for _, row := range rows {
		var v WorkflowMapping
		if json.Unmarshal(row.Value, &v) != nil || row.Key != m.backend().MappingPrefix()+v.Name {
			return nil, errors.New("stored Jira workflow mapping is corrupt")
		}
		v, err = m.Get(ctx, v.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (m WorkflowMappings) Put(ctx context.Context, in MappingInput, expected int64, actor string) (WorkflowMapping, error) {
	if err := in.validate(); err != nil {
		return WorkflowMapping{}, err
	}
	if expected < 0 {
		return WorkflowMapping{}, ErrInvalid
	}
	old, err := m.Get(ctx, in.Name)
	fresh := errors.Is(err, storage.ErrNotFound)
	if err != nil && !fresh {
		return WorkflowMapping{}, err
	}
	if (fresh && expected != 0) || (!fresh && (expected == 0 || old.Revision != expected)) {
		return WorkflowMapping{}, storage.ErrConflict
	}
	next := WorkflowMapping{MappingInput: in, UpdatedAt: time.Now().UTC(), UpdatedBy: actor}
	guards := []storage.SettingRevision{}
	// Disabling exactly the saved definition must work even when the connection or
	// sample issue is inaccessible. It never dispatches discovery or a mutation.
	disabledInput := old.MappingInput
	disabledInput.Enabled = false
	if !fresh && !in.Enabled && in == disabledInput {
		next.SiteURL, next.ProjectID = old.SiteURL, old.ProjectID
		next.TransitionName, next.TargetStatusName = old.TransitionName, old.TargetStatusName
	} else {
		c, err := m.backend().GetConnection(ctx, in.Connection)
		if err != nil {
			return WorkflowMapping{}, err
		}
		if c.Revision != in.ConnectionRevision || !c.Enabled {
			return WorkflowMapping{}, storage.ErrConflict
		}
		client, err := m.backend().Open(ctx, in.Connection, in.ConnectionRevision, true, nil)
		if err != nil {
			return WorkflowMapping{}, err
		}
		d, err := client.DiscoverTransitions(ctx, in.SampleIssueKey)
		if err != nil {
			return WorkflowMapping{}, fmt.Errorf("%w: %w", ErrMappingDiscovery, err)
		}
		if d.IssueID != in.SampleIssueID || d.IssueTypeID != in.IssueTypeID || d.Current.ID != in.FromStatusID || d.Updated != in.IssueUpdated {
			return WorkflowMapping{}, fmt.Errorf("%w: sample issue changed since discovery; review it again", storage.ErrConflict)
		}
		found := false
		for _, tr := range d.Transitions {
			if tr.ID != in.TransitionID {
				continue
			}
			if tr.To.ID != in.TargetStatusID || tr.Fields == nil {
				return WorkflowMapping{}, fmt.Errorf("%w: mapped destination or field metadata changed", ErrInvalid)
			}
			for _, f := range tr.Fields {
				if f.Required == nil || *f.Required {
					return WorkflowMapping{}, fmt.Errorf("%w: required transition fields are not supported by this mapping profile", ErrInvalid)
				}
			}
			found = true
			next.TransitionName, next.TargetStatusName = tr.Name, tr.To.Name
		}
		if !found {
			return WorkflowMapping{}, fmt.Errorf("%w: select a discovered transition available for this issue", ErrInvalid)
		}
		next.SiteURL, next.ProjectID = c.SiteURL, c.ProjectID
		guards, err = m.backend().SettingGuards(ctx, in.Connection, in.ConnectionRevision)
		if err != nil {
			return WorkflowMapping{}, err
		}
	}
	ev := domain.NewEvent("tracker.workflow_mapping_change_requested", "", map[string]any{"mapping": in.Name, "connection": in.Connection, "siteUrl": next.SiteURL, "projectId": next.ProjectID, "expectedRevision": expected, "enabled": in.Enabled}).WithActor(actor)
	if err = m.backend().Store().AppendEvent(ctx, ev); err != nil {
		return WorkflowMapping{}, err
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return WorkflowMapping{}, err
	}
	rev, err := m.backend().Store().PutSettingIf(ctx, m.backend().MappingPrefix()+in.Name, raw, expected, actor, guards)
	if err != nil {
		return WorkflowMapping{}, err
	}
	next.Revision = rev
	return next, nil
}
