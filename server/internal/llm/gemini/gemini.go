// Package gemini implements Google's native Gemini protocol
// (generateContent) for two services:
//
//   - the Gemini API (generativelanguage.googleapis.com), authenticated with
//     an API key in the x-goog-api-key header;
//   - Vertex AI, authenticated with the server's Application Default
//     Credentials, selected by setting a project (and optionally a region).
//
// Mapping notes and trade-offs:
//   - The system prompt is systemInstruction; mid-conversation system
//     messages are folded into it.
//   - Assistant turns are "model" contents. Tool results are functionResponse
//     parts in a "user" turn; consecutive results are merged into one turn,
//     as the API expects one turn answering every call of the previous one.
//   - Gemini 3 attaches a thought signature to function calls and rejects
//     the next turn unless it is sent back. It is kept in
//     llm.ToolCall.ProviderData in the same shape the OpenAI-compatible
//     endpoint uses ({"google":{"thought_signature":...}}), so a
//     conversation can move between the two plugins.
//   - Tool parameters are sent as parametersJsonSchema, which accepts JSON
//     Schema as tools declare it, instead of the OpenAPI subset of
//     "parameters".
//   - JSON mode is responseMimeType application/json.
//   - Thinking tokens are billed as output and counted as output.
package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/llm"
)

const (
	defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	defaultRegion  = "global"
	// maxModelPages bounds model listing pagination.
	maxModelPages = 20
)

func init() {
	llm.RegisterPlugin(llm.Manifest{
		ID: "gemini", Label: "Google Gemini",
		Description:    "Gemini models through the Gemini API (API key) or Vertex AI (the server's Application Default Credentials; set a project).",
		DefaultBaseURL: defaultBaseURL, Key: llm.KeyOptional,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONNative, Usage: true, ModelListing: true},
		Settings:     []string{llm.SettingContextWindow, llm.SettingProject, llm.SettingRegion},
	}, func(c llm.ProviderConfig) (llm.Provider, error) { return New(c) })
}

var (
	// Google Cloud project IDs: 6-30 lowercase letters, digits and hyphens.
	projectPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	// Regions such as europe-west4, us-central1 or global.
	regionPattern = regexp.MustCompile(`^[a-z]+(-[a-z]+[0-9]*)*$`)
)

// Provider is a Gemini API or Vertex AI client.
type Provider struct {
	cfg  llm.ProviderConfig
	http *llm.HTTPClient
	// vertex is set when the provider uses Vertex AI.
	vertex *vertex
}

type vertex struct {
	project, region string
	tokens          *tokenSource
}

// New builds a provider. A configured project selects Vertex AI with
// Application Default Credentials; otherwise an API key is required.
func New(cfg llm.ProviderConfig) (*Provider, error) {
	key, err := cfg.APIKey()
	if err != nil {
		return nil, err
	}
	if cfg.Project == "" {
		if cfg.Region != "" {
			return nil, fmt.Errorf("llm %s: a region applies to Vertex AI only; set the Vertex AI project too, or clear the region to use the Gemini API", cfg.Name)
		}
		if key == "" {
			return nil, fmt.Errorf("llm %s: an API key is required for the Gemini API (or set a Vertex AI project to use the server's Application Default Credentials)", cfg.Name)
		}
		base := cfg.BaseURL
		if base == "" {
			base = defaultBaseURL
		}
		return &Provider{cfg: cfg, http: llm.NewHTTPClient(cfg, base, map[string]string{"x-goog-api-key": key})}, nil
	}

	if key != "" {
		return nil, fmt.Errorf("llm %s: Vertex AI uses the server's Application Default Credentials, not an API key; remove the key, or clear the project to use the Gemini API", cfg.Name)
	}
	if !projectPattern.MatchString(cfg.Project) {
		return nil, fmt.Errorf("llm %s: %q is not a Google Cloud project ID (6-30 lowercase letters, digits and hyphens)", cfg.Name, cfg.Project)
	}
	region := cfg.Region
	if region == "" {
		region = defaultRegion
	}
	if len(region) > 40 || !regionPattern.MatchString(region) {
		return nil, fmt.Errorf("llm %s: %q is not a Google Cloud region (for example global, us-central1 or europe-west4)", cfg.Name, region)
	}
	tokens, err := findADC(&http.Client{Timeout: 30 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("llm %s: %w", cfg.Name, err)
	}
	base := cfg.BaseURL
	if base == "" {
		base = "https://aiplatform.googleapis.com"
		if region != defaultRegion {
			base = "https://" + region + "-aiplatform.googleapis.com"
		}
	}
	p := &Provider{cfg: cfg, vertex: &vertex{project: cfg.Project, region: region, tokens: tokens}}
	p.http = llm.NewHTTPClient(cfg, base, nil)
	p.http.Authorize = p.authorize
	return p, nil
}

func (p *Provider) Name() string { return p.cfg.Name }

// HTTP exposes the underlying client (tests tune backoff through it).
func (p *Provider) HTTP() *llm.HTTPClient { return p.http }

// ContextWindow is the configured context window (0: not configured, the
// model's own limit applies).
func (p *Provider) ContextWindow() int { return p.cfg.ContextWindow }

func (p *Provider) authorize(ctx context.Context, h http.Header) error {
	tok, err := p.vertex.tokens.Token(ctx)
	if err != nil {
		return err
	}
	h.Set("Authorization", "Bearer "+tok)
	// User credentials bill a quota project: theirs, else the provider's.
	if ts := p.vertex.tokens; ts.user {
		qp := ts.quotaProject
		if qp == "" {
			qp = p.vertex.project
		}
		h.Set("x-goog-user-project", qp)
	}
	return nil
}

// modelID strips the "models/" prefix the Gemini API lists models with.
func modelID(m string) string { return strings.TrimPrefix(m, "models/") }

func (p *Provider) chatPath(model string) string {
	m := url.PathEscape(modelID(model))
	if p.vertex == nil {
		return "/models/" + m + ":generateContent"
	}
	return fmt.Sprintf("/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent", p.vertex.project, p.vertex.region, m)
}

type part struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
}

type functionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type functionResponse struct {
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type functionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parametersJsonSchema,omitempty"`
}

type toolDecl struct {
	FunctionDeclarations []functionDecl `json:"functionDeclarations"`
}

type generationConfig struct {
	Temperature      *float64 `json:"temperature,omitempty"`
	MaxOutputTokens  int      `json:"maxOutputTokens,omitempty"`
	StopSequences    []string `json:"stopSequences,omitempty"`
	ResponseMimeType string   `json:"responseMimeType,omitempty"`
}

type request struct {
	Contents          []content         `json:"contents"`
	SystemInstruction *content          `json:"systemInstruction,omitempty"`
	Tools             []toolDecl        `json:"tools,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
}

type response struct {
	Candidates []struct {
		Content       content `json:"content"`
		FinishReason  string  `json:"finishReason"`
		FinishMessage string  `json:"finishMessage"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason        string `json:"blockReason"`
		BlockReasonMessage string `json:"blockReasonMessage"`
	} `json:"promptFeedback"`
	UsageMetadata struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
		ToolUsePromptTokenCount int `json:"toolUsePromptTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

// providerData is the opaque state kept on a tool call.
type providerData struct {
	Google struct {
		ThoughtSignature string `json:"thought_signature,omitempty"`
	} `json:"google"`
}

func signatureOf(raw json.RawMessage) string {
	var d providerData
	if len(raw) == 0 || json.Unmarshal(raw, &d) != nil {
		return ""
	}
	return d.Google.ThoughtSignature
}

func withSignature(sig string) json.RawMessage {
	if sig == "" {
		return nil
	}
	var d providerData
	d.Google.ThoughtSignature = sig
	b, _ := json.Marshal(d)
	return b
}

// toolResponse wraps a tool result as the JSON object functionResponse
// requires.
func toolResponse(s string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"result": s})
	return b
}

func (p *Provider) buildRequest(req *llm.ChatRequest) *request {
	r := &request{}
	system := req.System
	// Tool results name the function they answer; earlier calls tell it
	// when the platform message does not.
	callNames := map[string]string{}
	add := func(role string, parts ...part) {
		if n := len(r.Contents); n > 0 && r.Contents[n-1].Role == role {
			r.Contents[n-1].Parts = append(r.Contents[n-1].Parts, parts...)
			return
		}
		r.Contents = append(r.Contents, content{Role: role, Parts: parts})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleSystem:
			system = strings.TrimSpace(system + "\n\n" + m.Content)
		case llm.RoleTool:
			name := m.Name
			if name == "" {
				name = callNames[m.ToolCallID]
			}
			add("user", part{FunctionResponse: &functionResponse{ID: m.ToolCallID, Name: llm.WireToolName(name), Response: toolResponse(m.Content)}})
		case llm.RoleAssistant:
			var parts []part
			if m.Content != "" {
				parts = append(parts, part{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				callNames[tc.ID] = tc.Name
				parts = append(parts, part{
					FunctionCall:     &functionCall{ID: tc.ID, Name: llm.WireToolName(tc.Name), Args: llm.NormalizeArgs(tc.Arguments)},
					ThoughtSignature: signatureOf(tc.ProviderData),
				})
			}
			if len(parts) == 0 {
				parts = []part{{Text: " "}}
			}
			add("model", parts...)
		default:
			text := m.Content
			if text == "" {
				text = " "
			}
			add("user", part{Text: text})
		}
	}
	if system != "" {
		r.SystemInstruction = &content{Parts: []part{{Text: system}}}
	}
	if len(req.Tools) > 0 {
		decls := make([]functionDecl, 0, len(req.Tools))
		for _, t := range req.Tools {
			decls = append(decls, functionDecl{Name: llm.WireToolName(t.Name), Description: t.Description, Parameters: llm.SchemaOrEmpty(t.Parameters)})
		}
		r.Tools = []toolDecl{{FunctionDeclarations: decls}}
	}
	gc := generationConfig{Temperature: req.Temperature, MaxOutputTokens: req.MaxTokens, StopSequences: req.Stop}
	if req.JSON {
		gc.ResponseMimeType = "application/json"
	}
	if gc.Temperature != nil || gc.MaxOutputTokens > 0 || len(gc.StopSequences) > 0 || gc.ResponseMimeType != "" {
		r.GenerationConfig = &gc
	}
	return r
}

func (p *Provider) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if err := llm.CheckContext(p.cfg.Name, p.ContextWindow(), req); err != nil {
		return nil, err
	}
	model := req.Model
	if model == "" {
		model = p.cfg.DefaultModel
	}
	if model == "" {
		return nil, fmt.Errorf("llm %s: no model: set the provider's default model or choose one for the agent", p.cfg.Name)
	}
	var out response
	if err := p.http.Do(ctx, http.MethodPost, p.chatPath(model), p.buildRequest(req), &out); err != nil {
		return nil, p.mapError(err, model)
	}
	if pf := out.PromptFeedback; pf != nil && pf.BlockReason != "" {
		return nil, p.blocked("the prompt", pf.BlockReason, pf.BlockReasonMessage)
	}
	if len(out.Candidates) == 0 {
		return nil, fmt.Errorf("llm %s: response has no candidates", p.cfg.Name)
	}
	cand := out.Candidates[0]
	msg := llm.ChatMessage{Role: llm.RoleAssistant}
	var texts []string
	for _, pt := range cand.Content.Parts {
		switch {
		case pt.FunctionCall != nil:
			id := pt.FunctionCall.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", len(msg.ToolCalls))
			}
			msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
				ID: id, Name: llm.PlatformToolName(pt.FunctionCall.Name),
				Arguments: llm.NormalizeArgs(pt.FunctionCall.Args), ProviderData: withSignature(pt.ThoughtSignature),
			})
		case pt.Thought:
			// Thought summaries are not part of the answer.
		case pt.Text != "":
			texts = append(texts, pt.Text)
		}
	}
	msg.Content = strings.Join(texts, "")
	stop := mapFinish(cand.FinishReason, len(msg.ToolCalls) > 0)
	if msg.Content == "" && len(msg.ToolCalls) == 0 {
		switch stop {
		case "content_filter":
			return nil, p.blocked("the answer", cand.FinishReason, cand.FinishMessage)
		case "malformed_function_call":
			return nil, fmt.Errorf("llm %s: %s produced a malformed function call; retry the turn, or use a model that handles these tools", p.cfg.Name, model)
		}
	}
	if out.ModelVersion != "" {
		model = out.ModelVersion
	}
	u := out.UsageMetadata
	return &llm.ChatResponse{
		Message: msg, StopReason: stop, Model: model,
		// promptTokenCount includes the cached part; thinking is billed as output.
		Usage: llm.Usage{
			InputTokens:     u.PromptTokenCount + u.ToolUsePromptTokenCount,
			OutputTokens:    u.CandidatesTokenCount + u.ThoughtsTokenCount,
			CacheReadTokens: u.CachedContentTokenCount,
		},
	}, nil
}

func mapFinish(r string, hasTools bool) string {
	switch r {
	case "STOP", "", "FINISH_REASON_UNSPECIFIED":
		if hasTools {
			return "tool_calls"
		}
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY", "LANGUAGE":
		return "content_filter"
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL":
		return "malformed_function_call"
	}
	if hasTools {
		return "tool_calls"
	}
	return strings.ToLower(r)
}

// Models lists the models that support generateContent (Gemini API) or the
// Gemini publisher models (Vertex AI).
func (p *Provider) Models(ctx context.Context) ([]llm.ModelInfo, error) {
	if p.vertex != nil {
		return p.vertexModels(ctx)
	}
	var res []llm.ModelInfo
	pageToken := ""
	for range maxModelPages {
		var out struct {
			Models []struct {
				Name                       string   `json:"name"`
				InputTokenLimit            int      `json:"inputTokenLimit"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		path := "/models?pageSize=1000"
		if pageToken != "" {
			path += "&pageToken=" + url.QueryEscape(pageToken)
		}
		if err := p.http.Do(ctx, http.MethodGet, path, nil, &out); err != nil {
			return nil, p.mapError(err, "")
		}
		for _, m := range out.Models {
			if len(m.SupportedGenerationMethods) > 0 && !slices.Contains(m.SupportedGenerationMethods, "generateContent") {
				continue
			}
			res = append(res, llm.ModelInfo{ID: modelID(m.Name), Provider: p.cfg.Name, ContextSize: m.InputTokenLimit})
		}
		if pageToken = out.NextPageToken; pageToken == "" {
			break
		}
	}
	return res, nil
}

// vertexModels lists Google's publisher models (a v1beta1 API) and keeps
// the Gemini ones; the list is best effort, any Gemini model ID the region
// serves can still be used.
func (p *Provider) vertexModels(ctx context.Context) ([]llm.ModelInfo, error) {
	var res []llm.ModelInfo
	pageToken := ""
	for range maxModelPages {
		var out struct {
			PublisherModels []struct {
				Name string `json:"name"`
			} `json:"publisherModels"`
			NextPageToken string `json:"nextPageToken"`
		}
		path := "/v1beta1/publishers/google/models?pageSize=200"
		if pageToken != "" {
			path += "&pageToken=" + url.QueryEscape(pageToken)
		}
		if err := p.http.Do(ctx, http.MethodGet, path, nil, &out); err != nil {
			return nil, p.mapError(err, "")
		}
		for _, m := range out.PublisherModels {
			id := m.Name[strings.LastIndex(m.Name, "/")+1:]
			if strings.HasPrefix(id, "gemini-") {
				res = append(res, llm.ModelInfo{ID: id, Provider: p.cfg.Name})
			}
		}
		if pageToken = out.NextPageToken; pageToken == "" {
			break
		}
	}
	return res, nil
}
