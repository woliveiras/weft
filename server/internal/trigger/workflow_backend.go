package trigger

import (
	"context"
	"errors"

	"github.com/woliveiras/weft/server/internal/jiracloud"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
)

func workflowConnection(source SourceConfig) string {
	if source.Kind == "jira" {
		return source.ProfileID
	}
	return source.ConnectionID
}
func workflowRevision(source SourceConfig, trackerRevision int64) int64 {
	if source.Kind == "jira" {
		return source.ProfileRevision
	}
	return trackerRevision
}
func (m Manager) workflowServices(source SourceConfig) (jiradc.WorkflowMappings, jiradc.TransitionSync, error) {
	switch source.Kind {
	case "jira-dc":
		if m.NativeConnections != nil && m.NativeConnections.Store != nil {
			return jiradc.WorkflowMappings{Connections: *m.NativeConnections}, jiradc.TransitionSync{Connections: *m.NativeConnections}, nil
		}
	case "jira":
		if m.CloudProfiles != nil && m.CloudProfiles.Store != nil && source.ProfileID != "" && source.ProfileRevision > 0 {
			backend := m.CloudProfiles.Backend()
			return jiradc.WorkflowMappings{Backend: backend}, jiradc.TransitionSync{Backend: backend}, nil
		}
	}
	return jiradc.WorkflowMappings{}, jiradc.TransitionSync{}, errors.New("reviewed tracker synchronization backend is unavailable")
}
func (m Manager) cloudProfile(ctx context.Context, source SourceConfig, trackerRevision int64) (jiracloud.Profile, error) {
	if m.CloudProfiles == nil || m.CloudProfiles.Store == nil || source.Kind != "jira" || source.ProfileID == "" || source.ProfileRevision < 1 {
		return jiracloud.Profile{}, storage.ErrConflict
	}
	p, err := m.CloudProfiles.Get(ctx, source.ProfileID)
	if err != nil {
		return p, err
	}
	if p.Revision != source.ProfileRevision || !p.Enabled || !p.ProjectRestricted || p.MCPConnection != source.ConnectionID || p.MCPRevision != trackerRevision || p.CloudID != source.CloudID || p.SiteURL != source.SiteURL || p.ProjectID != source.ProjectID {
		return p, storage.ErrConflict
	}
	return p, nil
}
func lifecycleIntentMatches(v LifecycleSync, p jiradc.TransitionIntent) bool {
	return p.OperationID == "lifecycle:"+v.ID && p.Connection == workflowConnection(v.Source) && p.ConnectionRevision == v.ConnectionRevision && p.IssueID == v.IssueID && p.IssueTypeID == v.IssueTypeID && p.IssueKey == v.IssueKey && p.MappingName == v.Mapping.Name && p.MappingRevision == v.Mapping.Revision
}

// LifecycleTransition reads the exact edition-specific historical journal. It
// never resolves a same-named mapping/profile in another edition or dispatches.
func (m Manager) LifecycleTransition(ctx context.Context, v LifecycleSync) (jiradc.SyncRecord, error) {
	if !v.valid() {
		return jiradc.SyncRecord{}, storage.ErrConflict
	}
	_, syncer, err := m.workflowServices(v.Source)
	if err != nil {
		return jiradc.SyncRecord{}, err
	}
	core, err := syncer.Get(ctx, "lifecycle:"+v.ID)
	if err != nil {
		return core, err
	}
	if !lifecycleIntentMatches(v, core.Intent) || core.SiteURL != v.Source.SiteURL || core.ProjectID != v.Source.ProjectID {
		return core, storage.ErrConflict
	}
	return core, nil
}
