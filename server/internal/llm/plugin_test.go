package llm_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
	_ "github.com/woliveiras/weft/server/internal/llm/providers"
)

func TestBuiltinPluginsHaveManifests(t *testing.T) {
	want := []string{"anthropic", "gemini", "huggingface", "lmstudio", "mock", "ollama", "openai", "openrouter", "tgi", "vllm"}
	got := llm.Plugins()
	if len(got) != len(want) {
		t.Fatalf("plugins = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, m := range got {
		if m.ID != want[i] || m.Label == "" || !m.Builtin || m.Key == "" || m.Capabilities.JSON == "" {
			t.Fatalf("manifest %d = %+v, want complete %q", i, m, want[i])
		}
	}
	ollama, ok := llm.PluginManifest("OLLAMA")
	if !ok || !ollama.Local || ollama.Key != llm.KeyNone || ollama.DefaultBaseURL != "http://localhost:11434" ||
		!ollama.Capabilities.Tools || ollama.Capabilities.Streaming || len(ollama.Settings) != 1 || ollama.Settings[0] != llm.SettingContextWindow {
		t.Fatalf("ollama manifest = %+v ok=%v", ollama, ok)
	}
	if openai, _ := llm.PluginManifest("openai"); openai.Local || openai.Key != llm.KeyRequired {
		t.Fatalf("openai manifest = %+v", openai)
	}
	if mock, _ := llm.PluginManifest("mock"); !mock.DevOnly {
		t.Fatalf("mock must be a development plugin: %+v", mock)
	}
}

// The built-in model plugins honour a configured context window: each
// declares the setting and reports the window to native turns.
func TestBuiltinPluginsReportAConfiguredContextWindow(t *testing.T) {
	for _, id := range []string{"anthropic", "gemini", "huggingface", "lmstudio", "ollama", "openai", "openrouter", "tgi", "vllm"} {
		m, ok := llm.PluginManifest(id)
		if !ok || !slices.Contains(m.Settings, llm.SettingContextWindow) {
			t.Fatalf("%s settings = %v (ok=%v)", id, m.Settings, ok)
		}
		p, err := llm.New(llm.ProviderConfig{Name: id, Type: id, BaseURL: "http://127.0.0.1:1", APIKeyValue: "k", ContextWindow: 4096})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if got := llm.ContextWindowOf(p); got != 4096 {
			t.Fatalf("%s window = %d, want 4096", id, got)
		}
	}
}

func TestNewRejectsUnknownPlugin(t *testing.T) {
	if _, ok := llm.PluginManifest("nope"); ok {
		t.Fatal("unknown plugin has a manifest")
	}
	if _, err := llm.New(llm.ProviderConfig{Name: "x", Type: "nope"}); err == nil || !strings.Contains(err.Error(), "unknown provider type nope") {
		t.Fatalf("err = %v", err)
	}
}

func TestRegisterPluginRejectsIncompleteAndDuplicate(t *testing.T) {
	for name, fn := range map[string]func(){
		"no factory": func() { llm.RegisterPlugin(llm.Manifest{ID: "x-test", Label: "X"}, nil) },
		"no label": func() {
			llm.RegisterPlugin(llm.Manifest{ID: "x-test"}, func(llm.ProviderConfig) (llm.Provider, error) { return nil, nil })
		},
		"duplicate": func() {
			llm.RegisterPlugin(llm.Manifest{ID: "Ollama", Label: "Again"}, func(llm.ProviderConfig) (llm.Provider, error) { return nil, nil })
		},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s: RegisterPlugin did not panic", name)
				}
			}()
			fn()
		}()
	}
}
