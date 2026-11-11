// Package huggingface adapts locally served HuggingFace models. Text
// Generation Inference (TGI), HuggingFace's own server, exposes an
// OpenAI-compatible Messages API at /v1/chat/completions, so this is the
// openai client pointed at it. The same type works for HF Inference
// Endpoints (set apiKeyEnv to the HF token) and for any other local server
// that loads HF weights and speaks the OpenAI protocol (vLLM, llama.cpp,
// a transformers-based server).
//
// The compose stack runs TGI (or any OpenAI-compatible local server) behind
// the "huggingface" provider; the default base URL matches its port.
//
// TGI rejects response_format json_object on some versions (it expects a
// grammar), so JSON mode is expressed as an instruction instead.
package huggingface

import (
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/llm/openai"
)

const defaultBaseURL = "http://localhost:8081/v1"

func factory(c llm.ProviderConfig) (llm.Provider, error) {
	if c.DefaultModel == "" {
		c.DefaultModel = "tgi" // TGI serves one model and ignores the name
	}
	return openai.New(c, openai.Options{DefaultBaseURL: defaultBaseURL, NoJSONMode: true})
}

func init() {
	llm.RegisterPlugin(llm.Manifest{
		ID: "huggingface", Label: "Hugging Face TGI", Description: "One model served by Text Generation Inference (OpenAI-compatible API).",
		Local: true, DefaultBaseURL: defaultBaseURL, Key: llm.KeyOptional,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONInstruction, Usage: true, ModelListing: true}, Settings: []string{llm.SettingContextWindow},
	}, factory)
	llm.RegisterPlugin(llm.Manifest{
		ID: "tgi", Label: "Text Generation Inference", Description: "One model served by Text Generation Inference (OpenAI-compatible API).",
		Local: true, DefaultBaseURL: defaultBaseURL, Key: llm.KeyOptional,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONInstruction, Usage: true, ModelListing: true}, Settings: []string{llm.SettingContextWindow},
	}, factory)
}
