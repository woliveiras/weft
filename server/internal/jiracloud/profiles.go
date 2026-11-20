// Package jiracloud implements explicitly reviewed Jira Cloud workflow scopes.
// Profiles hold no credential: transport remains bound to an existing compatible
// first-party static MCP connection and its exact native revision.
package jiracloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/storage"
)

const (
	ProfilePrefix = "tracker/jira-cloud/profiles/"
	mcpPrefix     = "mcp/connections/"
	apiOrigin     = "https://api.atlassian.com"
)

var (
	ErrInvalid       = errors.New("invalid Jira Cloud workflow profile")
	ErrUnavailable   = errors.New("Jira Cloud workflow transport is unavailable")
	ErrWriteDisabled = errors.New("Jira Cloud transitions require a confirmed project-restricted technical account")
	identifier       = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	numericID        = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	cloudID          = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
)

// TrackerAPI is satisfied by mcpconfig.TrackerAPI without importing a trigger.
// Revision MUST validate first-party static REST compatibility, not merely read
// a counter. Call MUST retain origin, revision, redirect and credential guards.
// The API and profile store must share the same installation storage.
type TrackerAPI interface {
	Revision(context.Context, string, string) (int64, error)
	Call(context.Context, string, string, int64, string, string, any) (json.RawMessage, int, error)
}

type Input struct {
	Name              string `json:"name"`
	MCPConnection     string `json:"mcpConnection"`
	MCPRevision       int64  `json:"mcpRevision"`
	CloudID           string `json:"cloudId"`
	SiteURL           string `json:"siteUrl"`
	ProjectID         string `json:"projectId"`
	Enabled           bool   `json:"enabled"`
	ProjectRestricted bool   `json:"projectRestricted"`
}

type Profile struct {
	Input
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy string    `json:"updatedBy"`
}

type Profiles struct {
	Store storage.Store
	API   TrackerAPI
}

func normalize(in Input) (Input, error) {
	if !identifier.MatchString(in.Name) || !identifier.MatchString(in.MCPConnection) || in.MCPRevision < 1 || !cloudID.MatchString(in.CloudID) {
		return in, ErrInvalid
	}
	in.SiteURL = strings.TrimSuffix(in.SiteURL, "/")
	if in.SiteURL != apiOrigin+"/ex/jira/"+in.CloudID || !numericID.MatchString(in.ProjectID) {
		return in, ErrInvalid
	}
	return in, nil
}

// ValidateScope accepts only the Cloud gateway's explicit site identity. It
// cannot turn a Data Center URL or a tenant URL into a credential destination.
func ValidateScope(siteURL, projectID string) error {
	const prefix = apiOrigin + "/ex/jira/"
	if !strings.HasPrefix(siteURL, prefix) || !cloudID.MatchString(strings.TrimPrefix(siteURL, prefix)) || !numericID.MatchString(projectID) {
		return ErrInvalid
	}
	return nil
}

func (m Profiles) Get(ctx context.Context, name string) (Profile, error) {
	if !identifier.MatchString(name) {
		return Profile{}, ErrInvalid
	}
	if m.Store == nil {
		return Profile{}, ErrUnavailable
	}
	row, err := m.Store.GetSetting(ctx, ProfilePrefix+name)
	if err != nil {
		return Profile{}, err
	}
	var out Profile
	if json.Unmarshal(row.Value, &out) != nil || out.Name != name {
		return Profile{}, errors.New("stored Jira Cloud workflow profile is corrupt")
	}
	in, err := normalize(out.Input)
	if err != nil || in != out.Input || out.UpdatedAt.IsZero() || out.UpdatedBy == "" {
		return Profile{}, errors.New("stored Jira Cloud workflow profile is invalid")
	}
	out.Revision = row.Revision
	return out, nil
}

// mcpGuard checks compatibility through TrackerAPI.Revision, which rejects MCP
// OAuth/custom audiences. The setting metadata is then used for an atomic guard;
// the credential-bearing setting value is never decoded or opened here.
func (m Profiles) mcpGuard(ctx context.Context, in Input) (storage.SettingRevision, error) {
	if m.API == nil || m.Store == nil {
		return storage.SettingRevision{}, ErrUnavailable
	}
	key := mcpPrefix + in.MCPConnection
	row, err := m.Store.GetSetting(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.SettingRevision{}, fmt.Errorf("%w: a saved first-party static Jira REST-compatible MCP connection is required", ErrInvalid)
	}
	if err != nil {
		return storage.SettingRevision{}, err
	}
	if row.Revision != in.MCPRevision {
		return storage.SettingRevision{}, storage.ErrConflict
	}
	revision, err := m.API.Revision(ctx, in.MCPConnection, "jira")
	if errors.Is(err, mcpconfig.ErrTrackerCompatibility) || errors.Is(err, storage.ErrNotFound) {
		return storage.SettingRevision{}, fmt.Errorf("%w: a first-party static Jira REST-compatible MCP connection is required", ErrInvalid)
	}
	if err != nil {
		return storage.SettingRevision{}, err
	}
	if row.Revision != revision {
		return storage.SettingRevision{}, storage.ErrConflict
	}
	return storage.SettingRevision{Key: key, Revision: revision}, nil
}

// Preview performs local compatibility/scope reads only; it does not call Jira,
// decrypt a credential, create a profile, or confirm remote project permissions.
func (m Profiles) Preview(ctx context.Context, in Input) (Profile, error) {
	in, err := normalize(in)
	if err != nil {
		return Profile{}, err
	}
	_, err = m.mcpGuard(ctx, in)
	return Profile{Input: in}, err
}

func (m Profiles) List(ctx context.Context) ([]Profile, error) {
	if m.Store == nil {
		return nil, ErrUnavailable
	}
	rows, err := m.Store.ListSettings(ctx, ProfilePrefix)
	if err != nil {
		return nil, err
	}
	if len(rows) > 1000 {
		return nil, errors.New("Jira Cloud workflow profile catalogue exceeds its bound")
	}
	out := make([]Profile, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimPrefix(row.Key, ProfilePrefix)
		p, err := m.Get(ctx, name)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (m Profiles) Put(ctx context.Context, in Input, expected int64, actor string) (Profile, error) {
	in, err := normalize(in)
	if err != nil || expected < 0 || actor == "" || len(actor) > 200 || !utf8.ValidString(actor) || strings.ContainsAny(actor, "\x00\r\n") {
		return Profile{}, ErrInvalid
	}
	old, err := m.Get(ctx, in.Name)
	fresh := errors.Is(err, storage.ErrNotFound)
	if err != nil && !fresh {
		return Profile{}, err
	}
	if (fresh && expected != 0) || (!fresh && (expected == 0 || old.Revision != expected)) {
		return Profile{}, storage.ErrConflict
	}
	if !fresh && old.Input == in {
		return old, nil
	}
	var guards []storage.SettingRevision
	disabled := old.Input
	disabled.Enabled = false
	if fresh || in != disabled {
		guard, err := m.mcpGuard(ctx, in)
		if err != nil {
			return Profile{}, err
		}
		guards = append(guards, guard)
	}
	ev := domain.NewEvent("tracker.jira_cloud_profile_change_requested", "", map[string]any{"profile": in.Name, "mcpConnection": in.MCPConnection, "mcpRevision": in.MCPRevision, "cloudId": in.CloudID, "siteUrl": in.SiteURL, "projectId": in.ProjectID, "expectedRevision": expected, "enabled": in.Enabled, "projectRestricted": in.ProjectRestricted}).WithActor(actor)
	if err := m.Store.AppendEvent(ctx, ev); err != nil {
		return Profile{}, err
	}
	next := Profile{Input: in, UpdatedAt: time.Now().UTC(), UpdatedBy: actor}
	raw, err := json.Marshal(next)
	if err != nil {
		return Profile{}, err
	}
	next.Revision, err = m.Store.PutSettingIf(ctx, ProfilePrefix+in.Name, raw, expected, actor, guards)
	return next, err
}

func (m Profiles) checked(ctx context.Context, name string, revision int64, writable bool) (Profile, []storage.SettingRevision, error) {
	p, err := m.Get(ctx, name)
	if err != nil {
		return p, nil, err
	}
	if revision < 1 || p.Revision != revision || !p.Enabled {
		return p, nil, storage.ErrConflict
	}
	if writable && !p.ProjectRestricted {
		return p, nil, ErrWriteDisabled
	}
	guard, err := m.mcpGuard(ctx, p.Input)
	if err != nil {
		return p, nil, err
	}
	return p, []storage.SettingRevision{{Key: ProfilePrefix + name, Revision: revision}, guard}, nil
}

func (m Profiles) Open(ctx context.Context, name string, revision int64, writable bool, beforeWrite func(context.Context) error) (*Client, error) {
	p, _, err := m.checked(ctx, name, revision, writable)
	if err != nil {
		return nil, err
	}
	return &Client{profiles: m, profile: p, writable: writable, beforeWrite: beforeWrite}, nil
}

func scopeError() error {
	return fmt.Errorf("%w: Jira Cloud returned a different issue or project", ErrInvalid)
}
