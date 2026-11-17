// Package scm pushes task branches and opens pull requests through
// Weft's gateway, with credentials held by the server. Agents never run
// an SCM CLI and never see a token: pushes run in Weft's own Git process
// with the credential passed only in that process's environment, and pull
// requests are created over the provider's REST API.
package scm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

// SettingKey holds the SCM connections; tokens are sealed by the
// installation key.
const SettingKey = "scm/connections"

// DefaultBranchPrefix is required on every pushed branch unless a
// connection sets its own.
const DefaultBranchPrefix = "weft/"

// ErrPush is a push the remote or the policy refused.
var ErrPush = errors.New("push refused")

// Identity is what a credential can do on the connection's repository.
type Identity struct {
	User          string `json:"user"`
	Repository    string `json:"repository"`
	DefaultBranch string `json:"defaultBranch,omitempty"`
	CanPush       bool   `json:"canPush"`
}

// PullRequest is a provider pull request.
type PullRequest struct {
	Number  int    `json:"number"`
	URL     string `json:"url"`
	Head    string `json:"head"`
	Base    string `json:"base"`
	State   string `json:"state"`
	Created bool   `json:"created"` // false when an open one already existed
}

// PullRequestInput creates a pull request.
type PullRequestInput struct {
	Title, Body, Head, Base string
}

// Provider is one SCM vendor's API for a repository.
type Provider interface {
	// Identity reads the credential's user and repository access.
	Identity(ctx context.Context) (Identity, error)
	// FindPullRequest returns the open pull request for head, or nil.
	FindPullRequest(ctx context.Context, head, base string) (*PullRequest, error)
	CreatePullRequest(ctx context.Context, in PullRequestInput) (*PullRequest, error)
	Comment(ctx context.Context, number int, body string) error
	// GitAuthHeader is the HTTP header value Git sends to push.
	GitAuthHeader() string
	// RemoteURL is the Git remote (the connection's gitUrl or the kind's
	// default), without credentials.
	RemoteURL() string
}

// Factory builds a provider for a connection with its plain token.
type Factory func(c Connection, token string) (Provider, error)

var (
	factoriesMu sync.RWMutex
	factories   = map[string]Factory{}
)

// RegisterKind makes an SCM kind available.
func RegisterKind(kind string, f Factory) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	factories[kind] = f
}

func factory(kind string) (Factory, bool) {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	f, ok := factories[kind]
	return f, ok
}

// Connection is a stored SCM connection. Token is sealed, never plain text.
type Connection struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Repository string `json:"repository"`
	// APIURL is the REST base; GitURL the HTTPS (or file) remote. Both
	// default per kind; neither may contain credentials.
	APIURL       string `json:"apiUrl,omitempty"`
	GitURL       string `json:"gitUrl,omitempty"`
	BranchPrefix string `json:"branchPrefix,omitempty"`
	// RequirePullRequestApproval: opening a pull request waits for a person
	// unless an administrator explicitly relaxes it (nil means required).
	RequirePullRequestApproval *bool      `json:"requirePullRequestApproval,omitempty"`
	Token                      string     `json:"token,omitempty"`
	CreatedAt                  time.Time  `json:"createdAt"`
	UpdatedAt                  time.Time  `json:"updatedAt"`
	UpdatedBy                  string     `json:"updatedBy,omitempty"`
	TokenUpdatedAt             *time.Time `json:"tokenUpdatedAt,omitempty"`
}

// Prefix is the mandatory branch prefix.
func (c Connection) Prefix() string {
	if c.BranchPrefix == "" {
		return DefaultBranchPrefix
	}
	return c.BranchPrefix
}

// ApprovalRequired reports whether pull requests need a person.
func (c Connection) ApprovalRequired() bool {
	return c.RequirePullRequestApproval == nil || *c.RequirePullRequestApproval
}

// View is a connection without its credential.
type View struct {
	Connection
	TokenConfigured bool `json:"tokenConfigured"`
}

// Input creates or updates a connection. Token nil keeps the stored one.
type Input struct {
	Name                       string  `json:"name"`
	Kind                       string  `json:"kind"`
	Repository                 string  `json:"repository"`
	APIURL                     string  `json:"apiUrl,omitempty"`
	GitURL                     string  `json:"gitUrl,omitempty"`
	BranchPrefix               string  `json:"branchPrefix,omitempty"`
	RequirePullRequestApproval *bool   `json:"requirePullRequestApproval,omitempty"`
	Token                      *string `json:"token,omitempty"`
}

// Issue is a field-level validation problem.
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidationError lists problems with an input.
type ValidationError struct {
	Errors []Issue `json:"errors"`
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Errors))
	for _, i := range e.Errors {
		parts = append(parts, i.Path+": "+i.Message)
	}
	return "invalid SCM connection: " + strings.Join(parts, "; ")
}

type document struct {
	Connections []Connection `json:"connections"`
}

// Manager stores connections.
type Manager struct {
	Store storage.Store
	Box   *secrets.Box
	Bus   *events.Bus // optional
}

func tokenAAD(name string) string { return SettingKey + "/" + name + "/token" }

func (m Manager) read(ctx context.Context) (document, int64, error) {
	st, err := m.Store.GetSetting(ctx, SettingKey)
	if errors.Is(err, storage.ErrNotFound) {
		return document{}, 0, nil
	}
	if err != nil {
		return document{}, 0, fmt.Errorf("scm: read connections: %w", err)
	}
	var d document
	if err := json.Unmarshal(st.Value, &d); err != nil {
		return document{}, 0, errors.New("scm: stored connections are corrupt")
	}
	return d, st.Revision, nil
}

func view(c Connection) View {
	v := View{Connection: c, TokenConfigured: c.Token != ""}
	v.Token = ""
	return v
}

// List returns every connection without credentials.
func (m Manager) List(ctx context.Context) ([]View, error) {
	d, _, err := m.read(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(d.Connections))
	for _, c := range d.Connections {
		out = append(out, view(c))
	}
	return out, nil
}

// Get returns one connection (with its sealed token) by name.
func (m Manager) Get(ctx context.Context, name string) (Connection, error) {
	d, _, err := m.read(ctx)
	if err != nil {
		return Connection{}, err
	}
	for _, c := range d.Connections {
		if c.Name == name {
			return c, nil
		}
	}
	return Connection{}, fmt.Errorf("%w: SCM connection %q", storage.ErrNotFound, name)
}

var (
	nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,61}[a-z0-9])?$`)
	repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+){1,2}$`)
	// A prefix is one or more path segments ending in "/".
	prefixRe = regexp.MustCompile(`^([A-Za-z0-9._-]+/)+$`)
)

func checkURL(raw string, schemes ...string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "is not a URL"
	}
	if u.User != nil {
		return "must not contain credentials; use the token field"
	}
	for _, s := range schemes {
		if u.Scheme == s && (u.Host != "" || s == "file") {
			return ""
		}
	}
	return "must use " + strings.Join(schemes, " or ")
}

func (m Manager) validate(in *Input, d document, update bool) *ValidationError {
	ve := &ValidationError{}
	add := func(path, f string, a ...any) { ve.Errors = append(ve.Errors, Issue{path, fmt.Sprintf(f, a...)}) }
	in.Name, in.Kind, in.Repository = strings.TrimSpace(in.Name), strings.TrimSpace(in.Kind), strings.Trim(strings.TrimSpace(in.Repository), "/")
	if !update {
		if !nameRe.MatchString(in.Name) {
			add("name", "use 1-63 lowercase letters, digits, '.', '_' or '-'")
		}
		for _, c := range d.Connections {
			if c.Name == in.Name {
				add("name", "a connection named %q already exists", in.Name)
			}
		}
	}
	if _, ok := factory(in.Kind); !ok {
		add("kind", "unsupported SCM kind %q", in.Kind)
	}
	if !repoRe.MatchString(in.Repository) {
		add("repository", "use owner/name (GitHub) or project/name (Bitbucket)")
	}
	if msg := checkURL(in.APIURL, "https", "http"); msg != "" {
		add("apiUrl", "%s", msg)
	}
	if msg := checkURL(in.GitURL, "https", "http", "file"); msg != "" {
		add("gitUrl", "%s", msg)
	}
	if in.BranchPrefix != "" && !prefixRe.MatchString(in.BranchPrefix) {
		add("branchPrefix", `use path segments ending in "/", e.g. "weft/"`)
	}
	if in.Token != nil && (strings.TrimSpace(*in.Token) == "" || len(*in.Token) > 4096 || strings.ContainsAny(*in.Token, "\r\n")) {
		add("token", "enter a single-line token, or omit the field to keep the stored one")
	}
	if len(ve.Errors) > 0 {
		return ve
	}
	return nil
}

func (m Manager) mutate(ctx context.Context, actor string, fn func(d *document) error) error {
	for i := 0; i < 5; i++ {
		d, rev, err := m.read(ctx)
		if err != nil {
			return err
		}
		if err := fn(&d); err != nil {
			return err
		}
		raw, _ := json.Marshal(d)
		_, err = m.Store.PutSetting(ctx, SettingKey, raw, rev, actor)
		if errors.Is(err, storage.ErrConflict) {
			continue
		}
		return err
	}
	return fmt.Errorf("%w: SCM connections are being changed concurrently; try again", storage.ErrConflict)
}

func (m Manager) apply(c *Connection, in Input, actor string, now time.Time) error {
	c.Kind, c.Repository, c.APIURL, c.GitURL, c.BranchPrefix = in.Kind, in.Repository, in.APIURL, in.GitURL, in.BranchPrefix
	c.RequirePullRequestApproval, c.UpdatedAt, c.UpdatedBy = in.RequirePullRequestApproval, now, actor
	if in.Token == nil {
		return nil
	}
	sealed, err := m.Box.Seal([]byte(strings.TrimSpace(*in.Token)), tokenAAD(c.Name))
	if errors.Is(err, secrets.ErrNoKey) {
		return fmt.Errorf("%w: set WEFT_SECRET_KEY (or secrets.keyFile) so SCM tokens can be encrypted", secrets.ErrNoKey)
	}
	if err != nil {
		return err
	}
	c.Token, c.TokenUpdatedAt = sealed, &now
	return nil
}

// Save creates (update=false) or updates a connection.
func (m Manager) Save(ctx context.Context, in Input, actor string, update bool) (View, error) {
	var out Connection
	err := m.mutate(ctx, actor, func(d *document) error {
		if ve := m.validate(&in, *d, update); ve != nil {
			return ve
		}
		now := time.Now().UTC()
		if !update {
			if in.Token == nil {
				return &ValidationError{Errors: []Issue{{"token", "a token is required"}}}
			}
			c := Connection{Name: in.Name, CreatedAt: now}
			if err := m.apply(&c, in, actor, now); err != nil {
				return err
			}
			d.Connections = append(d.Connections, c)
			out = c
			return nil
		}
		for i := range d.Connections {
			if d.Connections[i].Name == in.Name {
				if err := m.apply(&d.Connections[i], in, actor, now); err != nil {
					return err
				}
				out = d.Connections[i]
				return nil
			}
		}
		return fmt.Errorf("%w: SCM connection %q", storage.ErrNotFound, in.Name)
	})
	if err != nil {
		return View{}, err
	}
	m.audit(ctx, "saved", out.Name, actor)
	return view(out), nil
}

// Delete removes a connection.
func (m Manager) Delete(ctx context.Context, name, actor string) error {
	err := m.mutate(ctx, actor, func(d *document) error {
		for i, c := range d.Connections {
			if c.Name == name {
				d.Connections = append(d.Connections[:i], d.Connections[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("%w: SCM connection %q", storage.ErrNotFound, name)
	})
	if err == nil {
		m.audit(ctx, "deleted", name, actor)
	}
	return err
}

func (m Manager) audit(ctx context.Context, action, name, actor string) {
	if m.Bus != nil {
		m.Bus.Publish(ctx, domain.NewEvent(domain.EvSCMChanged, "", map[string]any{"action": action, "connection": name}).WithActor(actor))
	}
}

// Open builds the provider for a connection, decrypting its token in memory.
func (m Manager) Open(c Connection) (Provider, string, error) {
	f, ok := factory(c.Kind)
	if !ok {
		return nil, "", fmt.Errorf("scm: unsupported kind %q", c.Kind)
	}
	if c.Token == "" {
		return nil, "", fmt.Errorf("scm: connection %q has no token", c.Name)
	}
	tok, err := m.Box.Open(c.Token, tokenAAD(c.Name))
	if err != nil {
		return nil, "", fmt.Errorf("scm: the token of %q cannot be decrypted; re-enter it", c.Name)
	}
	p, err := f(c, string(tok))
	return p, string(tok), err
}

// Test reads the token's identity and repository access.
func (m Manager) Test(ctx context.Context, name string) (Identity, error) {
	c, err := m.Get(ctx, name)
	if err != nil {
		return Identity{}, err
	}
	p, tok, err := m.Open(c)
	if err != nil {
		return Identity{}, err
	}
	id, err := p.Identity(ctx)
	if err != nil {
		return Identity{}, errors.New(secrets.Redact(err.Error(), tok))
	}
	return id, nil
}

// BasicAuth is the Basic credential header value for user:token.
func BasicAuth(user, token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+token))
}

var refRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// validBranch rejects names Git or the policy would misread.
func validBranch(b string) bool {
	return refRe.MatchString(b) && !strings.HasPrefix(b, "-") && !strings.Contains(b, "..") &&
		!strings.HasSuffix(b, "/") && !strings.HasSuffix(b, ".lock") && !strings.Contains(b, "//")
}

// Push pushes branch from workdir to the connection's remote. It refuses
// branches outside the prefix and the base branch, never forces, and never
// pushes tags or other refs. The credential travels only as an HTTP header
// in the environment of this Git process.
func Push(ctx context.Context, c Connection, p Provider, token, workdir, branch, base string) (string, error) {
	if !validBranch(branch) {
		return "", fmt.Errorf("%w: %q is not a valid branch name", ErrPush, branch)
	}
	if !strings.HasPrefix(branch, c.Prefix()) {
		return "", fmt.Errorf("%w: only branches starting with %q may be pushed", ErrPush, c.Prefix())
	}
	if base != "" && branch == base {
		return "", fmt.Errorf("%w: the base branch %q is never pushed", ErrPush, base)
	}
	remote := p.RemoteURL()
	if remote == "" {
		return "", fmt.Errorf("%w: the connection has no Git URL", ErrPush)
	}
	ref := "refs/heads/" + branch
	cmd := exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "credential.helper=",
		"push", "--porcelain", "--no-verify", remote, ref+":"+ref)
	cmd.Dir = workdir
	cmd.Env = append(gitEnv(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: "+p.GitAuthHeader())
	out, err := cmd.CombinedOutput()
	text := secrets.Redact(strings.TrimSpace(string(out)), token, p.GitAuthHeader())
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrPush, text)
	}
	return text, nil
}

// gitEnv is the environment of Weft's own Git process: no inherited
// secrets, no user or system configuration (credential helpers), no prompts.
func gitEnv() []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.TempDir(), "LANG=C",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
}

func redact(err error, token string) string { return secrets.Redact(err.Error(), token) }

// event publishes a task-scoped SCM event for the calling execution.
func (m Manager) event(ctx context.Context, p *tools.Principal, typ domain.EventType, data map[string]any) {
	if m.Bus == nil || p == nil {
		return
	}
	m.Bus.Publish(context.WithoutCancel(ctx), domain.NewEvent(typ, p.GraphID, data).WithNode(p.NodeID, p.Replica).WithExecution(p.ExecutionID))
}
