package mcpconfig

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// TemplateRef pins public defaults. It never conveys credentials or tool grants.
type TemplateRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}
type TemplateParameter struct {
	InputType string   `json:"inputType,omitempty"`
	Name      string   `json:"name"`
	Label     string   `json:"label"`
	Default   string   `json:"default"`
	Options   []string `json:"options,omitempty"`
}
type Template struct {
	Parameters          []TemplateParameter `json:"parameters,omitempty"`
	AuthenticationModes []string            `json:"authenticationModes,omitempty"`
	TemplateRef
	Name           string `json:"name"`
	Service        string `json:"service"`
	Endpoint       string `json:"endpoint"`
	Authentication string `json:"authentication"`
	Guidance       string `json:"guidance"`
	Source         string `json:"source"`
	ReviewedAt     string `json:"reviewedAt"`
	Validation     string `json:"validation"`
}

// Published versions are immutable; future revisions must use a new version.
func Templates() []Template {
	return []Template{
		legacyTemplate(TemplateRef{"deepwiki", 1}, "DeepWiki", "generic", "https://mcp.deepwiki.com/mcp", "none", "Public repository documentation. Review discovered tools before granting agent access.", "https://docs.devin.ai/work-with-devin/deepwiki-mcp", "2026-10-17", "Previously exercised in the installed gateway; test this connection before use."),
		legacyTemplate(TemplateRef{"github", 1}, "GitHub read-only", "github", "https://api.githubcopilot.com/mcp/readonly", "bearer", "Use a PAT restricted to selected repositories and required read permissions. OAuth is not available yet.", "https://github.com/github/github-mcp-server/blob/main/docs/remote-server.md", "2026-10-17", "Documented configuration; live vendor acceptance pending."),
		legacyTemplate(TemplateRef{"context7", 1}, "Context7", "generic", "https://mcp.context7.com/mcp", "bearer", "Provide a Context7 API key. Discover library documentation tools and explicitly grant access.", "https://context7.com/docs/resources/all-clients", "2026-10-17", "Documented configuration; live vendor acceptance pending."),
		legacyTemplate(TemplateRef{"hugging-face", 1}, "Hugging Face", "generic", "https://huggingface.co/mcp", "optional-bearer", "Optional Hugging Face token. Review selected Space tools and their data/compute access separately.", "https://github.com/huggingface/hf-mcp-server", "2026-10-17", "Documented configuration; live vendor acceptance pending."),
		legacyTemplate(TemplateRef{"wandb", 1}, "W&B", "generic", "https://mcp.withwandb.com/mcp", "bearer", "Use a W&B API key with appropriate project access; grant read tools first.", "https://github.com/wandb/wandb-mcp-server", "2026-10-17", "Documented configuration; live vendor acceptance pending."),
		{TemplateRef: TemplateRef{"github", 2}, Name: "GitHub read-only", Service: "github", Endpoint: "https://api.githubcopilot.com/mcp/readonly", Authentication: "bearer or OAuth", AuthenticationModes: []string{"bearer", "oauth"}, Parameters: []TemplateParameter{{Name: "toolset", Label: "Read-only toolset", Default: "default", Options: []string{"default", "repos", "issues", "pull_requests", "actions"}}}, Guidance: "Select a read-only toolset. A PAT must restrict repositories and permissions; OAuth requires a registered client. Endpoint defaults do not restrict which repositories a credential can access. Agent grants remain explicit.", Source: "https://github.com/github/github-mcp-server/blob/main/docs/remote-server.md", ReviewedAt: "2026-10-18", Validation: "Protocol fixtures cover configuration; live acceptance of each toolset remains pending."},
		{TemplateRef: TemplateRef{"gitlab", 1}, Name: "GitLab instance", Service: "gitlab", Endpoint: "https://gitlab.com/api/v4/mcp", Authentication: "OAuth", AuthenticationModes: []string{"oauth"}, Parameters: []TemplateParameter{{Name: "instance", Label: "GitLab instance URL", Default: "https://gitlab.com"}}, Guidance: "Choose your instance. Use the mcp scope and a client registered for that instance, or review advertised DCR support. Availability depends on instance version, licensing and administrator policy.", Source: "https://docs.gitlab.com/user/gitlab_duo/model_context_protocol/mcp_server/", ReviewedAt: "2026-10-18", Validation: "Configuration and OAuth fixtures only; live instance qualification pending."},
		{TemplateRef: TemplateRef{"sentry", 1}, Name: "Sentry", Service: "sentry", Endpoint: "https://mcp.sentry.dev/mcp", Authentication: "OAuth", AuthenticationModes: []string{"oauth"}, Guidance: "Use the advertised MCP issuer with PKCE. Review automatic DCR/CIMD setup or use a compatible registered client. Start with org:read; broader scopes and every agent tool grant require separate review. Live account acceptance remains pending.", Source: "https://mcp.sentry.dev/", ReviewedAt: "2026-10-18", Validation: "Public metadata reviewed; configuration fixtures only. Live consent and tool qualification pending."},
		{TemplateRef: TemplateRef{"databricks", 1}, Name: "Databricks workspace service", Service: "databricks", Endpoint: "https://workspace.example/ai-gateway/mcp-services/catalog.schema.service", Authentication: "bearer or OAuth", AuthenticationModes: []string{"bearer", "oauth"}, Parameters: []TemplateParameter{{Name: "workspace", Label: "Databricks workspace URL", Default: "https://workspace.example", InputType: "url"}, {Name: "service", Label: "MCP service name", Default: "catalog.schema.service", InputType: "text"}}, Guidance: "Replace the example workspace and catalog.schema.service name. Use a PAT for testing or a registered public OAuth client with ai-gateway and offline_access. Obtain the issuer from the selected MCP metadata; no universal issuer is assumed. Service-principal client_credentials renewal is not supported.", Source: "https://docs.databricks.com/aws/en/ai-gateway/coding-agent-codex", ReviewedAt: "2026-10-18", Validation: "Configuration fixtures only. Workspace metadata, consent, service permissions and downstream login require live qualification."},
	}
}
func legacyTemplate(ref TemplateRef, name, service, endpoint, auth, guidance, source, reviewed, validation string) Template {
	return Template{TemplateRef: ref, Name: name, Service: service, Endpoint: endpoint, Authentication: auth, Guidance: guidance, Source: source, ReviewedAt: reviewed, Validation: validation}
}

var databricksServiceName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,62}\.[A-Za-z_][A-Za-z0-9_-]{0,62}\.[A-Za-z_][A-Za-z0-9_-]{0,62}$`)

func templateEndpoint(t Template, parameters map[string]string) (string, error) {
	if len(parameters) > len(t.Parameters) {
		return "", fmt.Errorf("%w: unknown template parameter", ErrInvalid)
	}
	values := map[string]string{}
	for _, p := range t.Parameters {
		value, ok := parameters[p.Name]
		if !ok {
			value = p.Default
		}
		if len(value) > 2048 || (len(p.Options) > 0 && !slices.Contains(p.Options, value)) {
			return "", fmt.Errorf("%w: invalid template parameter %s", ErrInvalid, p.Name)
		}
		values[p.Name] = value
	}
	for name := range parameters {
		if _, ok := values[name]; !ok {
			return "", fmt.Errorf("%w: unknown template parameter", ErrInvalid)
		}
	}
	if t.ID == "github" && t.Version == 2 {
		if values["toolset"] == "default" {
			return t.Endpoint, nil
		}
		return "https://api.githubcopilot.com/mcp/x/" + values["toolset"] + "/readonly", nil
	}
	if t.ID == "databricks" {
		origin := values["workspace"]
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" || strings.Contains(origin, "#") {
			return "", fmt.Errorf("%w: Databricks workspace must be an HTTPS origin without credentials, query, fragment or path", ErrInvalid)
		}
		if !databricksServiceName.MatchString(values["service"]) {
			return "", fmt.Errorf("%w: use a three-part MCP service name with letters, numbers, underscores or hyphens", ErrInvalid)
		}
		return strings.TrimSuffix(origin, "/") + "/ai-gateway/mcp-services/" + values["service"], nil
	}
	if t.ID == "gitlab" {
		origin := values["instance"]
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" || strings.Contains(origin, "#") {
			return "", fmt.Errorf("%w: GitLab instance must be an HTTPS origin without credentials, query, fragment or path", ErrInvalid)
		}
		return strings.TrimSuffix(origin, "/") + "/api/v4/mcp", nil
	}
	return t.Endpoint, nil
}
func validateTemplate(in Input) error {
	if in.Template == nil {
		if len(in.TemplateParameters) > 0 {
			return fmt.Errorf("%w: template parameters require a pinned template", ErrInvalid)
		}
		return nil
	}
	for _, t := range Templates() {
		if *in.Template == t.TemplateRef {
			endpoint, err := templateEndpoint(t, in.TemplateParameters)
			if err != nil {
				return err
			}
			if in.Endpoint != endpoint || in.Service != t.Service {
				return fmt.Errorf("%w: template defaults changed; detach the template to customize", ErrInvalid)
			}
			mode := credentialKind(in.CredentialType)
			if in.OAuth != nil {
				mode = "oauth"
			}
			if len(t.AuthenticationModes) > 0 && !slices.Contains(t.AuthenticationModes, mode) {
				return fmt.Errorf("%w: template authentication mode is unsupported", ErrInvalid)
			}
			return nil
		}
	}
	return fmt.Errorf("%w: unknown MCP template version", ErrInvalid)
}
