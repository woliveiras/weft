package llm

import (
	"errors"
	"sort"
	"strings"
)

// A plugin is a provider type: the adapter code plus a manifest that tells
// the console what it is, what it can do and how to configure it. Plugins
// are installed when they are available to the server (built-in plugins are
// compiled in), enabled by an administrator (package llmconfig), and then
// configured as one or more provider instances.

// KeyMode says whether instances of a plugin need an API key.
type KeyMode string

const (
	KeyRequired KeyMode = "required"
	KeyOptional KeyMode = "optional"
	KeyNone     KeyMode = "none"
)

// JSONMode says how a plugin asks for a JSON object answer.
type JSONMode string

const (
	JSONNative      JSONMode = "native"      // a request parameter of the backend
	JSONInstruction JSONMode = "instruction" // an added system instruction
)

// Capabilities are what the adapter supports through the gateway.
type Capabilities struct {
	Tools        bool     `json:"tools"`
	JSON         JSONMode `json:"json"`
	Usage        bool     `json:"usage"`
	ModelListing bool     `json:"modelListing"`
	Streaming    bool     `json:"streaming"`
}

// Optional instance settings a plugin honours, beyond name, base URL,
// default model, timeout, retries and concurrency that every plugin has.
const SettingContextWindow = "contextWindow"

// Manifest describes a plugin. ID is the provider type stored on instances.
type Manifest struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	// Builtin plugins are compiled into the server.
	Builtin bool `json:"builtin"`
	// Local plugins talk to a model server the operator runs (on this
	// machine or network) rather than a hosted API.
	Local          bool         `json:"local"`
	DefaultBaseURL string       `json:"defaultBaseUrl,omitempty"`
	Key            KeyMode      `json:"key"`
	Capabilities   Capabilities `json:"capabilities"`
	Settings       []string     `json:"settings,omitempty"`
	// DevOnly plugins are offered only when the server enables development
	// plugins in its configuration.
	DevOnly bool `json:"devOnly,omitempty"`
}

type plugin struct {
	manifest Manifest
	factory  Factory
}

var plugins = map[string]plugin{}

// RegisterPlugin makes a provider type available. Adding a backend is:
// implement Provider and call RegisterPlugin in an init().
func RegisterPlugin(m Manifest, f Factory) {
	m.ID = strings.ToLower(m.ID)
	if m.ID == "" || m.Label == "" || f == nil {
		panic("llm: a plugin needs an id, a label and a factory")
	}
	if m.Key == "" {
		m.Key = KeyNone
	}
	if _, dup := plugins[m.ID]; dup {
		panic("llm: plugin " + m.ID + " registered twice")
	}
	m.Builtin = true
	plugins[m.ID] = plugin{manifest: m, factory: f}
}

// Plugins lists every installed plugin, sorted by id.
func Plugins() []Manifest {
	out := make([]Manifest, 0, len(plugins))
	for _, p := range plugins {
		out = append(out, p.manifest)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// PluginManifest returns an installed plugin's manifest.
func PluginManifest(id string) (Manifest, bool) {
	p, ok := plugins[strings.ToLower(id)]
	return p.manifest, ok
}

// New builds a provider from config.
func New(cfg ProviderConfig) (Provider, error) {
	p, ok := plugins[strings.ToLower(cfg.Type)]
	if !ok {
		return nil, errors.New("llm: unknown provider type " + cfg.Type)
	}
	return p.factory(cfg)
}
