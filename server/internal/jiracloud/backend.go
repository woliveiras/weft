package jiracloud

import (
	"context"

	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
)

const (
	MappingPrefix = "tracker/jira-cloud/workflow-mappings/"
	SyncPrefix    = "tracker/jira-cloud/sync/"
)

// Backend reuses the shared Jira mapping and transition journal. Namespaces and
// profile authority remain Cloud-specific; no native DC credential is opened.
type Backend struct{ Profiles Profiles }

func (m Profiles) Backend() jiradc.WorkflowBackend { return Backend{Profiles: m} }
func (b Backend) Store() storage.Store             { return b.Profiles.Store }
func (b Backend) MappingPrefix() string            { return MappingPrefix }
func (b Backend) SyncPrefix() string               { return SyncPrefix }
func (b Backend) ValidateScope(site, project string) error {
	return ValidateScope(site, project)
}
func (b Backend) GetConnection(ctx context.Context, name string) (jiradc.WorkflowScope, error) {
	p, err := b.Profiles.Get(ctx, name)
	return jiradc.WorkflowScope{SiteURL: p.SiteURL, ProjectID: p.ProjectID, Revision: p.Revision, Enabled: p.Enabled, ProjectRestricted: p.ProjectRestricted}, err
}
func (b Backend) SettingGuards(ctx context.Context, name string, revision int64) ([]storage.SettingRevision, error) {
	_, guards, err := b.Profiles.checked(ctx, name, revision, false)
	return guards, err
}
func (b Backend) Open(ctx context.Context, name string, revision int64, writable bool, beforeWrite func(context.Context) error) (jiradc.WorkflowClient, error) {
	return b.Profiles.Open(ctx, name, revision, writable, beforeWrite)
}

var _ jiradc.WorkflowBackend = Backend{}
