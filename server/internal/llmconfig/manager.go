// Package llmconfig lets operators manage model providers from the console.
//
// Two kinds of providers coexist:
//
//   - static providers come from the platform config file or well-known
//     environment variables. They are read-only in the console; their
//     credentials stay wherever the deployment put them.
//   - managed providers are created, edited, tested and removed through the
//     API. They live in one installation setting ("llm/providers"), with
//     every credential sealed by the installation key (package secrets).
//
// The Manager owns that setting and keeps the shared llm.Registry in sync
// with it: every write rebuilds the registry atomically, and Refresh picks up
// writes made by other instances. Agents never see a credential: native
// turns and external runtimes alike reach providers only through the
// registry (the LLM gateway), which holds the decrypted key in memory.
package llmconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
)

// SettingKey is the installation setting holding managed providers.
const SettingKey = "llm/providers"

// Source says where a provider is defined.
type Source string

const (
	SourceConfig  Source = "config"
	SourceManaged Source = "managed"
)

// managedProvider is the stored form of a console-managed provider. APIKey
// is a sealed value (secrets.Box), never plain text.
type managedProvider struct {
	Name                string     `json:"name"`
	Type                string     `json:"type"`
	BaseURL             string     `json:"baseUrl,omitempty"`
	DefaultModel        string     `json:"defaultModel,omitempty"`
	TimeoutSec          int        `json:"timeoutSec,omitempty"`
	MaxRetries          int        `json:"maxRetries,omitempty"`
	Concurrency         int        `json:"concurrency,omitempty"`
	ContextWindow       int        `json:"contextWindow,omitempty"`
	ModelGuidance       string     `json:"modelGuidance,omitempty"`
	APIKey              string     `json:"apiKey,omitempty"`
	CredentialUpdatedAt *time.Time `json:"credentialUpdatedAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
	UpdatedBy           string     `json:"updatedBy,omitempty"`
}

type document struct {
	Providers []managedProvider `json:"providers"`
	// Default overrides the config default when set.
	Default string `json:"default,omitempty"`
	// Plugins records which provider plugins an administrator enabled.
	// Absent means disabled.
	Plugins map[string]pluginState `json:"plugins,omitempty"`
}

type pluginState struct {
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
}

func (d *document) find(name string) int {
	for i := range d.Providers {
		if d.Providers[i].Name == name {
			return i
		}
	}
	return -1
}

func (d document) clone() document {
	out := document{Default: d.Default, Providers: make([]managedProvider, len(d.Providers))}
	copy(out.Providers, d.Providers)
	if d.Plugins != nil {
		out.Plugins = make(map[string]pluginState, len(d.Plugins))
		for k, v := range d.Plugins {
			out.Plugins[k] = v
		}
	}
	return out
}

// CredentialUpdate changes a stored credential. Omit it (nil) to keep the
// current one; set Value to replace it; set Clear to remove it.
type CredentialUpdate struct {
	Value string `json:"value,omitempty"`
	Clear bool   `json:"clear,omitempty"`
}

// ProviderInput creates or updates a managed provider.
type ProviderInput struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	BaseURL      string `json:"baseUrl,omitempty"`
	DefaultModel string `json:"defaultModel,omitempty"`
	TimeoutSec   int    `json:"timeoutSec,omitempty"`
	MaxRetries   int    `json:"maxRetries,omitempty"`
	Concurrency  int    `json:"concurrency,omitempty"`
	// ContextWindow in tokens (0 = the type's default; Ollama: 32768).
	ContextWindow int `json:"contextWindow,omitempty"`
	// ModelGuidance: "" (standard) or "small" (compact tool schemas,
	// argument examples and one tool call per round for small models).
	ModelGuidance string            `json:"modelGuidance,omitempty"`
	APIKey        *CredentialUpdate `json:"apiKey,omitempty"`
}

// CredentialState describes a credential without revealing it.
type CredentialState struct {
	Configured bool `json:"configured"`
	// Source: stored (console, encrypted) | env | file | none.
	Source string `json:"source"`
	// Env is the environment variable name for config providers.
	Env       string     `json:"env,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	// Error explains a configured credential that cannot be used.
	Error string `json:"error,omitempty"`
}

// ProviderView is the public description of a provider. It extends
// llm.ProviderInfo (same field names) and never contains a secret.
type ProviderView struct {
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	BaseURL       string          `json:"baseUrl,omitempty"`
	DefaultModel  string          `json:"defaultModel,omitempty"`
	Default       bool            `json:"default"`
	TimeoutSec    int             `json:"timeoutSec,omitempty"`
	MaxRetries    int             `json:"maxRetries,omitempty"`
	Concurrency   int             `json:"concurrency,omitempty"`
	ContextWindow int             `json:"contextWindow,omitempty"`
	ModelGuidance string          `json:"modelGuidance,omitempty"`
	Source        Source          `json:"source"`
	Editable      bool            `json:"editable"`
	Credential    CredentialState `json:"credential"`
	// Status: ready | unavailable (Error says why).
	Status    string      `json:"status"`
	Error     string      `json:"error,omitempty"`
	LastTest  *TestResult `json:"lastTest,omitempty"`
	CreatedAt *time.Time  `json:"createdAt,omitempty"`
	UpdatedAt *time.Time  `json:"updatedAt,omitempty"`
	UpdatedBy string      `json:"updatedBy,omitempty"`
}

// TypeInfo helps the console build the provider form.
type TypeInfo struct {
	Type           string `json:"type"`
	Label          string `json:"label"`
	DefaultBaseURL string `json:"defaultBaseUrl,omitempty"`
	// KeyRequired: the type cannot work without an API key (with its
	// default base URL).
	KeyRequired bool `json:"keyRequired"`
}

// Meta describes the provider settings as a whole.
type Meta struct {
	Revision      int64      `json:"revision"`
	Default       string     `json:"default"`
	ConfigDefault string     `json:"configDefault,omitempty"`
	Secrets       SecretInfo `json:"secrets"`
	Types         []TypeInfo `json:"types"`
}

// SecretInfo reports whether credentials can be stored (never the key).
type SecretInfo struct {
	Enabled   bool   `json:"enabled"`
	KeySource string `json:"keySource"`
	KeyID     string `json:"keyId,omitempty"`
}

// Issue is one field-level validation problem.
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidationError lists every problem with a provider input.
type ValidationError struct {
	Errors []Issue `json:"errors"`
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Errors))
	for _, i := range e.Errors {
		parts = append(parts, i.Path+": "+i.Message)
	}
	return "invalid provider: " + strings.Join(parts, "; ")
}

// Options wire a Manager.
type Options struct {
	Store storage.Store
	// Box seals credentials; nil means credentials cannot be stored.
	Box       *secrets.Box
	KeySource secrets.KeySource
	Registry  *llm.Registry
	// Static are the providers from config (read-only in the console).
	Static        []llm.ProviderConfig
	StaticDefault string
	Bus           *events.Bus // optional
	Log           *slog.Logger
	// InUse lists where a provider is referenced explicitly (graph/node),
	// so deleting it can be refused. Optional.
	InUse func(ctx context.Context, provider string) ([]string, error)
	// TestTimeout bounds a connection test (default 180s: two requests, and local models may need to load first).
	TestTimeout time.Duration
}

// Manager keeps managed providers and the registry in sync.
type Manager struct {
	opt Options

	mu       sync.Mutex
	revision int64
	doc      document
	tests    map[string]*TestResult
}

// New builds a Manager. Call Load before serving.
func New(opt Options) (*Manager, error) {
	if opt.Store == nil || opt.Registry == nil {
		return nil, errors.New("llmconfig: store and registry are required")
	}
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	if opt.TestTimeout <= 0 {
		opt.TestTimeout = 180 * time.Second
	}
	return &Manager{opt: opt, tests: map[string]*TestResult{}}, nil
}

var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,61}[a-z0-9])?$`)

func credentialAAD(name string) string { return SettingKey + "/" + name + "/apiKey" }

// read returns the stored document and its revision (0 when absent).
func (m *Manager) read(ctx context.Context) (document, int64, error) {
	st, err := m.opt.Store.GetSetting(ctx, SettingKey)
	if errors.Is(err, storage.ErrNotFound) {
		return document{}, 0, nil
	}
	if err != nil {
		return document{}, 0, fmt.Errorf("llmconfig: read providers: %w", err)
	}
	var d document
	if err := json.Unmarshal(st.Value, &d); err != nil {
		return document{}, 0, fmt.Errorf("llmconfig: stored providers are corrupt: %w", err)
	}
	return d, st.Revision, nil
}

// Load reads the stored providers, re-seals credentials still sealed with a
// previous installation key, and applies everything to the registry.
func (m *Manager) Load(ctx context.Context) error {
	d, rev, err := m.read(ctx)
	if err != nil {
		return err
	}
	if n, err := m.reseal(ctx, d, rev); err != nil {
		m.opt.Log.Error("re-sealing provider credentials with the current key failed; they stay readable with the previous key", "err", err)
	} else if n > 0 {
		m.opt.Log.Info("provider credentials re-sealed with the current installation key", "count", n, "key_id", m.opt.Box.PrimaryKeyID())
		if d, rev, err = m.read(ctx); err != nil {
			return err
		}
	}
	m.apply(d, rev)
	return nil
}

// reseal rewrites credentials sealed with a previous key. It returns how
// many were rewritten.
func (m *Manager) reseal(ctx context.Context, d document, rev int64) (int, error) {
	box := m.opt.Box
	if !box.Enabled() || rev == 0 {
		return 0, nil
	}
	next := d.clone()
	n := 0
	for i := range next.Providers {
		p := &next.Providers[i]
		if p.APIKey == "" || !box.NeedsReseal(p.APIKey) {
			continue
		}
		pt, err := box.Open(p.APIKey, credentialAAD(p.Name))
		if err != nil {
			continue // reported as unavailable by apply
		}
		if p.APIKey, err = box.Seal(pt, credentialAAD(p.Name)); err != nil {
			return 0, err
		}
		n++
	}
	if n == 0 {
		return 0, nil
	}
	b, err := json.Marshal(next)
	if err != nil {
		return 0, err
	}
	if _, err := m.opt.Store.PutSetting(ctx, SettingKey, b, rev, "system:key-rotation"); err != nil {
		return 0, err
	}
	return n, nil
}

// Refresh re-applies the stored providers if another instance changed them.
func (m *Manager) Refresh(ctx context.Context) error {
	st, err := m.opt.Store.GetSetting(ctx, SettingKey)
	var rev int64
	switch {
	case errors.Is(err, storage.ErrNotFound):
	case err != nil:
		return fmt.Errorf("llmconfig: refresh: %w", err)
	default:
		rev = st.Revision
	}
	m.mu.Lock()
	same := rev == m.revision
	m.mu.Unlock()
	if same {
		return nil
	}
	d, rev, err := m.read(ctx)
	if err != nil {
		return err
	}
	m.apply(d, rev)
	return nil
}

// build turns a stored provider into a config and a provider. A provider
// that cannot be built becomes llm.Unavailable with the reason.
func (m *Manager) build(p managedProvider) (llm.ProviderConfig, llm.Provider) {
	cfg := llm.ProviderConfig{
		Name: p.Name, Type: p.Type, BaseURL: p.BaseURL, DefaultModel: p.DefaultModel,
		TimeoutSec: p.TimeoutSec, MaxRetries: p.MaxRetries, Concurrency: p.Concurrency, ContextWindow: p.ContextWindow,
		ModelGuidance: p.ModelGuidance,
	}
	if p.APIKey != "" {
		key, err := m.opt.Box.Open(p.APIKey, credentialAAD(p.Name))
		if err != nil {
			return cfg, llm.Unavailable(p.Name, credentialError(err))
		}
		cfg.APIKeyValue = string(key)
	}
	prov, err := llm.New(cfg)
	if err != nil {
		return cfg, llm.Unavailable(p.Name, errors.New(secrets.Redact(err.Error(), cfg.APIKeyValue)))
	}
	return cfg, prov
}

func credentialError(err error) error {
	switch {
	case errors.Is(err, secrets.ErrNoKey):
		return errors.New("the stored credential cannot be used because no installation encryption key is configured (set WEFT_SECRET_KEY or secrets.keyFile)")
	case errors.Is(err, secrets.ErrUnknownKey):
		return errors.New("the stored credential was encrypted with a key this installation no longer has; re-enter the credential or configure the previous key in WEFT_SECRET_KEY_PREVIOUS")
	default:
		return errors.New("the stored credential is corrupt; re-enter it")
	}
}

// apply rebuilds the registry from static + managed providers.
func (m *Manager) apply(d document, rev int64) {
	entries := make([]llm.Entry, 0, len(m.opt.Static)+len(d.Providers))
	for _, c := range m.opt.Static {
		if c.Name == "" {
			c.Name = c.Type
		}
		p, err := llm.New(c)
		if err != nil {
			p = llm.Unavailable(c.Name, err)
		}
		entries = append(entries, llm.Entry{Config: c, Provider: p})
	}
	for _, mp := range d.Providers {
		cfg, p := m.build(mp)
		if !m.pluginEnabled(d, mp.Type) {
			p = llm.Unavailable(mp.Name, pluginDisabledError(mp.Type))
		}
		entries = append(entries, llm.Entry{Config: cfg, Provider: p})
	}
	def := d.Default
	if def == "" {
		def = m.opt.StaticDefault
	}
	if err := m.opt.Registry.ReplaceProviders(entries, def); err != nil {
		// Only possible with a corrupt document (duplicate names, dangling
		// default): keep serving the current set and say so loudly.
		m.opt.Log.Error("provider settings could not be applied; the previous provider set stays active", "revision", rev, "err", err)
		return
	}
	m.mu.Lock()
	m.doc, m.revision = d, rev
	m.mu.Unlock()
}

func (m *Manager) isStatic(name string) bool {
	for _, c := range m.opt.Static {
		n := c.Name
		if n == "" {
			n = c.Type
		}
		if n == name {
			return true
		}
	}
	return false
}

// List describes every provider in registry order.
func (m *Manager) List() []ProviderView {
	m.mu.Lock()
	doc := m.doc
	tests := make(map[string]*TestResult, len(m.tests))
	for k, v := range m.tests {
		tests[k] = v
	}
	m.mu.Unlock()
	managed := map[string]managedProvider{}
	for _, p := range doc.Providers {
		managed[p.Name] = p
	}
	static := map[string]llm.ProviderConfig{}
	for _, c := range m.opt.Static {
		if c.Name == "" {
			c.Name = c.Type
		}
		static[c.Name] = c
	}
	infos := m.opt.Registry.List()
	out := make([]ProviderView, 0, len(infos))
	for _, info := range infos {
		v := ProviderView{
			Name: info.Name, Type: info.Type, BaseURL: info.BaseURL, DefaultModel: info.DefaultModel,
			Default: info.Default, Status: "ready", LastTest: tests[info.Name],
		}
		if p, ok := m.opt.Registry.Get(info.Name); ok {
			if err := llm.UnavailableReason(p); err != nil {
				v.Status, v.Error = "unavailable", err.Error()
			}
		}
		if mp, ok := managed[info.Name]; ok {
			v.Source, v.Editable = SourceManaged, true
			v.TimeoutSec, v.MaxRetries, v.Concurrency, v.ContextWindow, v.ModelGuidance = mp.TimeoutSec, mp.MaxRetries, mp.Concurrency, mp.ContextWindow, mp.ModelGuidance
			created, updated := mp.CreatedAt, mp.UpdatedAt
			v.CreatedAt, v.UpdatedAt, v.UpdatedBy = &created, &updated, mp.UpdatedBy
			v.Credential = CredentialState{Source: "none"}
			if mp.APIKey != "" {
				v.Credential = CredentialState{Configured: true, Source: "stored", UpdatedAt: mp.CredentialUpdatedAt}
				if _, err := m.opt.Box.Open(mp.APIKey, credentialAAD(mp.Name)); err != nil {
					v.Credential.Error = credentialError(err).Error()
				}
			}
		} else {
			c := static[info.Name]
			v.Source = SourceConfig
			v.TimeoutSec, v.MaxRetries, v.Concurrency, v.ContextWindow, v.ModelGuidance = c.TimeoutSec, c.MaxRetries, c.Concurrency, c.ContextWindow, c.ModelGuidance
			v.Credential = staticCredential(c)
		}
		out = append(out, v)
	}
	return out
}

func staticCredential(c llm.ProviderConfig) CredentialState {
	switch {
	case c.APIKeyEnv != "":
		return CredentialState{Configured: strings.TrimSpace(os.Getenv(c.APIKeyEnv)) != "", Source: "env", Env: c.APIKeyEnv}
	case c.APIKeyFile != "":
		b, err := os.ReadFile(c.APIKeyFile)
		st := CredentialState{Source: "file", Configured: err == nil && strings.TrimSpace(string(b)) != ""}
		if err != nil {
			st.Error = "the configured key file cannot be read"
		}
		return st
	}
	return CredentialState{Source: "none"}
}

// Get returns one provider view.
func (m *Manager) Get(name string) (*ProviderView, error) {
	for _, v := range m.List() {
		if v.Name == name {
			return &v, nil
		}
	}
	return nil, fmt.Errorf("%w: provider %q", storage.ErrNotFound, name)
}

// Meta describes the settings document.
func (m *Manager) Meta() Meta {
	m.mu.Lock()
	rev := m.revision
	m.mu.Unlock()
	return Meta{
		Revision: rev, Default: m.opt.Registry.DefaultName(), ConfigDefault: m.opt.StaticDefault,
		Secrets: SecretInfo{Enabled: m.opt.Box.Enabled(), KeySource: string(m.opt.KeySource), KeyID: m.opt.Box.PrimaryKeyID()},
		Types:   Types(),
	}
}

// Types lists the provider types this build supports, from the plugin
// manifests.
func Types() []TypeInfo {
	var out []TypeInfo
	for _, m := range llm.Plugins() {
		out = append(out, TypeInfo{Type: m.ID, Label: m.Label, DefaultBaseURL: m.DefaultBaseURL, KeyRequired: m.Key == llm.KeyRequired})
	}
	return out
}

// mutate applies fn to the latest document and stores the result with a
// compare-and-set. With ifRevision > 0 the write fails with ErrConflict
// unless the document is still at that revision (the caller edited a view
// that is now stale); with 0 a concurrent write is retried on the fresh
// document, which is safe because fn re-applies one operation.
func (m *Manager) mutate(ctx context.Context, ifRevision int64, actor string, fn func(d *document) error) (document, int64, error) {
	for attempt := 0; attempt < 3; attempt++ {
		cur, rev, err := m.read(ctx)
		if err != nil {
			return document{}, 0, err
		}
		if ifRevision > 0 && rev != ifRevision {
			return document{}, 0, fmt.Errorf("%w: provider settings changed (revision %d, you edited %d); reload and try again", storage.ErrConflict, rev, ifRevision)
		}
		next := cur.clone()
		if err := fn(&next); err != nil {
			return document{}, 0, err
		}
		b, err := json.Marshal(next)
		if err != nil {
			return document{}, 0, err
		}
		newRev, err := m.opt.Store.PutSetting(ctx, SettingKey, b, rev, actor)
		if errors.Is(err, storage.ErrConflict) && ifRevision == 0 {
			continue
		}
		if err != nil {
			return document{}, 0, err
		}
		m.apply(next, newRev)
		return next, newRev, nil
	}
	return document{}, 0, fmt.Errorf("%w: provider settings are being changed concurrently; try again", storage.ErrConflict)
}

// validate checks an input. existing is the stored provider when updating.
func (m *Manager) validate(in *ProviderInput, d *document, existing *managedProvider) *ValidationError {
	ve := &ValidationError{}
	add := func(path, f string, a ...any) { ve.Errors = append(ve.Errors, Issue{path, fmt.Sprintf(f, a...)}) }
	in.Name = strings.TrimSpace(in.Name)
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.DefaultModel = strings.TrimSpace(in.DefaultModel)
	if existing == nil {
		switch {
		case !nameRe.MatchString(in.Name):
			add("name", "use 1-63 lowercase letters, digits, '.', '_' or '-', starting and ending with a letter or digit")
		case in.Name == "default":
			add("name", `"default" is reserved (it always means the default provider)`)
		case m.isStatic(in.Name):
			add("name", "a provider named %q is defined in the server configuration", in.Name)
		case d.find(in.Name) >= 0:
			add("name", "a provider named %q already exists", in.Name)
		}
	}
	if _, ok := llm.PluginManifest(in.Type); !ok {
		add("type", "unsupported provider type %q", in.Type)
	} else if !m.pluginEnabled(*d, in.Type) {
		add("type", "%s", pluginDisabledError(in.Type).Error())
	}
	if in.BaseURL != "" {
		u, err := url.Parse(in.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			add("baseUrl", "must be an http:// or https:// URL")
		} else if u.User != nil {
			add("baseUrl", "must not contain credentials; use the API key field")
		}
	}
	if len(in.DefaultModel) > 200 {
		add("defaultModel", "is too long (max 200 characters)")
	}
	if in.TimeoutSec < 0 || in.TimeoutSec > 3600 {
		add("timeoutSec", "must be between 0 (default) and 3600")
	}
	if in.MaxRetries < -1 || in.MaxRetries > 10 {
		add("maxRetries", "must be between -1 (no retries) and 10")
	}
	if in.Concurrency < 0 || in.Concurrency > 1000 {
		add("concurrency", "must be between 0 (unlimited) and 1000")
	}
	if in.ContextWindow != 0 && (in.ContextWindow < 1024 || in.ContextWindow > 1<<20) {
		add("contextWindow", "must be 0 (default) or between 1024 and 1048576 tokens")
	}
	if in.ModelGuidance != "" && in.ModelGuidance != llm.GuidanceSmall {
		add("modelGuidance", `must be "" (standard) or "small"`)
	}
	if c := in.APIKey; c != nil {
		switch {
		case c.Clear && c.Value != "":
			add("apiKey", "either replace or clear the credential, not both")
		case !c.Clear && strings.TrimSpace(c.Value) == "":
			add("apiKey", "enter the credential, or omit the field to keep the current one")
		case len(c.Value) > 8192:
			add("apiKey", "is too long")
		case strings.ContainsAny(strings.TrimSpace(c.Value), "\r\n"):
			add("apiKey", "must be a single line")
		}
	}
	if len(ve.Errors) > 0 {
		return ve
	}
	return nil
}

// resolveKey computes the plain credential an input results in: the new
// value, nothing (cleared), or the existing stored one.
func (m *Manager) resolveKey(in *ProviderInput, existing *managedProvider) (string, error) {
	switch {
	case in.APIKey != nil && in.APIKey.Clear:
		return "", nil
	case in.APIKey != nil:
		return strings.TrimSpace(in.APIKey.Value), nil
	case existing != nil && existing.APIKey != "":
		k, err := m.opt.Box.Open(existing.APIKey, credentialAAD(existing.Name))
		if err != nil {
			return "", credentialError(err)
		}
		return string(k), nil
	}
	return "", nil
}

// checkBuild constructs the provider so configuration errors (a hosted type
// without a key, ...) are reported now, not on the first agent turn.
func checkBuild(in *ProviderInput, key string) *ValidationError {
	cfg := llm.ProviderConfig{Name: in.Name, Type: in.Type, BaseURL: in.BaseURL, DefaultModel: in.DefaultModel, APIKeyValue: key}
	if _, err := llm.New(cfg); err != nil {
		msg := secrets.Redact(err.Error(), key)
		msg = strings.Replace(msg, "(set apiKeyEnv or apiKeyFile)", "(enter it in the API key field)", 1)
		path := "type"
		if key == "" {
			path = "apiKey"
		}
		return &ValidationError{Errors: []Issue{{path, msg}}}
	}
	return nil
}

func (m *Manager) applyInput(p *managedProvider, in *ProviderInput, key string, actor string, now time.Time) error {
	p.Type, p.BaseURL, p.DefaultModel = in.Type, in.BaseURL, in.DefaultModel
	p.TimeoutSec, p.MaxRetries, p.Concurrency, p.ContextWindow, p.ModelGuidance = in.TimeoutSec, in.MaxRetries, in.Concurrency, in.ContextWindow, in.ModelGuidance
	p.UpdatedAt, p.UpdatedBy = now, actor
	if in.APIKey == nil {
		return nil
	}
	if in.APIKey.Clear || key == "" {
		p.APIKey, p.CredentialUpdatedAt = "", &now
		return nil
	}
	sealed, err := m.opt.Box.Seal([]byte(key), credentialAAD(p.Name))
	if errors.Is(err, secrets.ErrNoKey) {
		return fmt.Errorf("%w: set WEFT_SECRET_KEY (or secrets.keyFile) on the server so credentials can be encrypted, then try again", secrets.ErrNoKey)
	}
	if err != nil {
		return err
	}
	p.APIKey, p.CredentialUpdatedAt = sealed, &now
	return nil
}

// Create adds a managed provider.
func (m *Manager) Create(ctx context.Context, in ProviderInput, actor string, ifRevision int64) (*ProviderView, error) {
	_, _, err := m.mutate(ctx, ifRevision, actor, func(d *document) error {
		if ve := m.validate(&in, d, nil); ve != nil {
			return ve
		}
		key, _ := m.resolveKey(&in, nil)
		if ve := checkBuild(&in, key); ve != nil {
			return ve
		}
		now := time.Now().UTC()
		p := managedProvider{Name: in.Name, CreatedAt: now}
		if err := m.applyInput(&p, &in, key, actor, now); err != nil {
			return err
		}
		d.Providers = append(d.Providers, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.changed(ctx, "created", in.Name, actor)
	return m.Get(in.Name)
}

// Update edits a managed provider. The name cannot change.
func (m *Manager) Update(ctx context.Context, name string, in ProviderInput, actor string, ifRevision int64) (*ProviderView, error) {
	if in.Name != "" && strings.TrimSpace(in.Name) != name {
		return nil, &ValidationError{Errors: []Issue{{"name", "providers cannot be renamed; create a new provider instead"}}}
	}
	in.Name = name
	_, _, err := m.mutate(ctx, ifRevision, actor, func(d *document) error {
		i := d.find(name)
		if i < 0 {
			if m.isStatic(name) {
				return &ValidationError{Errors: []Issue{{"name", fmt.Sprintf("%q is defined in the server configuration and cannot be edited here", name)}}}
			}
			return fmt.Errorf("%w: provider %q", storage.ErrNotFound, name)
		}
		existing := d.Providers[i]
		if ve := m.validate(&in, d, &existing); ve != nil {
			return ve
		}
		key, err := m.resolveKey(&in, &existing)
		if err != nil {
			return &ValidationError{Errors: []Issue{{"apiKey", err.Error()}}}
		}
		if ve := checkBuild(&in, key); ve != nil {
			return ve
		}
		return m.applyInput(&d.Providers[i], &in, key, actor, time.Now().UTC())
	})
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	delete(m.tests, name) // a test of the old settings says nothing about the new ones
	m.mu.Unlock()
	m.changed(ctx, "updated", name, actor)
	return m.Get(name)
}

// Delete removes a managed provider. It refuses to remove the default or a
// provider that graphs select explicitly.
func (m *Manager) Delete(ctx context.Context, name, actor string, ifRevision int64) error {
	if m.isStatic(name) {
		return &ValidationError{Errors: []Issue{{"name", fmt.Sprintf("%q is defined in the server configuration and cannot be removed here", name)}}}
	}
	if m.opt.InUse != nil {
		users, err := m.opt.InUse(ctx, name)
		if err != nil {
			return err
		}
		if len(users) > 0 {
			return fmt.Errorf("%w: provider %q is selected by %s; choose another provider there first", storage.ErrConflict, name, strings.Join(users, ", "))
		}
	}
	_, _, err := m.mutate(ctx, ifRevision, actor, func(d *document) error {
		i := d.find(name)
		if i < 0 {
			return fmt.Errorf("%w: provider %q", storage.ErrNotFound, name)
		}
		if d.Default == name || (d.Default == "" && m.opt.StaticDefault == name) {
			return fmt.Errorf("%w: %q is the default provider; choose another default first", storage.ErrConflict, name)
		}
		d.Providers = append(d.Providers[:i], d.Providers[i+1:]...)
		return nil
	})
	if err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.tests, name)
	m.mu.Unlock()
	m.changed(ctx, "deleted", name, actor)
	return nil
}

// SetDefault selects the default provider (static or managed). An empty
// name returns to the configuration default.
func (m *Manager) SetDefault(ctx context.Context, name, actor string, ifRevision int64) (Meta, error) {
	name = strings.TrimSpace(name)
	_, _, err := m.mutate(ctx, ifRevision, actor, func(d *document) error {
		if name != "" && d.find(name) < 0 && !m.isStatic(name) {
			return &ValidationError{Errors: []Issue{{"default", fmt.Sprintf("unknown provider %q", name)}}}
		}
		d.Default = name
		return nil
	})
	if err != nil {
		return Meta{}, err
	}
	m.changed(ctx, "default", name, actor)
	return m.Meta(), nil
}

func (m *Manager) changed(ctx context.Context, action, name, actor string) {
	m.opt.Log.Info("model provider settings changed", "action", action, "provider", name, "actor", actor)
	if m.opt.Bus != nil {
		m.opt.Bus.Publish(ctx, domain.NewEvent(domain.EvProviderChanged, "", map[string]any{
			"action": action, "provider": name, "revision": m.Meta().Revision,
		}).WithActor(actor))
	}
}

// PluginView is an installed provider plugin as the console sees it.
type PluginView struct {
	llm.Manifest
	Enabled bool `json:"enabled"`
	// EnabledBy is "config" when server configuration provides an instance
	// of the plugin (it cannot be disabled in the console), else "console".
	EnabledBy string     `json:"enabledBy,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	UpdatedBy string     `json:"updatedBy,omitempty"`
	// Instances are the configured providers built from this plugin.
	Instances []string `json:"instances"`
}

func pluginDisabledError(id string) error {
	return fmt.Errorf("the %q inference plugin is disabled; an administrator can enable it under Inference plugins", id)
}

// pluginEnabled: an administrator enabled the plugin, or the server
// configuration provides an instance of it.
func (m *Manager) pluginEnabled(d document, id string) bool {
	return d.Plugins[id].Enabled || m.staticUses(id)
}

func (m *Manager) staticUses(id string) bool {
	for _, c := range m.opt.Static {
		if strings.EqualFold(c.Type, id) {
			return true
		}
	}
	return false
}

// SetPluginEnabled enables or disables a provider plugin. Disabling keeps the
// plugin's instances; they report unavailable until it is enabled again.
func (m *Manager) SetPluginEnabled(ctx context.Context, id string, enabled bool, actor string, ifRevision int64) (*PluginView, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if _, ok := llm.PluginManifest(id); !ok {
		return nil, fmt.Errorf("%w: inference plugin %q", storage.ErrNotFound, id)
	}
	if !enabled && m.staticUses(id) {
		return nil, &ValidationError{Errors: []Issue{{"enabled", fmt.Sprintf("the %q plugin is provided by the server configuration and cannot be disabled here", id)}}}
	}
	_, _, err := m.mutate(ctx, ifRevision, actor, func(d *document) error {
		if d.Plugins == nil {
			d.Plugins = map[string]pluginState{}
		}
		d.Plugins[id] = pluginState{Enabled: enabled, UpdatedAt: time.Now().UTC(), UpdatedBy: actor}
		return nil
	})
	if err != nil {
		return nil, err
	}
	action := "plugin.disabled"
	if enabled {
		action = "plugin.enabled"
	}
	m.opt.Log.Info("inference plugin changed", "action", action, "plugin", id, "actor", actor)
	if m.opt.Bus != nil {
		m.opt.Bus.Publish(ctx, domain.NewEvent(domain.EvProviderChanged, "", map[string]any{
			"action": action, "plugin": id, "revision": m.Meta().Revision,
		}).WithActor(actor))
	}
	for _, v := range m.Plugins() {
		if v.ID == id {
			return &v, nil
		}
	}
	return nil, fmt.Errorf("%w: inference plugin %q", storage.ErrNotFound, id)
}

// Plugins lists the installed provider plugins with their instances.
func (m *Manager) Plugins() []PluginView {
	m.mu.Lock()
	doc := m.doc
	m.mu.Unlock()
	byType := map[string][]string{}
	for _, info := range m.opt.Registry.List() {
		byType[info.Type] = append(byType[info.Type], info.Name)
	}
	var out []PluginView
	for _, man := range llm.Plugins() {
		inst := byType[man.ID]
		if inst == nil {
			inst = []string{}
		}
		v := PluginView{Manifest: man, Instances: inst}
		switch st, ok := doc.Plugins[man.ID]; {
		case m.staticUses(man.ID):
			v.Enabled, v.EnabledBy = true, "config"
		case ok:
			at := st.UpdatedAt
			v.Enabled, v.UpdatedAt, v.UpdatedBy = st.Enabled, &at, st.UpdatedBy
			if st.Enabled {
				v.EnabledBy = "console"
			}
		}
		out = append(out, v)
	}
	return out
}
