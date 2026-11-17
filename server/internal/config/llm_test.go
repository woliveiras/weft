package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProvidersDoNotComeFromEnvironment(t *testing.T) {
	for k, v := range map[string]string{
		"OLLAMA_BASE_URL": "http://ollama:11434", "OPENAI_API_KEY": "sk-x", "ANTHROPIC_API_KEY": "sk-y",
		"OPENROUTER_API_KEY": "sk-z", "VLLM_BASE_URL": "http://vllm:8000/v1", "HF_BASE_URL": "http://tgi:8081/v1",
		"WEFT_DEFAULT_PROVIDER": "ollama",
	} {
		t.Setenv(k, v)
	}
	c := Default()
	applyEnv(c)
	if p, def := c.LLM.Static(); len(p) != 0 || def != "" {
		t.Fatalf("static providers = %+v default %q", p, def)
	}
}

func TestDevMockOffersOnlyTheMockProvider(t *testing.T) {
	c := Default()
	t.Setenv("WEFT_DEV_MOCK", "true")
	applyEnv(c)
	p, def := c.LLM.Static()
	if !c.LLM.DevMock || len(p) != 1 || p[0].Name != "mock" || p[0].Type != "mock" || def != "mock" {
		t.Fatalf("dev mock = %+v default %q", p, def)
	}
	t.Setenv("WEFT_DEV_MOCK", "false")
	applyEnv(c)
	if p, _ := c.LLM.Static(); len(p) != 0 {
		t.Fatalf("disabled dev mock = %+v", p)
	}
}

func TestLegacyProviderConfigurationIsRejected(t *testing.T) {
	t.Setenv("WEFT_ADMIN_TOKEN", "t")
	dir := t.TempDir()
	for key, body := range map[string]string{
		"providers": "llm:\n  providers:\n    - {name: ollama, type: ollama}\n",
		"default":   "llm:\n  default: ollama\n",
	} {
		path := filepath.Join(dir, key+".yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "llm."+key+" was removed") {
			t.Fatalf("%s: err = %v", key, err)
		}
	}
	path := filepath.Join(dir, "dev.yaml")
	if err := os.WriteFile(path, []byte("llm:\n  devMock: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || !c.LLM.DevMock {
		t.Fatalf("devMock config: %+v %v", c, err)
	}
}
