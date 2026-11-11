package environmentpolicy

import (
	"github.com/woliveiras/weft/server/internal/domain"
	"testing"
)

func TestPolicy(t *testing.T) {
	for _, test := range []struct {
		name   string
		p      Policy
		e      domain.EnvironmentSpec
		denied bool
	}{
		{name: "compatible defaults"},
		{name: "offline", p: Policy{SetupNetworks: []string{"none"}, RunNetworks: []string{"none"}}, e: domain.EnvironmentSpec{Network: domain.EnvironmentNetwork{Setup: "none"}}},
		{name: "implicit setup denied", p: Policy{SetupNetworks: []string{"none"}}, denied: true},
		{name: "run denied", p: Policy{RunNetworks: []string{"none"}}, e: domain.EnvironmentSpec{Network: domain.EnvironmentNetwork{Run: "bridge"}}, denied: true},
		{name: "host denied", e: domain.EnvironmentSpec{Network: domain.EnvironmentNetwork{Run: "host"}}, denied: true},
		{name: "setup", p: Policy{DisableSetup: true}, e: domain.EnvironmentSpec{Setup: []string{"echo ready"}}, denied: true},
		{name: "Dockerfile", p: Policy{DisableDockerfiles: true}, e: domain.EnvironmentSpec{Dockerfile: "FROM alpine"}, denied: true},
		{name: "agent", p: Policy{DisableAgentProposals: true}, e: domain.EnvironmentSpec{Origin: &domain.EnvironmentOrigin{Source: "agent"}}, denied: true},
		{name: "companions", p: Policy{DisableCompanions: true}, e: domain.EnvironmentSpec{Companions: []domain.Companion{{Name: "db", Image: "postgres"}}}, denied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.p.Check(&test.e); (err != nil) != test.denied {
				t.Fatalf("Check: %v", err)
			}
		})
	}
	for _, p := range []Policy{{SetupNetworks: []string{}}, {RunNetworks: []string{"host"}}, {SetupNetworks: []string{"custom"}}} {
		if p.Validate() == nil {
			t.Fatal("invalid policy accepted")
		}
	}
}

func TestEgressPolicy(t *testing.T) {
	p := Policy{RunNetworks: []string{"none", "egress"}, EgressHosts: []string{"example.com"}}
	e := &domain.EnvironmentSpec{Network: domain.EnvironmentNetwork{Run: "egress", AllowedHosts: []string{"example.com"}}}
	if err := p.Check(e); err != nil {
		t.Fatal(err)
	}
	if err := (Policy{}).Check(e); err == nil {
		t.Fatal("default enabled egress")
	}
	e.Network.AllowedHosts = []string{"other.example"}
	if p.Check(e) == nil {
		t.Fatal("host outside installation list")
	}
	e.Network.AllowedHosts = nil
	if p.Check(e) == nil {
		t.Fatal("empty allowlist")
	}
	e.Network.Run = "none"
	e.Network.AllowedHosts = []string{"example.com"}
	if p.Check(e) == nil {
		t.Fatal("ignored allowlist on non-proxy network")
	}
}

func TestPreparationEgressPolicy(t *testing.T) {
	p := Policy{SetupNetworks: []string{"none", "egress"}, EgressHosts: []string{"packages.example"}}
	for _, tc := range []struct {
		mode  string
		hosts []string
		valid bool
	}{
		{"egress", []string{"packages.example"}, true},
		{"egress", nil, false},
		{"egress", []string{"other.example"}, false},
		{"egress", []string{"*.example"}, false},
		{"egress", []string{"packages.example", "packages.example"}, false},
		{"none", []string{"packages.example"}, false},
		{"none", nil, true},
	} {
		e := &domain.EnvironmentSpec{Image: "alpine", Network: domain.EnvironmentNetwork{Setup: tc.mode, SetupAllowedHosts: tc.hosts}}
		if err := p.Check(e); (err == nil) != tc.valid {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	if err := (Policy{}).Check(&domain.EnvironmentSpec{Network: domain.EnvironmentNetwork{Setup: "egress", SetupAllowedHosts: []string{"packages.example"}}}); err == nil {
		t.Fatal("not opt-in")
	}
}
