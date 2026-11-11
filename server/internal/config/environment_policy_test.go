package config

import (
	"gopkg.in/yaml.v3"
	"testing"
)

func TestEnvironmentPolicyConfiguration(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{"setupNetworks: [none], runNetworks: [none], disableAgentProposals: true", true},
		{"setupNetworks: []", false},
		{"runNetworks: [host]", false},
		{"setupNetworks: [custom]", false},
	} {
		cfg := Default()
		cfg.Auth.Disabled = true
		if err := yaml.Unmarshal([]byte("isolation:\n  docker:\n    environmentPolicy: {"+tc.body+"}\n"), cfg); err != nil {
			t.Fatal(err)
		}
		if err := cfg.Validate(); (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.body, err)
		}
	}
}
