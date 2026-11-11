package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	_ "github.com/woliveiras/weft/server/internal/llm/providers"
)

func TestRegistryResolveAndChat(t *testing.T) {
	r, err := llm.NewRegistry([]llm.ProviderConfig{
		{Name: "offline", Type: "mock", DefaultModel: "mock-1", Concurrency: 1},
		{Name: "local", Type: "ollama", BaseURL: "http://127.0.0.1:1", DefaultModel: "llama3"},
	}, "offline")
	if err != nil {
		t.Fatal(err)
	}
	p, model, err := r.Resolve(domain.ModelRef{})
	if err != nil || p.Name() != "offline" || model != "mock-1" {
		t.Fatalf("default resolve: %v %q %v", p, model, err)
	}
	p, model, _ = r.Resolve(domain.ModelRef{Provider: "local", Name: "qwen"})
	if p.Name() != "local" || model != "qwen" {
		t.Fatalf("explicit resolve: %s %s", p.Name(), model)
	}
	if _, _, err := r.Resolve(domain.ModelRef{Provider: "nope"}); err == nil {
		t.Fatal("expected unknown provider error")
	}
	if _, _, err := r.Resolve(domain.ModelRef{Provider: "default"}); err != nil {
		t.Fatal("'default' alias should resolve")
	}
	resp, err := r.Chat(context.Background(), "", &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}})
	if err != nil || resp.Message.Content != "MOCK: hi" || resp.Model != "mock-1" {
		t.Fatalf("%+v %v", resp, err)
	}
	if l := r.List(); len(l) != 2 || !l[0].Default || l[1].Type != "ollama" {
		t.Fatalf("list %+v", l)
	}
}

func TestRegistryErrors(t *testing.T) {
	if _, err := llm.NewRegistry([]llm.ProviderConfig{{Name: "x", Type: "nope"}}, ""); err == nil {
		t.Fatal("unknown type")
	}
	if _, err := llm.NewRegistry([]llm.ProviderConfig{{Name: "x", Type: "mock"}}, "y"); err == nil {
		t.Fatal("missing default")
	}
	if _, err := llm.NewRegistry([]llm.ProviderConfig{{Name: "x", Type: "mock"}, {Name: "x", Type: "mock"}}, ""); err == nil {
		t.Fatal("duplicate")
	}
}

func TestNormalizeArgs(t *testing.T) {
	for in, want := range map[string]string{``: `{}`, `null`: `{}`, `"{\"a\":1}"`: `{"a":1}`, `{"b":2}`: `{"b":2}`, `""`: `{}`} {
		if got := string(llm.NormalizeArgs([]byte(in))); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
}

func TestRegistryReplace(t *testing.T) {
	r, err := llm.NewRegistry([]llm.ProviderConfig{{Name: "a", Type: "mock", DefaultModel: "m1"}}, "a")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Replace([]llm.ProviderConfig{{Name: "a", Type: "mock"}, {Name: "b", Type: "nope"}}, "a"); err == nil {
		t.Fatal("a provider that fails to build must fail the whole replace")
	}
	if err := r.Replace([]llm.ProviderConfig{{Name: "a", Type: "mock"}}, "ghost"); err == nil {
		t.Fatal("unknown default must fail")
	}
	if names := r.Names(); len(names) != 1 || names[0] != "a" || r.DefaultName() != "a" {
		t.Fatalf("failed replace must keep the current set: %v %q", names, r.DefaultName())
	}
	if err := r.Replace([]llm.ProviderConfig{{Name: "a", Type: "mock"}, {Name: "b", Type: "mock", DefaultModel: "m2"}}, "b"); err != nil {
		t.Fatal(err)
	}
	p, model, err := r.Resolve(domain.ModelRef{})
	if err != nil || p.Name() != "b" || model != "m2" {
		t.Fatalf("after replace: %v %q %v", p, model, err)
	}
	if err := r.Replace(nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Resolve(domain.ModelRef{}); err == nil {
		t.Fatal("empty registry must not resolve")
	}
}

func TestAPIKeyValueIsPreferredAndNeverMarshalled(t *testing.T) {
	t.Setenv("WEFT_TEST_KEY", "from-env")
	c := llm.ProviderConfig{Name: "x", Type: "openai", APIKeyEnv: "WEFT_TEST_KEY", APIKeyValue: "from-console"}
	if k, err := c.APIKey(); err != nil || k != "from-console" {
		t.Fatalf("APIKey = %q %v", k, err)
	}
	b, _ := json.Marshal(c)
	y, _ := yaml.Marshal(c)
	if strings.Contains(string(b), "from-console") || strings.Contains(string(y), "from-console") {
		t.Fatalf("in-memory key leaked: %s %s", b, y)
	}
}

func TestUnavailableProviderFailsExplicitly(t *testing.T) {
	r, err := llm.NewRegistry([]llm.ProviderConfig{{Name: "ok", Type: "mock"}}, "ok")
	if err != nil {
		t.Fatal(err)
	}
	r.Add(llm.ProviderConfig{Name: "broken", Type: "openai"}, llm.Unavailable("broken", errors.New("credential cannot be decrypted")))
	if _, _, err := r.Resolve(domain.ModelRef{Provider: "broken"}); !errors.Is(err, llm.ErrUnavailable) || !strings.Contains(err.Error(), "decrypted") {
		t.Fatalf("resolve unavailable: %v", err)
	}
	if _, err := r.Chat(context.Background(), "broken", &llm.ChatRequest{}); !errors.Is(err, llm.ErrUnavailable) {
		t.Fatalf("chat unavailable: %v", err)
	}
	if _, _, err := r.Resolve(domain.ModelRef{}); err != nil {
		t.Fatalf("the default must not be affected: %v", err)
	}
}

type scripted struct {
	resp *llm.ChatResponse
	err  error
}

func (s scripted) Name() string { return "s" }
func (s scripted) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return s.resp, s.err
}
func (s scripted) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }

func TestGatewayReportsEveryDispatchedCall(t *testing.T) {
	var calls []llm.Call
	observe := func(_ context.Context, c llm.Call) { calls = append(calls, c) }
	req := func(max int) *llm.ChatRequest {
		return &llm.ChatRequest{MaxTokens: max, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: strings.Repeat("x", 400)}},
			Metadata: map[string]string{"execution": "x1"}}
	}
	for _, tc := range []struct {
		name      string
		p         llm.Provider
		max       int
		want      *llm.Usage
		estimated bool
	}{
		{"reported", scripted{resp: &llm.ChatResponse{Usage: llm.Usage{InputTokens: 9, OutputTokens: 2, CacheReadTokens: 4}}}, 0, &llm.Usage{InputTokens: 9, OutputTokens: 2, CacheReadTokens: 4}, false},
		{"zero usage is estimated", scripted{resp: &llm.ChatResponse{}}, 50, &llm.Usage{InputTokens: 102, OutputTokens: 50}, true},
		{"server failure is estimated", scripted{err: &llm.APIError{Status: 503}}, 0, &llm.Usage{InputTokens: 102, OutputTokens: llm.DefaultEstimatedOutput}, true},
		{"timeout is estimated", scripted{err: context.DeadlineExceeded}, 10, &llm.Usage{InputTokens: 102, OutputTokens: 10}, true},
		{"rate limit is estimated", scripted{err: &llm.APIError{Status: 429}}, 10, &llm.Usage{InputTokens: 102, OutputTokens: 10}, true},
		{"rejected request is not charged", scripted{err: &llm.APIError{Status: 401}}, 10, nil, false},
		{"unavailable provider is never dispatched", llm.Unavailable("u", errors.New("no key")), 10, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls = nil
			r, _ := llm.NewRegistry(nil, "")
			r.Add(llm.ProviderConfig{Name: "p", Type: "openai", DefaultModel: "m"}, tc.p)
			r.SetObserver(observe)
			_, _ = r.Chat(context.Background(), "p", req(tc.max))
			if tc.want == nil {
				if len(calls) != 0 {
					t.Fatalf("calls = %+v", calls)
				}
				return
			}
			if len(calls) != 1 {
				t.Fatalf("calls = %+v", calls)
			}
			c := calls[0]
			if c.Source != "gateway" || c.Provider != "p" || c.Type != "openai" || c.Model != "m" || c.Metadata["execution"] != "x1" ||
				c.Usage != *tc.want || c.Estimated != tc.estimated {
				t.Fatalf("call = %+v, want usage %+v estimated %v", c, *tc.want, tc.estimated)
			}
		})
	}
	calls = nil
	r, _ := llm.NewRegistry(nil, "")
	r.ReportExternal(context.Background(), llm.Call{Usage: llm.Usage{InputTokens: 1}}) // no observer: no panic
	r.SetObserver(observe)
	r.ReportExternal(context.Background(), llm.Call{Source: "gateway", Usage: llm.Usage{InputTokens: 1}})
	if len(calls) != 1 || calls[0].Source != "external" {
		t.Fatalf("external = %+v", calls)
	}
}
