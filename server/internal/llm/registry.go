package llm

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
)

// Registry holds the configured providers and is the LLM gateway every
// caller goes through. Wrapping calls here (rather than in each adapter)
// means concurrency limits, metrics and traces are uniform no matter which
// backend answers, and an adapter author only writes the wire mapping.
//
// The provider set can be replaced at runtime (operators manage providers in
// the console). Replace swaps the whole set atomically; calls already in
// flight finish on the provider they resolved.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]*entry
	order     []string
	def       string
	observer  func(context.Context, Call)
}

// Call describes one model call for accounting (package cost). Source is
// "gateway" for calls through Chat and "external" for usage an external
// runtime reports for inference outside the gateway.
type Call struct {
	Source   string
	Provider string
	Type     string
	Model    string
	Metadata map[string]string
	Usage    Usage
	// Estimated: the provider did not report usage (failure after dispatch,
	// or zero usage), so Usage is the conservative request estimate.
	Estimated bool
	Err       error
}

// DefaultEstimatedOutput is the output allowance assumed for an estimate
// when the request sets none.
const DefaultEstimatedOutput = 4096

// SetObserver registers fn to receive every dispatched call. It is called
// synchronously after the provider answers or fails.
func (r *Registry) SetObserver(fn func(context.Context, Call)) {
	r.mu.Lock()
	r.observer = fn
	r.mu.Unlock()
}

// ReportExternal passes usage an external runtime reported to the observer.
func (r *Registry) ReportExternal(ctx context.Context, c Call) {
	c.Source = "external"
	r.observe(ctx, c)
}

func (r *Registry) observe(ctx context.Context, c Call) {
	r.mu.RLock()
	fn := r.observer
	r.mu.RUnlock()
	if fn != nil {
		fn(ctx, c)
	}
}

// accounted turns a provider outcome into a Call: reported usage when the
// provider gave it, otherwise a conservative estimate. A call the provider
// rejected (HTTP 4xx other than 408/429) was not served and is not charged.
func accounted(req *ChatRequest, resp *ChatResponse, err error) (Usage, bool, bool) {
	if resp != nil && (resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0) {
		return resp.Usage, false, true
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 && apiErr.Status != 408 && apiErr.Status != 429 {
		return Usage{}, false, false
	}
	out := req.MaxTokens
	if out <= 0 {
		out = DefaultEstimatedOutput
	}
	return Usage{InputTokens: EstimateTokens(req), OutputTokens: out}, true, true
}

type entry struct {
	cfg ProviderConfig
	p   Provider
	sem chan struct{} // nil when unlimited
}

// ProviderInfo is the public description of a configured provider (no secrets).
type ProviderInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	BaseURL      string `json:"baseUrl,omitempty"`
	DefaultModel string `json:"defaultModel,omitempty"`
	Default      bool   `json:"default"`
}

// NewRegistry builds every provider in cfgs. defaultName selects the default
// provider; empty means the first configured one.
func NewRegistry(cfgs []ProviderConfig, defaultName string) (*Registry, error) {
	r := &Registry{providers: map[string]*entry{}}
	if err := r.Replace(cfgs, defaultName); err != nil {
		return nil, err
	}
	return r, nil
}

// Replace builds every provider in cfgs and, only if all of them build,
// swaps them in as the registry's provider set. On error the current set is
// left untouched.
func (r *Registry) Replace(cfgs []ProviderConfig, defaultName string) error {
	entries := make([]Entry, 0, len(cfgs))
	for _, c := range cfgs {
		if c.Name == "" {
			c.Name = c.Type
		}
		p, err := New(c)
		if err != nil {
			return fmt.Errorf("llm: provider %q: %w", c.Name, err)
		}
		entries = append(entries, Entry{Config: c, Provider: p})
	}
	return r.ReplaceProviders(entries, defaultName)
}

// Entry is a built provider and the configuration it came from.
type Entry struct {
	Config   ProviderConfig
	Provider Provider
}

// ReplaceProviders swaps in an already-built provider set. Names must be
// unique and defaultName (when set) must be one of them; otherwise the
// current set is left untouched.
func (r *Registry) ReplaceProviders(entries []Entry, defaultName string) error {
	next := &Registry{providers: map[string]*entry{}}
	for _, en := range entries {
		c := en.Config
		if c.Name == "" {
			c.Name = en.Provider.Name()
		}
		if _, dup := next.providers[c.Name]; dup {
			return fmt.Errorf("llm: duplicate provider name %q", c.Name)
		}
		next.addLocked(c, en.Provider)
	}
	if defaultName != "" {
		if _, ok := next.providers[defaultName]; !ok {
			return fmt.Errorf("llm: default provider %q is not configured", defaultName)
		}
		next.def = defaultName
	}
	r.mu.Lock()
	r.providers, r.order, r.def = next.providers, next.order, next.def
	r.mu.Unlock()
	return nil
}

// Add registers an already-built provider (useful for tests and embedding).
func (r *Registry) Add(cfg ProviderConfig, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addLocked(cfg, p)
}

func (r *Registry) addLocked(cfg ProviderConfig, p Provider) {
	if cfg.Name == "" {
		cfg.Name = p.Name()
	}
	e := &entry{cfg: cfg, p: p}
	if cfg.Concurrency > 0 {
		e.sem = make(chan struct{}, cfg.Concurrency)
	}
	if _, exists := r.providers[cfg.Name]; !exists {
		r.order = append(r.order, cfg.Name)
	}
	r.providers[cfg.Name] = e
	if r.def == "" {
		r.def = cfg.Name
	}
}

// Get returns a provider by name.
func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.providers[name]
	if !ok {
		return nil, false
	}
	return e.p, true
}

// Default returns the default provider (nil if none configured).
func (r *Registry) Default() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.providers[r.def]; ok {
		return e.p
	}
	return nil
}

// DefaultName is the default provider's name.
func (r *Registry) DefaultName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.def
}

// lookup resolves a provider name (empty or the "default" alias → default)
// to its entry under the read lock.
func (r *Registry) lookup(name string) (*entry, string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if name == "" || (name == "default" && r.providers["default"] == nil) {
		name = r.def
	}
	e, ok := r.providers[name]
	return e, name, ok
}

// Resolve maps a node's model reference to a provider and concrete model.
// Empty provider → default (the name "default" is accepted as an alias);
// empty model → the provider's DefaultModel. A provider registered as
// Unavailable resolves to its ErrUnavailable reason.
func (r *Registry) Resolve(ref domain.ModelRef) (Provider, string, error) {
	e, name, ok := r.lookup(ref.Provider)
	if !ok {
		if name == "" {
			return nil, "", fmt.Errorf("llm: no providers configured")
		}
		return nil, "", fmt.Errorf("llm: unknown provider %q", name)
	}
	if err := UnavailableReason(e.p); err != nil {
		return nil, "", err
	}
	model := ref.Name
	if model == "" {
		model = e.cfg.DefaultModel
	}
	return e.p, model, nil
}

// List describes configured providers in config order.
func (r *Registry) List() []ProviderInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderInfo, 0, len(r.order))
	for _, n := range r.order {
		e := r.providers[n]
		out = append(out, ProviderInfo{
			Name: n, Type: e.cfg.Type, BaseURL: e.cfg.BaseURL,
			DefaultModel: e.cfg.DefaultModel, Default: n == r.def,
		})
	}
	return out
}

// ModelGuidance returns a provider's model guidance ("" when standard or
// unknown).
func (r *Registry) ModelGuidance(name string) string {
	e, _, ok := r.lookup(name)
	if !ok {
		return ""
	}
	return e.cfg.ModelGuidance
}

// Type returns a provider's plugin type ("" when unknown).
func (r *Registry) Type(name string) string {
	e, _, ok := r.lookup(name)
	if !ok {
		return ""
	}
	return e.cfg.Type
}

// Names returns provider names sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	out := append([]string(nil), r.order...)
	r.mu.RUnlock()
	sort.Strings(out)
	return out
}

// Chat calls providerName (empty → default) with uniform instrumentation:
// concurrency limiting, Prometheus metrics and a gen_ai OpenTelemetry span.
// An empty req.Model is filled with the provider's default model.
func (r *Registry) Chat(ctx context.Context, providerName string, req *ChatRequest) (*ChatResponse, error) {
	return r.chat(ctx, providerName, req, nil)
}

// ChatStream is Chat with the answer's text passed to onText as it arrives,
// when the provider streams (StreamingProvider); other providers answer as
// Chat does and onText is not called. Limits, metrics, the span and the
// observed call (accounting) are exactly Chat's, and the returned response
// is the same one Chat would return. A stream that fails midway returns its
// error and no response.
func (r *Registry) ChatStream(ctx context.Context, providerName string, req *ChatRequest, onText func(string)) (*ChatResponse, error) {
	if onText == nil {
		onText = func(string) {}
	}
	return r.chat(ctx, providerName, req, onText)
}

func (r *Registry) chat(ctx context.Context, providerName string, req *ChatRequest, onText func(string)) (*ChatResponse, error) {
	e, providerName, ok := r.lookup(providerName)
	if !ok {
		return nil, fmt.Errorf("llm: unknown provider %q", providerName)
	}
	if req.Model == "" {
		req.Model = e.cfg.DefaultModel
	}
	model := req.Model
	if model == "" {
		model = "default"
	}

	ctx, span := observability.Start(ctx, "llm.chat",
		attribute.String("gen_ai.system", e.cfg.Type),
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.request.model", model),
		attribute.String("weft.llm.provider", providerName),
		attribute.Int("weft.llm.tools", len(req.Tools)),
	)
	if req.MaxTokens > 0 {
		span.SetAttributes(attribute.Int("gen_ai.request.max_tokens", req.MaxTokens))
	}
	if req.Temperature != nil {
		span.SetAttributes(attribute.Float64("gen_ai.request.temperature", *req.Temperature))
	}

	if e.sem != nil {
		select {
		case e.sem <- struct{}{}:
			defer func() { <-e.sem }()
		case <-ctx.Done():
			observability.LLMRequests.WithLabelValues(providerName, model, "cancelled").Inc()
			observability.EndSpan(span, ctx.Err())
			return nil, ctx.Err()
		}
	}

	sp, streaming := e.p.(StreamingProvider)
	streaming = streaming && onText != nil
	span.SetAttributes(attribute.Bool("weft.llm.stream", streaming))
	start := time.Now()
	var resp *ChatResponse
	var err error
	if streaming {
		resp, err = sp.ChatStream(ctx, req, onText)
	} else {
		resp, err = e.p.Chat(ctx, req)
	}
	observability.LLMDuration.WithLabelValues(providerName, model).Observe(time.Since(start).Seconds())
	if usage, estimated, charged := accounted(req, resp, err); charged && UnavailableReason(e.p) == nil {
		r.observe(ctx, Call{Source: "gateway", Provider: providerName, Type: e.cfg.Type, Model: req.Model,
			Metadata: req.Metadata, Usage: usage, Estimated: estimated, Err: err})
	}
	if err != nil {
		outcome := "error"
		if ctx.Err() != nil {
			outcome = "cancelled"
		}
		observability.LLMRequests.WithLabelValues(providerName, model, outcome).Inc()
		observability.EndSpan(span, err)
		return nil, err
	}
	observability.LLMRequests.WithLabelValues(providerName, model, "ok").Inc()
	observability.LLMTokens.WithLabelValues(providerName, model, "input").Add(float64(resp.Usage.InputTokens))
	observability.LLMTokens.WithLabelValues(providerName, model, "output").Add(float64(resp.Usage.OutputTokens))
	span.SetAttributes(
		attribute.String("gen_ai.response.model", resp.Model),
		attribute.Int("gen_ai.usage.input_tokens", resp.Usage.InputTokens),
		attribute.Int("gen_ai.usage.output_tokens", resp.Usage.OutputTokens),
		attribute.StringSlice("gen_ai.response.finish_reasons", []string{resp.StopReason}),
	)
	observability.EndSpan(span, nil)
	return resp, nil
}

// Models lists models for a provider.
func (r *Registry) Models(ctx context.Context, providerName string) ([]ModelInfo, error) {
	p, ok := r.Get(providerName)
	if !ok {
		return nil, fmt.Errorf("llm: unknown provider %q", providerName)
	}
	return p.Models(ctx)
}
