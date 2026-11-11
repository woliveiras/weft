package llmconfig

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
)

// TestResult is the outcome of a provider connection test. A test lists the
// provider's models (informational: not every backend supports it) and then
// sends one tiny chat request through the LLM gateway - the same path agent
// turns use - so a passing test means inference works, not only that the
// endpoint answers.
type TestResult struct {
	Provider   string    `json:"provider"`
	OK         bool      `json:"ok"`
	CheckedAt  time.Time `json:"checkedAt"`
	DurationMS int64     `json:"durationMs"`
	// Draft is true for tests of unsaved settings.
	Draft     bool           `json:"draft,omitempty"`
	Model     string         `json:"model,omitempty"`
	Models    ModelsCheck    `json:"models"`
	Inference InferenceCheck `json:"inference"`
	// Tools tells whether the model calls tools in the structured form
	// agents need. It is advisory: OK reflects inference only.
	Tools ToolsCheck `json:"tools"`
}

// ToolsCheck is the result of offering the model one tool.
type ToolsCheck struct {
	// Checked is false when inference failed, so tools were not tried.
	Checked bool `json:"checked"`
	OK      bool `json:"ok"`
	// Reply is the text the model wrote instead of calling the tool.
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

type ModelsCheck struct {
	OK    bool   `json:"ok"`
	Count int    `json:"count"`
	Error string `json:"error,omitempty"`
	// DefaultModelListed is set when the provider has a default model and
	// the listing succeeded.
	DefaultModelListed *bool `json:"defaultModelListed,omitempty"`
}

type InferenceCheck struct {
	OK           bool   `json:"ok"`
	Reply        string `json:"reply,omitempty"`
	StopReason   string `json:"stopReason,omitempty"`
	InputTokens  int    `json:"inputTokens,omitempty"`
	OutputTokens int    `json:"outputTokens,omitempty"`
	DurationMS   int64  `json:"durationMs,omitempty"`
	Error        string `json:"error,omitempty"`
}

const testPrompt = "This is a connection test from Weft. Reply with the single word OK."

const toolTestPrompt = "This is a tool-calling test from Weft. Call the connection_check tool with word set to OK. Do not answer in text."

var toolTestSchema = llm.ToolSchema{
	Name:        "connection_check",
	Description: "Confirms that tool calling works. Call it with word=OK.",
	Parameters:  []byte(`{"type":"object","properties":{"word":{"type":"string"}},"required":["word"]}`),
}

// Test checks a saved provider (static or managed) through the shared
// registry.
func (m *Manager) Test(ctx context.Context, name, actor string) (*TestResult, error) {
	p, ok := m.opt.Registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: provider %q", storage.ErrNotFound, name)
	}
	var defModel string
	for _, info := range m.opt.Registry.List() {
		if info.Name == name {
			defModel = info.DefaultModel
		}
	}
	var redact []string
	if mp, ok := m.managed(name); ok && mp.APIKey != "" {
		if k, err := m.opt.Box.Open(mp.APIKey, credentialAAD(name)); err == nil {
			redact = append(redact, string(k))
		}
	}
	for _, c := range m.opt.Static {
		if c.Name == name {
			if k, err := c.APIKey(); err == nil && k != "" {
				redact = append(redact, k)
			}
		}
	}
	res := m.run(ctx, m.opt.Registry, p.Name(), defModel, redact)
	m.mu.Lock()
	m.tests[name] = res
	m.mu.Unlock()
	m.tested(ctx, res, actor)
	return res, nil
}

// TestDraft checks unsaved settings. When the draft names an existing
// managed provider and does not carry a new credential, the stored one is
// used, so an operator can test an edit without re-typing the key.
func (m *Manager) TestDraft(ctx context.Context, in ProviderInput, actor string) (*TestResult, error) {
	m.mu.Lock()
	d := m.doc.clone()
	m.mu.Unlock()
	var existing *managedProvider
	if i := d.find(strings.TrimSpace(in.Name)); i >= 0 {
		existing = &d.Providers[i]
	}
	if existing == nil && in.Name == "" {
		in.Name = "draft"
	}
	if ve := m.validate(&in, &d, existing); ve != nil {
		// Name clashes do not matter for a dry run.
		var keep []Issue
		for _, i := range ve.Errors {
			if i.Path != "name" {
				keep = append(keep, i)
			}
		}
		if len(keep) > 0 {
			return nil, &ValidationError{Errors: keep}
		}
	}
	key, err := m.resolveKey(&in, existing)
	if err != nil {
		return nil, &ValidationError{Errors: []Issue{{"apiKey", err.Error()}}}
	}
	if ve := checkBuild(&in, key); ve != nil {
		return nil, ve
	}
	cfg := llm.ProviderConfig{
		Name: in.Name, Type: in.Type, BaseURL: in.BaseURL, DefaultModel: in.DefaultModel,
		TimeoutSec: in.TimeoutSec, MaxRetries: in.MaxRetries, Concurrency: in.Concurrency, ContextWindow: in.ContextWindow, ModelGuidance: in.ModelGuidance, APIKeyValue: key,
	}
	reg, err := llm.NewRegistry([]llm.ProviderConfig{cfg}, cfg.Name)
	if err != nil {
		return nil, &ValidationError{Errors: []Issue{{"type", secrets.Redact(err.Error(), key)}}}
	}
	res := m.run(ctx, reg, cfg.Name, cfg.DefaultModel, []string{key})
	res.Draft = true
	m.tested(ctx, res, actor)
	return res, nil
}

func (m *Manager) managed(name string) (managedProvider, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := m.doc.find(name); i >= 0 {
		return m.doc.Providers[i], true
	}
	return managedProvider{}, false
}

func (m *Manager) run(ctx context.Context, reg *llm.Registry, name, defModel string, redact []string) *TestResult {
	ctx, cancel := context.WithTimeout(ctx, m.opt.TestTimeout)
	defer cancel()
	start := time.Now()
	res := &TestResult{Provider: name, CheckedAt: start.UTC()}
	clean := func(err error) string {
		msg := secrets.Redact(err.Error(), redact...)
		if errors.Is(err, context.DeadlineExceeded) {
			msg = fmt.Sprintf("timed out after %s: %s", m.opt.TestTimeout, msg)
		}
		return truncate(msg, 600)
	}

	mctx, mcancel := context.WithTimeout(ctx, 20*time.Second)
	models, err := reg.Models(mctx, name)
	mcancel()
	if err != nil {
		res.Models.Error = clean(err)
	} else {
		res.Models.OK, res.Models.Count = true, len(models)
		if defModel != "" {
			listed := false
			for _, mi := range models {
				if sameModel(mi.ID, defModel) {
					listed = true
				}
			}
			res.Models.DefaultModelListed = &listed
		}
	}

	res.Model = defModel
	if res.Model == "" && len(models) > 0 {
		res.Model = models[0].ID
	}
	if res.Model == "" {
		res.Inference.Error = "no model to test: set a default model for this provider"
	} else {
		istart := time.Now()
		resp, err := reg.Chat(ctx, name, &llm.ChatRequest{
			Model:    res.Model,
			Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: testPrompt}},
			// Small but not tiny: some models spend tokens before answering.
			MaxTokens: 64,
			Metadata:  map[string]string{"purpose": "provider-test"},
		})
		res.Inference.DurationMS = time.Since(istart).Milliseconds()
		if err != nil {
			res.Inference.Error = clean(err)
		} else {
			res.Inference.OK = true
			res.Inference.Reply = truncate(secrets.Redact(resp.Message.Content, redact...), 200)
			res.Inference.StopReason = resp.StopReason
			res.Inference.InputTokens, res.Inference.OutputTokens = resp.Usage.InputTokens, resp.Usage.OutputTokens
			if resp.Model != "" {
				res.Model = resp.Model
			}
		}
	}
	if res.Inference.OK {
		res.Tools.Checked = true
		resp, err := reg.Chat(ctx, name, &llm.ChatRequest{
			Model:     res.Model,
			Messages:  []llm.ChatMessage{{Role: llm.RoleUser, Content: toolTestPrompt}},
			Tools:     []llm.ToolSchema{toolTestSchema},
			MaxTokens: 256,
			Metadata:  map[string]string{"purpose": "provider-test"},
		})
		switch {
		case err != nil:
			res.Tools.Error = clean(err)
		case len(resp.Message.ToolCalls) > 0 && resp.Message.ToolCalls[0].Name == toolTestSchema.Name:
			res.Tools.OK = true
		default:
			res.Tools.Reply = truncate(secrets.Redact(resp.Message.Content, redact...), 200)
			res.Tools.Error = "the model answered in text instead of calling the tool; agents need a model with tool calling"
		}
	}
	res.OK = res.Inference.OK
	res.DurationMS = time.Since(start).Milliseconds()
	return res
}

func (m *Manager) tested(ctx context.Context, res *TestResult, actor string) {
	data := map[string]any{
		"provider": res.Provider, "ok": res.OK, "draft": res.Draft, "model": res.Model, "durationMs": res.DurationMS,
	}
	if res.Inference.Error != "" {
		data["error"] = res.Inference.Error
	}
	if res.Tools.Checked {
		data["toolCalling"] = res.Tools.OK
	}
	level := "info"
	if !res.OK {
		level = "warn"
		m.opt.Log.Warn("model provider test failed", "provider", res.Provider, "draft", res.Draft, "error", res.Inference.Error)
	}
	if m.opt.Bus != nil {
		ev := domain.NewEvent(domain.EvProviderTested, "", data).WithActor(actor)
		ev.Level = level
		m.opt.Bus.Publish(ctx, ev)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

// sameModel compares model IDs, ignoring the "models/" prefix some
// providers put on listed IDs (Gemini lists "models/gemini-2.5-flash" but
// accepts "gemini-2.5-flash" in requests).
func sameModel(listed, configured string) bool {
	return strings.TrimPrefix(listed, "models/") == strings.TrimPrefix(configured, "models/")
}
