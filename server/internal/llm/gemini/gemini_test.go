package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/llm"
)

const testKey = "AIza-test-key-0123456789"

// fixture is a scripted Gemini endpoint that records what it receives.
type fixture struct {
	*httptest.Server
	mu       sync.Mutex
	reqs     []*http.Request
	bodies   []map[string]any
	raw      []string
	statuses []int
	answers  []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		i := len(f.reqs)
		f.reqs, f.bodies, f.raw = append(f.reqs, r), append(f.bodies, body), append(f.raw, string(raw))
		status, answer := http.StatusOK, `{"candidates":[{"content":{"role":"model","parts":[{"text":"OK"}]},"finishReason":"STOP"}]}`
		if n := len(f.answers); n > 0 {
			answer = f.answers[min(i, n-1)]
		}
		if n := len(f.statuses); n > 0 {
			status = f.statuses[min(i, n-1)]
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fixture) script(status int, answers ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses, f.answers = []int{status}, answers
}

func newAPIProvider(t *testing.T, f *fixture, cfg llm.ProviderConfig) *Provider {
	t.Helper()
	cfg.Name, cfg.BaseURL, cfg.APIKeyValue = "gem", f.URL+"/v1beta", testKey
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = -1
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p.HTTP().BaseDelay = time.Millisecond
	return p
}

func TestRequestShape(t *testing.T) {
	f := newFixture(t)
	p := newAPIProvider(t, f, llm.ProviderConfig{DefaultModel: "models/gemini-test"})
	temp := 0.2
	_, err := p.Chat(context.Background(), &llm.ChatRequest{
		System: "be brief", Temperature: &temp, MaxTokens: 64, Stop: []string{"END"}, JSON: true,
		Messages: []llm.ChatMessage{
			{Role: llm.RoleUser, Content: "list files"},
			{Role: llm.RoleSystem, Content: "late rule"},
			{Role: llm.RoleAssistant, Content: "Looking.", ToolCalls: []llm.ToolCall{
				{ID: "c1", Name: "filesystem.list", Arguments: json.RawMessage(`{"path":"."}`)},
				{ID: "c2", Name: "filesystem.read", Arguments: json.RawMessage(`"{\"path\":\"a\"}"`)},
			}},
			{Role: llm.RoleTool, ToolCallID: "c1", Name: "filesystem.list", Content: "a b"},
			{Role: llm.RoleTool, ToolCallID: "c2", Content: "text of a"},
		},
		Tools: []llm.ToolSchema{{Name: "filesystem.list", Description: "List", Parameters: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"path":{"type":"string"}}}`)},
			{Name: "filesystem.read"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := f.reqs[0]
	if r.URL.Path != "/v1beta/models/gemini-test:generateContent" {
		t.Fatalf("path = %s", r.URL.Path)
	}
	if r.Header.Get("x-goog-api-key") != testKey || r.Header.Get("Authorization") != "" || strings.Contains(f.raw[0], testKey) || r.URL.RawQuery != "" {
		t.Fatalf("credential must travel only in x-goog-api-key: headers %v, query %q", r.Header, r.URL.RawQuery)
	}
	var got struct {
		Contents          []content `json:"contents"`
		SystemInstruction content   `json:"systemInstruction"`
		Tools             []struct {
			FunctionDeclarations []struct {
				Name   string          `json:"name"`
				Schema json.RawMessage `json:"parametersJsonSchema"`
			} `json:"functionDeclarations"`
		} `json:"tools"`
		GenerationConfig generationConfig `json:"generationConfig"`
	}
	if err := json.Unmarshal([]byte(f.raw[0]), &got); err != nil {
		t.Fatal(err)
	}
	if s := got.SystemInstruction.Parts[0].Text; s != "be brief\n\nlate rule" {
		t.Fatalf("system = %q", s)
	}
	var roles []string
	for _, c := range got.Contents {
		roles = append(roles, c.Role)
	}
	if strings.Join(roles, ",") != "user,model,user" {
		t.Fatalf("roles = %v", roles)
	}
	model := got.Contents[1].Parts
	if len(model) != 3 || model[0].Text != "Looking." || model[1].FunctionCall.Name != "filesystem__list" || model[1].FunctionCall.ID != "c1" ||
		string(model[2].FunctionCall.Args) != `{"path":"a"}` {
		t.Fatalf("model turn = %+v", model)
	}
	results := got.Contents[2].Parts
	if len(results) != 2 || results[0].FunctionResponse.ID != "c1" || string(results[0].FunctionResponse.Response) != `{"result":"a b"}` ||
		results[1].FunctionResponse.Name != "filesystem__read" {
		t.Fatalf("tool results = %+v", results)
	}
	decls := got.Tools[0].FunctionDeclarations
	if len(decls) != 2 || !strings.Contains(string(decls[0].Schema), "additionalProperties") || string(decls[1].Schema) != string(llm.EmptySchema) {
		t.Fatalf("tools = %s", f.raw[0])
	}
	gc := got.GenerationConfig
	if gc.Temperature == nil || *gc.Temperature != 0.2 || gc.MaxOutputTokens != 64 || gc.StopSequences[0] != "END" || gc.ResponseMimeType != "application/json" {
		t.Fatalf("generationConfig = %+v", gc)
	}

	// A plain request carries no generation config and no JSON mode.
	if _, err := p.Chat(context.Background(), &llm.ChatRequest{Model: "gemini-other", Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	if f.reqs[1].URL.Path != "/v1beta/models/gemini-other:generateContent" || strings.Contains(f.raw[1], "generationConfig") || strings.Contains(f.raw[1], "systemInstruction") {
		t.Fatalf("plain request = %s %s", f.reqs[1].URL.Path, f.raw[1])
	}
}

func TestThoughtSignatureRoundTrip(t *testing.T) {
	f := newFixture(t)
	p := newAPIProvider(t, f, llm.ProviderConfig{DefaultModel: "gemini-test"})
	f.script(http.StatusOK, `{"candidates":[{"content":{"role":"model","parts":[
		{"text":"thinking out loud","thought":true},
		{"functionCall":{"name":"fixture__echo","args":{"word":"a"}},"thoughtSignature":"U0lHLTE="},
		{"functionCall":{"name":"fixture__echo","args":{"word":"b"}}}]},"finishReason":"STOP"}]}`)
	user := []llm.ChatMessage{{Role: llm.RoleUser, Content: "echo twice"}}
	resp, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: user})
	if err != nil {
		t.Fatal(err)
	}
	calls := resp.Message.ToolCalls
	if resp.StopReason != "tool_calls" || resp.Message.Content != "" || len(calls) != 2 {
		t.Fatalf("response = %+v", resp)
	}
	if calls[0].ID != "call_0" || calls[1].ID != "call_1" || calls[0].Name != "fixture.echo" {
		t.Fatalf("calls = %+v", calls)
	}
	if string(calls[0].ProviderData) != `{"google":{"thought_signature":"U0lHLTE="}}` || calls[1].ProviderData != nil {
		t.Fatalf("provider data = %s / %s", calls[0].ProviderData, calls[1].ProviderData)
	}

	msgs := append(user, resp.Message,
		llm.ChatMessage{Role: llm.RoleTool, ToolCallID: calls[0].ID, Name: calls[0].Name, Content: "a"},
		llm.ChatMessage{Role: llm.RoleTool, ToolCallID: calls[1].ID, Name: calls[1].Name, Content: "b"})
	if _, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: msgs}); err != nil {
		t.Fatal(err)
	}
	var sent request
	if err := json.Unmarshal([]byte(f.raw[1]), &sent); err != nil {
		t.Fatal(err)
	}
	model := sent.Contents[1]
	if model.Role != "model" || len(model.Parts) != 2 || model.Parts[0].ThoughtSignature != "U0lHLTE=" || model.Parts[1].ThoughtSignature != "" {
		t.Fatalf("signatures not sent back: %s", f.raw[1])
	}
	if strings.Contains(f.raw[1], "thinking out loud") {
		t.Fatal("thought summary was sent back as answer text")
	}
	if sent.Contents[2].Parts[1].FunctionResponse.ID != "call_1" {
		t.Fatalf("tool results = %+v", sent.Contents[2])
	}
}

func TestUsageCountsThinkingAndCache(t *testing.T) {
	f := newFixture(t)
	p := newAPIProvider(t, f, llm.ProviderConfig{DefaultModel: "gemini-test"})
	f.script(http.StatusOK, `{"modelVersion":"gemini-test-001","candidates":[{"content":{"parts":[{"text":"Hi"},{"text":" there"}]},"finishReason":"MAX_TOKENS"}],
		"usageMetadata":{"promptTokenCount":100,"toolUsePromptTokenCount":5,"candidatesTokenCount":10,"thoughtsTokenCount":40,"cachedContentTokenCount":60,"totalTokenCount":155}}`)
	resp, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := llm.Usage{InputTokens: 105, OutputTokens: 50, CacheReadTokens: 60}
	if resp.Usage != want || resp.Message.Content != "Hi there" || resp.StopReason != "length" || resp.Model != "gemini-test-001" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestErrorsAreActionable(t *testing.T) {
	googleErr := func(code int, status, reason string) string {
		details := ""
		if reason != "" {
			details = `,"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"` + reason + `"}]`
		}
		return `{"error":{"code":` + itoa(code) + `,"message":"upstream says no for ` + testKey + `","status":"` + status + `"` + details + `}}`
	}
	cases := []struct {
		name    string
		status  int
		body    string
		kind    error
		hint    string
		retries int
	}{
		{"quota", 429, googleErr(429, "RESOURCE_EXHAUSTED", ""), ErrQuota, "quota or rate limit", 2},
		{"invalid key", 400, googleErr(400, "INVALID_ARGUMENT", "API_KEY_INVALID"), ErrPermission, "API key was rejected", 1},
		{"permission", 403, googleErr(403, "PERMISSION_DENIED", ""), ErrPermission, "permission denied", 1},
		{"model not found", 404, googleErr(404, "NOT_FOUND", ""), ErrModelNotFound, `model "gemini-missing" was not found`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			p := newAPIProvider(t, f, llm.ProviderConfig{DefaultModel: "models/gemini-missing", MaxRetries: 1})
			f.script(tc.status, tc.body)
			_, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}})
			var apiErr *llm.APIError
			if !errors.Is(err, tc.kind) || !errors.As(err, &apiErr) || apiErr.Status != tc.status || !strings.Contains(err.Error(), tc.hint) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), testKey) || !strings.Contains(err.Error(), "upstream says no") {
				t.Fatalf("Google's message must be kept, the key redacted: %v", err)
			}
			if len(f.reqs) != tc.retries {
				t.Fatalf("%d requests, want %d", len(f.reqs), tc.retries)
			}
		})
	}

	t.Run("server error passes through", func(t *testing.T) {
		f := newFixture(t)
		p := newAPIProvider(t, f, llm.ProviderConfig{DefaultModel: "gemini-test"})
		f.script(http.StatusInternalServerError, googleErr(500, "INTERNAL", ""))
		_, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}})
		var ge *Error
		var apiErr *llm.APIError
		if errors.As(err, &ge) || !errors.As(err, &apiErr) || apiErr.Status != 500 {
			t.Fatalf("err = %v", err)
		}
	})

	for name, body := range map[string]string{
		"prompt blocked": `{"promptFeedback":{"blockReason":"SAFETY"}}`,
		"answer blocked": `{"candidates":[{"content":{"parts":[]},"finishReason":"PROHIBITED_CONTENT"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			p := newAPIProvider(t, f, llm.ProviderConfig{DefaultModel: "gemini-test"})
			f.script(http.StatusOK, body)
			_, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}})
			if !errors.Is(err, ErrSafetyBlocked) || !strings.Contains(err.Error(), "rephrase") {
				t.Fatalf("err = %v", err)
			}
		})
	}

	t.Run("partial answer cut by a filter is kept", func(t *testing.T) {
		f := newFixture(t)
		p := newAPIProvider(t, f, llm.ProviderConfig{DefaultModel: "gemini-test"})
		f.script(http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"partial"}]},"finishReason":"RECITATION"}]}`)
		resp, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}})
		if err != nil || resp.Message.Content != "partial" || resp.StopReason != "content_filter" {
			t.Fatalf("response = %+v, %v", resp, err)
		}
	})

	t.Run("malformed function call", func(t *testing.T) {
		f := newFixture(t)
		p := newAPIProvider(t, f, llm.ProviderConfig{DefaultModel: "gemini-test"})
		f.script(http.StatusOK, `{"candidates":[{"content":{"parts":[]},"finishReason":"MALFORMED_FUNCTION_CALL"}]}`)
		_, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}})
		if err == nil || !strings.Contains(err.Error(), "malformed function call") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("no candidates", func(t *testing.T) {
		f := newFixture(t)
		p := newAPIProvider(t, f, llm.ProviderConfig{DefaultModel: "gemini-test"})
		f.script(http.StatusOK, `{}`)
		if _, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}}); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestModelsListing(t *testing.T) {
	f := newFixture(t)
	p := newAPIProvider(t, f, llm.ProviderConfig{})
	f.script(http.StatusOK,
		`{"models":[{"name":"models/gemini-a","inputTokenLimit":1000,"outputTokenLimit":24,"supportedGenerationMethods":["generateContent","countTokens"]},
			{"name":"models/text-embedding","supportedGenerationMethods":["embedContent"]}],"nextPageToken":"page 2"}`,
		`{"models":[{"name":"models/gemini-b"}]}`)
	ms, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0] != (llm.ModelInfo{ID: "gemini-a", Provider: "gem", ContextSize: 1000}) || ms[1].ID != "gemini-b" {
		t.Fatalf("models = %+v", ms)
	}
	if f.reqs[0].URL.Path != "/v1beta/models" || f.reqs[1].URL.Query().Get("pageToken") != "page 2" {
		t.Fatalf("listing requests = %s, %s", f.reqs[0].URL, f.reqs[1].URL)
	}

	f.script(http.StatusForbidden, `{"error":{"code":403,"message":"denied","status":"PERMISSION_DENIED"}}`)
	if _, err := p.Models(context.Background()); !errors.Is(err, ErrPermission) {
		t.Fatalf("err = %v", err)
	}
}

func TestContextWindow(t *testing.T) {
	f := newFixture(t)
	p := newAPIProvider(t, f, llm.ProviderConfig{DefaultModel: "gemini-test"})
	if llm.ContextWindowOf(p) != 0 {
		t.Fatal("an unconfigured window must be reported as unknown")
	}
	p = newAPIProvider(t, f, llm.ProviderConfig{DefaultModel: "gemini-test", ContextWindow: 1024})
	if llm.ContextWindowOf(p) != 1024 {
		t.Fatalf("window = %d", llm.ContextWindowOf(p))
	}
	big := strings.Repeat("word ", 2000)
	_, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: big}}})
	if !errors.Is(err, llm.ErrContextExceeded) || len(f.reqs) != 0 {
		t.Fatalf("err = %v after %d requests", err, len(f.reqs))
	}
}

func TestConfigErrors(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", writeUserCredentials(t))
	for _, tc := range []struct {
		name string
		cfg  llm.ProviderConfig
		want string
	}{
		{"no key", llm.ProviderConfig{}, "API key is required"},
		{"region without project", llm.ProviderConfig{APIKeyValue: testKey, Region: "us-central1"}, "region applies to Vertex AI only"},
		{"key with project", llm.ProviderConfig{APIKeyValue: testKey, Project: "my-project"}, "not an API key"},
		{"bad project", llm.ProviderConfig{Project: "My_Project"}, "not a Google Cloud project ID"},
		{"bad region", llm.ProviderConfig{Project: "my-project", Region: "evil.example.com/x"}, "not a Google Cloud region"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.Name = "gem"
			_, err := New(tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), testKey) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if _, err := llm.New(llm.ProviderConfig{Name: "gem", Type: "gemini", APIKeyValue: testKey}); err != nil {
		t.Fatalf("registered plugin: %v", err)
	}
	m, ok := llm.PluginManifest("gemini")
	if !ok || m.Key != llm.KeyOptional || m.Capabilities.JSON != llm.JSONNative || len(m.Settings) != 3 {
		t.Fatalf("manifest = %+v", m)
	}
}
