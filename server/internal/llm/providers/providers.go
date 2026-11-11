// Package providers registers every built-in LLM provider type. Import it
// for side effects:
//
//	import _ "github.com/woliveiras/weft/server/internal/llm/providers"
//
// Registered types: openai, vllm, lmstudio, anthropic, ollama, openrouter,
// huggingface, tgi, mock.
//
// Adding a backend: implement llm.Provider in a new package, call
// llm.RegisterPlugin with its manifest in its init(), and add a blank import here.
package providers

import (
	_ "github.com/woliveiras/weft/server/internal/llm/anthropic"
	_ "github.com/woliveiras/weft/server/internal/llm/huggingface"
	_ "github.com/woliveiras/weft/server/internal/llm/mock"
	_ "github.com/woliveiras/weft/server/internal/llm/ollama"
	_ "github.com/woliveiras/weft/server/internal/llm/openai"
	_ "github.com/woliveiras/weft/server/internal/llm/openrouter"
)
