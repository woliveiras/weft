package api

import "testing"

func TestValidateChatModelRequiresAnExplicitProviderAndModel(t *testing.T) {
	for _, cases := range []struct {
		model chatModelSelection
		valid bool
	}{
		{chatModelSelection{Provider: "team-ollama", Name: "qwen:7b"}, true},
		{chatModelSelection{Provider: "huggingface", Name: "team/catalog-model"}, true},
		{chatModelSelection{Provider: "team-ollama"}, false},
		{chatModelSelection{Name: "qwen:7b"}, false},
		{chatModelSelection{Provider: "team-ollama", Name: "https://other.example"}, false},
		{chatModelSelection{Provider: "team-ollama", Name: "two identifiers"}, false},
		{chatModelSelection{Provider: "team-ollama", Name: "sk-fixture-credential-shaped"}, false},
	} {
		if err := validateChatModel(cases.model); (err == nil) != cases.valid {
			t.Errorf("validateChatModel(%+v) = %v, want valid=%v", cases.model, err, cases.valid)
		}
	}
}
