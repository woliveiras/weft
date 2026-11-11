// Package mcpconfig manages remote MCP connections, discovery and gateway tools.
package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
)

const prefix = "mcp/connections/"

var ErrInvalid = errors.New("invalid MCP configuration")
var identifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var toolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

type Credential struct {
	Username string `json:"username,omitempty"`
	Value    string `json:"value,omitempty"`
	Clear    bool   `json:"clear,omitempty"`
}
type Input struct {
	LocalProcess       *LocalProcess      `json:"localProcess,omitempty"`
	ProcessEnvironment *map[string]string `json:"processEnvironment,omitempty"`
	ArgumentPolicy     []ArgumentRule     `json:"argumentPolicy,omitempty"`
	AtlassianPolicy    *AtlassianPolicy   `json:"atlassianPolicy,omitempty"`
	TransportProfile   string             `json:"transportProfile,omitempty"`
	MetadataHeaders    map[string]string  `json:"metadataHeaders,omitempty"`
	CredentialType     string             `json:"credentialType,omitempty"`
	TemplateParameters map[string]string  `json:"templateParameters,omitempty"`
	CallsPerMinute     int                `json:"callsPerMinute,omitempty"`
	OAuth              *OAuthConfig       `json:"oauth,omitempty"`
	OAuthClientSecret  *Credential        `json:"oauthClientSecret,omitempty"`
	Template           *TemplateRef       `json:"template,omitempty"`
	ID                 string             `json:"id"`
	Name               string             `json:"name"`
	Service            string             `json:"service"`
	Endpoint           string             `json:"endpoint"`
	Enabled            bool               `json:"enabled"`
	Credential         *Credential        `json:"credential,omitempty"`
}
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}
type View struct {
	LocalProcess         *LocalProcess     `json:"localProcess,omitempty"`
	EnvironmentKeys      []string          `json:"environmentKeys,omitempty"`
	ArgumentPolicy       []ArgumentRule    `json:"argumentPolicy,omitempty"`
	AtlassianPolicy      *AtlassianPolicy  `json:"atlassianPolicy,omitempty"`
	TransportProfile     string            `json:"transportProfile,omitempty"`
	MetadataHeaders      map[string]string `json:"metadataHeaders,omitempty"`
	CredentialType       string            `json:"credentialType,omitempty"`
	TemplateParameters   map[string]string `json:"templateParameters,omitempty"`
	CallsPerMinute       int               `json:"callsPerMinute,omitempty"`
	OAuth                *OAuthView        `json:"oauth,omitempty"`
	Template             *TemplateRef      `json:"template,omitempty"`
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	Service              string            `json:"service"`
	Endpoint             string            `json:"endpoint"`
	Enabled              bool              `json:"enabled"`
	Revision             int64             `json:"revision"`
	Status               string            `json:"status"`
	CredentialConfigured bool              `json:"credentialConfigured"`
	Tools                []Tool            `json:"tools"`
	ToolsRevision        int64             `json:"toolsRevision"`
	LastTest             *time.Time        `json:"lastTest,omitempty"`
	Error                string            `json:"error,omitempty"`
	CreatedAt            time.Time         `json:"createdAt"`
	UpdatedAt            time.Time         `json:"updatedAt"`
	UpdatedBy            string            `json:"updatedBy"`
}
type record struct {
	ProcessEnvironmentSealed string              `json:"processEnvironmentSealed,omitempty"`
	Deleted                  bool                `json:"deleted,omitempty"`
	Registration             *clientRegistration `json:"registration,omitempty"`
	OAuthClientSealed        string              `json:"oauthClientSealed,omitempty"`
	OAuthRefreshSealed       string              `json:"oauthRefreshSealed,omitempty"`
	OAuthMeta                oauthMetadata       `json:"oauthMetadata,omitempty"`
	View
	Sealed string `json:"sealed,omitempty"`
}
type Manager struct {
	AllowLocalProcesses bool             // native launcher capability, never a request field
	QuotaNow            func() time.Time // trusted clock injection for deterministic quota tests
	OAuthClient         *http.Client     // trusted test/platform injection, never user-supplied
	Store               storage.Store
	Box                 *secrets.Box
	Bus                 *events.Bus
	Discover            func(context.Context, string, string) ([]Tool, error)
	Call                func(context.Context, string, string, string, json.RawMessage) (json.RawMessage, error)
}

func New(store storage.Store, box *secrets.Box, bus *events.Bus) *Manager {
	return &Manager{Store: store, Box: box, Bus: bus, Discover: Discover, Call: Call}
}
func validate(in Input) error {
	if err := validateLocalProcess(in); err != nil {
		return err
	}
	if err := validateArgumentPolicy(in.ArgumentPolicy); err != nil {
		return err
	}
	if err := validateAtlassianPolicy(in); err != nil {
		return err
	}
	if in.TransportProfile != "" && in.TransportProfile != statelessProfile {
		return fmt.Errorf("%w: unsupported transport profile", ErrInvalid)
	}
	if in.TransportProfile != "" && in.Template != nil {
		return fmt.Errorf("%w: detach the template before selecting another transport profile", ErrInvalid)
	}
	if err := validateMetadataHeaders(in); err != nil {
		return err
	}
	if err := validateCredentialMode(in); err != nil {
		return err
	}
	if in.CallsPerMinute < 0 || in.CallsPerMinute > 60000 {
		return fmt.Errorf("%w: callsPerMinute must be between 0 and 60000", ErrInvalid)
	}
	if err := validateOAuth(in.OAuth); err != nil {
		return err
	}
	if in.OAuth != nil && in.Credential != nil {
		return fmt.Errorf("%w: OAuth cannot be combined with a bearer credential", ErrInvalid)
	}
	if in.OAuthClientSecret != nil && (in.OAuth == nil || len(in.OAuthClientSecret.Value) > 16384 || in.OAuthClientSecret.Clear && in.OAuthClientSecret.Value != "") {
		return fmt.Errorf("%w: invalid OAuth client credential", ErrInvalid)
	}
	if in.OAuth != nil && !manualOAuth(in.OAuth) && in.OAuthClientSecret != nil && !in.OAuthClientSecret.Clear {
		return fmt.Errorf("%w: automatic registration cannot accept a manual client secret", ErrInvalid)
	}
	if err := validateTemplate(in); err != nil {
		return err
	}
	u, e := url.Parse(in.Endpoint)
	if !identifier.MatchString(in.ID) || len(strings.TrimSpace(in.Name)) == 0 || len(in.Name) > 120 {
		return fmt.Errorf("%w: valid id and display name required", ErrInvalid)
	}
	if in.LocalProcess == nil && (e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.RawQuery != "" || u.ForceQuery) && !flatAtlassianEndpoint(in.Endpoint) || u.Fragment != "" || len(in.Endpoint) > 2048) {
		return fmt.Errorf("%w: endpoint must be HTTPS without user info or fragment; only Atlassian v2 tools=all is an allowed query", ErrInvalid)
	}
	switch in.Service {
	case "databricks", "sentry", "github", "atlassian", "gitlab", "bitbucket", "jira", "confluence", "generic":
	default:
		return fmt.Errorf("%w: unknown service", ErrInvalid)
	}
	if err := validateServiceOAuth(in); err != nil {
		return err
	}
	if c := in.Credential; c != nil && (len(c.Value) > 16384 || strings.ContainsAny(c.Value, "\r\n") || c.Clear && c.Value != "") {
		return fmt.Errorf("%w: invalid credential update", ErrInvalid)
	}
	return nil
}
func (m *Manager) load(ctx context.Context, id string) (record, error) {
	var r record
	if !identifier.MatchString(id) {
		return r, fmt.Errorf("%w: invalid connection id", ErrInvalid)
	}
	s, e := m.Store.GetSetting(ctx, prefix+id)
	if e != nil {
		return r, e
	}
	e = json.Unmarshal(s.Value, &r)
	r.Revision = s.Revision
	if e == nil && r.Deleted {
		return record{}, storage.ErrNotFound
	}
	return r, e
}
func (m *Manager) Get(ctx context.Context, id string) (View, error) {
	r, e := m.load(ctx, id)
	return operationCatalogue(r.View), e
}
func (m *Manager) List(ctx context.Context) ([]View, error) {
	ss, e := m.Store.ListSettings(ctx, prefix)
	if e != nil {
		return nil, e
	}
	out := []View{}
	for _, s := range ss {
		var r record
		if e = json.Unmarshal(s.Value, &r); e != nil {
			return nil, e
		}
		if r.Deleted {
			continue
		}
		r.Revision = s.Revision
		out = append(out, operationCatalogue(r.View))
	}
	return out, nil
}
func (m *Manager) save(ctx context.Context, r record, rev int64, actor, event string) (View, error) {
	r.View = operationCatalogue(r.View)
	r.UpdatedAt = time.Now().UTC()
	r.UpdatedBy = actor
	r.CredentialConfigured = r.Sealed != ""
	if r.OAuth != nil {
		r.OAuth.ClientSecretConfigured = r.OAuthClientSealed != ""
		if r.Registration != nil {
			r.OAuth.RegistrationStatus = r.Registration.Status
			r.OAuth.RegistrationDiagnostic = r.Registration.Diagnostic
			r.OAuth.EffectiveRegistrationMode = r.Registration.Mode
		}
	}
	b, e := json.Marshal(r)
	if r.Deleted {
		b, e = json.Marshal(map[string]any{"id": r.ID, "deleted": true, "updatedAt": r.UpdatedAt, "updatedBy": actor})
	}
	if e != nil {
		return View{}, e
	}
	r.Revision, e = m.Store.PutSetting(ctx, prefix+r.ID, b, rev, actor)
	if e != nil {
		return View{}, e
	}
	if m.Bus != nil {
		m.Bus.Publish(ctx, domain.NewEvent(domain.EventType(event), "", map[string]any{"connectionId": r.ID, "revision": r.Revision, "status": r.Status, "toolCount": len(r.Tools)}).WithActor(actor))
	}
	return r.View, nil
}
func (m *Manager) Put(ctx context.Context, in Input, revision int64, actor string) (View, error) {
	if in.LocalProcess != nil && !m.AllowLocalProcesses {
		return View{}, fmt.Errorf("%w: local processes require the native installation", ErrInvalid)
	}
	if e := validate(in); e != nil {
		return View{}, e
	}
	r, e := m.load(ctx, in.ID)
	if revision == 0 {
		if e == nil {
			return View{}, storage.ErrConflict
		}
		if !errors.Is(e, storage.ErrNotFound) {
			return View{}, e
		}
		r = record{View: View{ID: in.ID, CreatedAt: time.Now().UTC(), Tools: []Tool{}}}
	} else {
		if e != nil {
			return View{}, e
		}
		if r.Revision != revision {
			return View{}, storage.ErrConflict
		}
	}
	if err := m.updateProcess(&r, in); err != nil {
		return View{}, err
	}
	if (r.Endpoint != "" && r.Endpoint != in.Endpoint || credentialKind(r.CredentialType) != credentialKind(in.CredentialType) || !reflect.DeepEqual(r.MetadataHeaders, in.MetadataHeaders)) && r.Sealed != "" && r.OAuth == nil && in.OAuth == nil && (in.Credential == nil || !in.Credential.Clear && in.Credential.Value == "") {
		return View{}, fmt.Errorf("%w: replace or clear the credential when changing endpoint, credential type or metadata headers", ErrInvalid)
	}
	var oldOAuth *OAuthConfig
	if r.OAuth != nil {
		oldOAuth = &r.OAuth.OAuthConfig
	}
	oauthChanged := r.TransportProfile != in.TransportProfile || !reflect.DeepEqual(r.MetadataHeaders, in.MetadataHeaders) || r.Service != in.Service || !reflect.DeepEqual(oldOAuth, in.OAuth) || in.OAuthClientSecret != nil || r.Endpoint != in.Endpoint
	if oauthChanged {
		r.Registration = nil
		if in.OAuth != nil && !manualOAuth(in.OAuth) {
			r.OAuthClientSealed = ""
		}
		r.OAuthRefreshSealed = ""
		r.OAuthMeta = oauthMetadata{}
		if oldOAuth != nil || in.OAuth != nil {
			r.Sealed = ""
		}
		// A different issuer/client must never inherit the previous client secret.
		if in.OAuth == nil || oldOAuth == nil || oldOAuth.Issuer != in.OAuth.Issuer || oldOAuth.ClientID != in.OAuth.ClientID || oldOAuth.RegistrationMode != in.OAuth.RegistrationMode {
			r.OAuthClientSealed = ""
		}
		r.OAuth = nil
		if in.OAuth != nil {
			r.OAuth = &OAuthView{OAuthConfig: *in.OAuth, Status: "authorization_required"}
		}
	}
	if in.OAuthClientSecret != nil {
		if in.OAuthClientSecret.Clear {
			r.OAuthClientSealed = ""
		} else if in.OAuthClientSecret.Value != "" {
			r.OAuthClientSealed, e = m.Box.Seal([]byte(in.OAuthClientSecret.Value), prefix+in.ID+"/oauth-client")
			if e != nil {
				return View{}, e
			}
		}
	}
	changed := !reflect.DeepEqual(r.LocalProcess, in.LocalProcess) || in.ProcessEnvironment != nil || !reflect.DeepEqual(r.ArgumentPolicy, in.ArgumentPolicy) || !reflect.DeepEqual(r.AtlassianPolicy, in.AtlassianPolicy) || !reflect.DeepEqual(r.MetadataHeaders, in.MetadataHeaders) || r.CredentialType != in.CredentialType || r.Endpoint != in.Endpoint || in.Credential != nil || r.Enabled != in.Enabled || oauthChanged
	r.LocalProcess = in.LocalProcess
	r.ArgumentPolicy = in.ArgumentPolicy
	r.AtlassianPolicy = in.AtlassianPolicy
	r.TransportProfile = in.TransportProfile
	r.MetadataHeaders = in.MetadataHeaders
	r.CredentialType = in.CredentialType
	r.CallsPerMinute = in.CallsPerMinute
	r.Template = in.Template
	r.TemplateParameters = in.TemplateParameters
	r.Name = in.Name
	r.Service = in.Service
	r.Endpoint = in.Endpoint
	r.Enabled = in.Enabled
	if in.Credential != nil {
		if in.Credential.Clear {
			r.Sealed = ""
		} else if in.Credential.Value != "" {
			value := in.Credential.Value
			if in.CredentialType == "basic" {
				value = in.Credential.Username + ":" + value
			}
			r.Sealed, e = m.Box.Seal([]byte(value), prefix+in.ID)
			if e != nil {
				return View{}, e
			}
		}
	}
	if changed {
		r.Tools = []Tool{}
		r.ToolsRevision++
		r.LastTest = nil
		r.Error = ""
		r.Status = "untested"
	}
	if !r.Enabled {
		r.Status = "disabled"
	}
	return m.save(ctx, r, revision, actor, "mcp.connection.changed")
}
func (m *Manager) Refresh(ctx context.Context, id, actor string) (View, error) {
	r, e := m.load(ctx, id)
	if e != nil {
		return View{}, e
	}
	if !r.Enabled {
		return View{}, fmt.Errorf("%w: connection disabled", ErrInvalid)
	}
	var token string
	r, token, e = m.accessToken(ctx, r)
	if e != nil {
		return View{}, e
	}
	opctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	started := time.Now()
	catalog, discoverErr := m.discoverConnection(credentialContext(opctx, r.View), r, token)
	if r.OAuth != nil {
		var detail *DiscoveryError
		if errors.As(discoverErr, &detail) && detail.Reason == "MCP HTTP status 401" {
			return m.requireAuthorization(context.WithoutCancel(ctx), r)
		}
	}
	now := time.Now().UTC()
	r.LastTest = &now
	r.ToolsRevision++
	r.Tools = []Tool{}
	r.Error = ""
	r.Status = "ready"
	if discoverErr != nil {
		r.Status = "unavailable"
		r.Error = "Discovery failed: check endpoint, TLS, network policy and credentials."
		var detail *DiscoveryError
		if errors.As(discoverErr, &detail) {
			r.Error = "Discovery failed: " + detail.Reason
		}
		if guidance := accessGuidance(discoverErr); guidance != "" {
			r.Error = guidance
		}
	} else {
		r.Tools = catalog
	}
	result, e := m.save(ctx, r, r.Revision, actor, "mcp.tools.refreshed")
	if m.Bus != nil {
		outcome := "success"
		if discoverErr != nil || e != nil {
			outcome = "failed"
		}
		m.Bus.Publish(ctx, domain.NewEvent("mcp.connection.tested", "", map[string]any{"connectionId": id, "outcome": outcome, "durationMs": time.Since(started).Milliseconds()}).WithActor(actor))
	}
	return result, e
}

// ValidateGrants only accepts exact, currently discovered tool names. Wildcards
// do not opt agents into future connection catalogues.
func (m *Manager) ValidateGrants(ctx context.Context, g *domain.Graph) error {
	grants := append([]domain.ToolGrant{}, g.Spec.Defaults.Tools...)
	for _, n := range g.Spec.Nodes {
		grants = append(grants, n.Tools...)
	}
	var views []View
	for _, grant := range grants {
		if !strings.HasPrefix(grant.Name, "mcp.") {
			continue
		}
		if views == nil {
			var e error
			views, e = m.List(ctx)
			if e != nil {
				return e
			}
		}
		found := false
		for _, v := range views {
			if !v.Enabled || v.Status != "ready" {
				continue
			}
			for _, t := range v.Tools {
				if grant.Name == "mcp."+v.ID+"."+t.Name {
					found = true
				}
			}
		}
		if !found {
			return fmt.Errorf("%w: MCP grant must name an enabled discovered tool", ErrInvalid)
		}
	}
	return nil
}

// Delete removes local configuration and credentials, reserving the ID permanently
// so historical agent grants cannot acquire authority over a replacement connection.
// Remote grants/registrations are not deleted by this operation.
func (m *Manager) Delete(ctx context.Context, id string, revision int64, actor string) error {
	r, err := m.load(ctx, id)
	if err != nil {
		return err
	}
	if revision <= 0 || r.Revision != revision {
		return storage.ErrConflict
	}
	if r.Enabled {
		return fmt.Errorf("%w: disable the connection before deletion", ErrInvalid)
	}
	if r.OAuth != nil && (r.OAuth.Status != "revoked" || r.OAuth.Revocation == "pending") {
		return fmt.Errorf("%w: revoke OAuth access before deletion", ErrInvalid)
	}
	if err := m.updatePending(ctx, func(entries map[string]oauthPending) error {
		for state, pending := range entries {
			if pending.ID == id {
				delete(entries, state)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	tombstone := record{Deleted: true, View: View{ID: id, Status: "deleted"}}
	if _, err = m.save(ctx, tombstone, revision, actor, "mcp.connection.deleted"); err != nil {
		return err
	}
	return nil
}
