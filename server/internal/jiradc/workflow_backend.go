package jiradc

import (
	"context"

	"github.com/woliveiras/weft/server/internal/storage"
)

// WorkflowScope is the credential-free authority snapshot used by the shared
// mapping and transition journal. Names are local connection/profile names.
type WorkflowScope struct {
	SiteURL           string
	ProjectID         string
	Revision          int64
	Enabled           bool
	ProjectRestricted bool
}

// WorkflowClient adapts provider reads and one transition write to the shared
// durable core. AuthorizeWrite must recheck the current reviewed authority and
// the beforeWrite callback passed to Open, including immediately before sending.
// SendTransition returns dispatched=false only when no request was attempted;
// once entering an ambiguous transport boundary it must return true, even on
// error. It must not retry or include arbitrary issue fields in the request.
type WorkflowClient interface {
	Issue(context.Context, string) (Issue, error)
	DiscoverTransitions(context.Context, string) (TransitionDiscovery, error)
	AuthorizeWrite(context.Context) error
	SendTransition(context.Context, TransitionIntent) (dispatched bool, err error)
}

// WorkflowBackend supplies edition-specific scope, clients and local revision
// guards. SettingGuards must include every persisted authority (for example a
// Cloud profile and its MCP connection), not merely the profile's own revision.
// Open writable must require project-restricted authority and install beforeWrite
// in the client's authorization check. Read-only Open must never authorize writes.
// The journal, metadata preparation, snapshot rechecks and uncertainty policy
// remain shared. Backend configuration is internal, never an agent grant.
type WorkflowBackend interface {
	Store() storage.Store
	MappingPrefix() string
	SyncPrefix() string
	ValidateScope(siteURL, projectID string) error
	GetConnection(context.Context, string) (WorkflowScope, error)
	SettingGuards(context.Context, string, int64) ([]storage.SettingRevision, error)
	Open(ctx context.Context, name string, revision int64, writable bool, beforeWrite func(context.Context) error) (WorkflowClient, error)
}

type dcWorkflowBackend struct{ Connections }

func (b dcWorkflowBackend) Store() storage.Store  { return b.Connections.Store }
func (b dcWorkflowBackend) MappingPrefix() string { return MappingPrefix }
func (b dcWorkflowBackend) SyncPrefix() string    { return SyncPrefix }
func (b dcWorkflowBackend) ValidateScope(siteURL, projectID string) error {
	return ValidateScope(siteURL, projectID)
}
func (b dcWorkflowBackend) GetConnection(ctx context.Context, name string) (WorkflowScope, error) {
	c, err := b.Get(ctx, name)
	return WorkflowScope{SiteURL: c.SiteURL, ProjectID: c.ProjectID, Revision: c.Revision, Enabled: c.Enabled, ProjectRestricted: c.ProjectRestricted}, err
}
func (b dcWorkflowBackend) SettingGuards(_ context.Context, name string, revision int64) ([]storage.SettingRevision, error) {
	return []storage.SettingRevision{{Key: ConnectionPrefix + name, Revision: revision}}, nil
}
func (b dcWorkflowBackend) Open(ctx context.Context, name string, revision int64, writable bool, beforeWrite func(context.Context) error) (WorkflowClient, error) {
	if !writable {
		return b.Connections.Open(ctx, name, revision)
	}
	c, err := b.OpenForWrite(ctx, name, revision)
	if err != nil {
		return nil, err
	}
	check := c.writeCheck
	c.writeCheck = func(ctx context.Context) error {
		if err := check(ctx); err != nil {
			return err
		}
		if beforeWrite != nil {
			return beforeWrite(ctx)
		}
		return nil
	}
	return c, nil
}

func (m TransitionSync) backend() WorkflowBackend {
	if m.Backend != nil {
		return m.Backend
	}
	return dcWorkflowBackend{m.Connections}
}
func (m WorkflowMappings) backend() WorkflowBackend {
	if m.Backend != nil {
		return m.Backend
	}
	return dcWorkflowBackend{m.Connections}
}

// AuthorizeWrite preserves the native client's current-connection write gate.
func (c *Client) AuthorizeWrite(ctx context.Context) error { return c.authorizeWrite(ctx) }

// SendTransition is called only after the durable reservation and shared issue
// snapshot recheck. The native transport also rechecks authorization before POST.
func (c *Client) SendTransition(ctx context.Context, v TransitionIntent) (bool, error) {
	if err := c.authorizeWrite(ctx); err != nil {
		return false, err
	}
	payload := map[string]any{"transition": map[string]string{"id": v.TransitionID}}
	return true, c.do(ctx, "POST", "/issue/"+v.IssueID+"/transitions", payload, nil)
}
