package conformance_test

import (
	"os"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/llm/conformance"
	_ "github.com/woliveiras/weft/server/internal/llm/providers"
)

// wires maps every built-in HTTP plugin to its protocol. A new plugin must
// be added here, so it cannot ship without passing the suite.
var wires = map[string]conformance.Wire{
	"openai": conformance.OpenAI, "vllm": conformance.OpenAI, "lmstudio": conformance.OpenAI,
	"openrouter": conformance.OpenAI, "huggingface": conformance.OpenAI, "tgi": conformance.OpenAI,
	"anthropic": conformance.Anthropic,
	"gemini":    conformance.Gemini,
	"ollama":    conformance.Ollama,
}

func TestBuiltinPluginsConform(t *testing.T) {
	for _, m := range llm.Plugins() {
		if m.ID == "mock" {
			continue // no wire protocol; covered by its own tests
		}
		w, ok := wires[m.ID]
		if !ok {
			t.Errorf("plugin %q has no conformance wire", m.ID)
			continue
		}
		t.Run(m.ID, func(t *testing.T) {
			t.Parallel()
			conformance.Run(t, w, func(c llm.ProviderConfig) (llm.Provider, error) {
				c.Type = m.ID
				return llm.New(c)
			})
		})
	}
}

// TestOllamaLive runs the live subset against a real Ollama when
// WEFT_TEST_OLLAMA_URL is set (model: WEFT_TEST_OLLAMA_MODEL, default
// llama3.2). It is skipped otherwise, so CI stays deterministic.
func TestOllamaLive(t *testing.T) {
	url := os.Getenv("WEFT_TEST_OLLAMA_URL")
	if url == "" {
		t.Skip("set WEFT_TEST_OLLAMA_URL to run against a real Ollama")
	}
	model := os.Getenv("WEFT_TEST_OLLAMA_MODEL")
	if model == "" {
		model = "llama3.2:latest"
	}
	p, err := llm.New(llm.ProviderConfig{Name: "live-ollama", Type: "ollama", BaseURL: url, DefaultModel: model, TimeoutSec: 600})
	if err != nil {
		t.Fatal(err)
	}
	conformance.RunLive(t, p, model)
}

// TestOpenAICompatibleLive runs the live subset against a hosted
// OpenAI-compatible endpoint (e.g. Gemini's) when WEFT_TEST_OPENAI_URL is
// set, with WEFT_TEST_OPENAI_MODEL and the key in WEFT_TEST_OPENAI_KEY. It
// is skipped otherwise; the key never appears in output.
func TestOpenAICompatibleLive(t *testing.T) {
	url, model := os.Getenv("WEFT_TEST_OPENAI_URL"), os.Getenv("WEFT_TEST_OPENAI_MODEL")
	if url == "" || model == "" {
		t.Skip("set WEFT_TEST_OPENAI_URL, WEFT_TEST_OPENAI_MODEL and WEFT_TEST_OPENAI_KEY to run against a hosted endpoint")
	}
	p, err := llm.New(llm.ProviderConfig{Name: "live-openai", Type: "openai", BaseURL: url, DefaultModel: model, TimeoutSec: 120, MaxRetries: 3,
		APIKeyValue: os.Getenv("WEFT_TEST_OPENAI_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	conformance.RunLive(t, p, model)
}

// TestGeminiLive runs the live subset against the Gemini API with the
// native plugin when WEFT_TEST_GEMINI_KEY is set (model:
// WEFT_TEST_GEMINI_MODEL, default gemini-3.5-flash). It is skipped
// otherwise; the key never appears in output.
func TestGeminiLive(t *testing.T) {
	key := os.Getenv("WEFT_TEST_GEMINI_KEY")
	if key == "" {
		t.Skip("set WEFT_TEST_GEMINI_KEY (and optionally WEFT_TEST_GEMINI_MODEL) to run against the Gemini API")
	}
	model := os.Getenv("WEFT_TEST_GEMINI_MODEL")
	if model == "" {
		model = "gemini-3.5-flash"
	}
	p, err := llm.New(llm.ProviderConfig{Name: "live-gemini", Type: "gemini", DefaultModel: model, TimeoutSec: 120, MaxRetries: 3, APIKeyValue: key})
	if err != nil {
		t.Fatal(err)
	}
	conformance.RunLive(t, p, model)
}
