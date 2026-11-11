package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/llm"
)

// Registry holds the tools known to the platform. It is safe for
// concurrent use; tools are normally registered once at startup.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

// Register adds a tool; a duplicate name is an error (two tools answering
// to one name would make grants ambiguous).
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[t.Name()]; ok {
		return fmt.Errorf("tool %q already registered", t.Name())
	}
	r.tools[t.Name()] = t
	return nil
}

// MustRegister panics on duplicates (startup wiring).
func (r *Registry) MustRegister(ts ...Tool) {
	for _, t := range ts {
		if err := r.Register(t); err != nil {
			panic(err)
		}
	}
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns all tools sorted by name.
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// ToolInfo is the API description of a tool (GET /api/v1/tools).
type ToolInfo struct {
	Name        string          `json:"name"`
	Capability  string          `json:"capability"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

func (r *Registry) Describe() []ToolInfo {
	ts := r.List()
	out := make([]ToolInfo, len(ts))
	for i, t := range ts {
		out[i] = ToolInfo{Name: t.Name(), Capability: t.Capability(), Description: t.Description(), Schema: t.Schema()}
	}
	return out
}

// Schemas returns the schemas of the tools p is granted, for the model.
// Hiding ungranted tools is defence in depth: the gateway would refuse them
// anyway, but a model that never sees a tool never wastes a turn on it.
func (r *Registry) Schemas(p *Principal) []llm.ToolSchema {
	var out []llm.ToolSchema
	for _, t := range r.List() {
		if len(MatchingGrants(p, t)) == 0 {
			continue
		}
		if pt, ok := t.(principalSchema); ok {
			if s, visible := pt.SchemaFor(p); visible {
				out = append(out, s)
			}
			continue
		}
		out = append(out, llm.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: t.Schema()})
	}
	return out
}

// principalSchema is implemented by tools whose schema depends on the
// caller (check.run lists the caller's declared checks, and is hidden
// when there are none).
type principalSchema interface {
	SchemaFor(p *Principal) (llm.ToolSchema, bool)
}

// BuiltinConfig tunes the builtin tools.
type BuiltinConfig struct {
	// AllowPrivateNetworks lets http.fetch reach loopback/private/link-local
	// addresses. Off by default (SSRF protection).
	AllowPrivateNetworks bool
	// HTTPTimeout for http.fetch (default 30s).
	HTTPTimeout time.Duration
	// UserAgent for http.fetch.
	UserAgent string
	// MaxReadBytes caps filesystem.read (default 1 MiB).
	MaxReadBytes int64
}

func (c BuiltinConfig) withDefaults() BuiltinConfig {
	if c.HTTPTimeout <= 0 {
		c.HTTPTimeout = 30 * time.Second
	}
	if c.UserAgent == "" {
		c.UserAgent = "Weft-Agent/1.0"
	}
	if c.MaxReadBytes <= 0 {
		c.MaxReadBytes = 1 << 20
	}
	return c
}

// Builtin returns a registry with every builtin tool registered.
func Builtin(cfg BuiltinConfig) *Registry {
	cfg = cfg.withDefaults()
	r := NewRegistry()
	r.MustRegister(filesystemTools(cfg)...)
	r.MustRegister(editTools()...)
	r.MustRegister(codeTools()...)
	r.MustRegister(shellTools()...)
	r.MustRegister(checkTools()...)
	r.MustRegister(gitTools()...)
	r.MustRegister(httpTools(cfg)...)
	r.MustRegister(humanTools()...)
	r.MustRegister(delegationTools()...)
	return r
}

// funcTool is the common implementation of builtin tools.
type funcTool struct {
	name, capability, description string
	schema                        json.RawMessage
	fn                            func(ctx context.Context, inv *Invocation) (*Result, error)
}

func (t *funcTool) Name() string            { return t.name }
func (t *funcTool) Capability() string      { return t.capability }
func (t *funcTool) Description() string     { return t.description }
func (t *funcTool) Schema() json.RawMessage { return t.schema }
func (t *funcTool) Invoke(ctx context.Context, inv *Invocation) (*Result, error) {
	return t.fn(ctx, inv)
}

// decodeArgs unmarshals args strictly enough to catch typos in field types.
func decodeArgs(inv *Invocation, v any) error {
	if len(inv.Args) == 0 {
		return json.Unmarshal([]byte(`{}`), v)
	}
	if err := json.Unmarshal(inv.Args, v); err != nil {
		return argError(err)
	}
	return nil
}

func textResult(s string) *Result { return &Result{Content: s} }

func jsonResult(content string, data any) *Result {
	b, _ := json.Marshal(data)
	return &Result{Content: content, Data: b}
}

func workdirOf(inv *Invocation) string {
	if inv.Workdir != "" {
		return inv.Workdir
	}
	if inv.Principal != nil {
		return inv.Principal.Workdir
	}
	return ""
}

func sandboxOf(inv *Invocation) Sandbox {
	if inv.Sandbox != nil {
		return inv.Sandbox
	}
	if inv.Principal != nil && inv.Principal.Sandbox != nil {
		return inv.Principal.Sandbox
	}
	return &ProcessSandbox{Root: workdirOf(inv)}
}
