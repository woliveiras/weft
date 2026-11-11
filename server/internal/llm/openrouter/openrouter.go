// Package openrouter adapts OpenRouter, a hosted router across many model
// vendors. It speaks the OpenAI protocol, so this is a configuration of the
// openai client: a default base URL, a required key, and the attribution
// headers OpenRouter uses for rankings and rate-limit tiers. Model ids are
// vendor-qualified ("anthropic/claude-...", "meta-llama/...").
package openrouter

import (
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/llm/openai"
)

const defaultBaseURL = "https://openrouter.ai/api/v1"

func init() {
	llm.RegisterPlugin(llm.Manifest{
		ID: "openrouter", Label: "OpenRouter", Description: "Hosted models from many vendors through OpenRouter.",
		DefaultBaseURL: defaultBaseURL, Key: llm.KeyRequired,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONNative, Usage: true, ModelListing: true},
	}, func(c llm.ProviderConfig) (llm.Provider, error) {
		return openai.New(c, openai.Options{
			DefaultBaseURL: defaultBaseURL,
			RequireKey:     true,
			ExtraHeaders: map[string]string{
				"HTTP-Referer": "https://github.com/woliveiras/weft",
				"X-Title":      "Weft",
			},
		})
	})
}
