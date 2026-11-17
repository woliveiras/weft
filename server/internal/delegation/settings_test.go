package delegation

import (
	"strings"
	"testing"
)

func TestPolicyValidation(t *testing.T) {
	if e := DefaultPolicy().Validate(); e != nil {
		t.Fatal(e)
	}
	for _, mut := range []func(*Policy){func(p *Policy) { p.ApprovalMode = "skip" }, func(p *Policy) { p.MaxDepth = 5 }, func(p *Policy) { p.MaxChildrenPerExecution = 0 }, func(p *Policy) { p.MaxConcurrentChildren = 33 }, func(p *Policy) { p.MaxDurationSeconds = 601 }, func(p *Policy) { p.MaxOutputTokens = -1 }, func(p *Policy) { p.MaxObjectiveBytes = 16385 }, func(p *Policy) { p.MaxResultBytes = 65537 }, func(p *Policy) { p.MaxTreeTokens = 0 }, func(p *Policy) { p.MaxTreeTokens = MaxTreeTokens + 1 }} {
		p := DefaultPolicy()
		mut(&p)
		if p.Validate() == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	p := Policy{true, "automatic", 1, 32, 32, 600, 4096, 16384, 65536, MaxTreeTokens}
	if e := p.Validate(); e != nil {
		t.Fatal(e)
	}
}
func TestProfileValidation(t *testing.T) {
	for _, mut := range []func(*Profile){func(p *Profile) { p.ID = "../escape" }, func(p *Profile) { p.Name = " " }, func(p *Profile) { p.Instructions = "" }, func(p *Profile) { p.Instructions = "\x00" }, func(p *Profile) { p.Instructions = strings.Repeat("é", 2049) }, func(p *Profile) { p.MaxOutputTokens = 0 }, func(p *Profile) { p.MaxOutputTokens = 4097 }} {
		p := DefaultProfile()
		mut(&p)
		if p.Validate() == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	p := DefaultProfile()
	p.Instructions = strings.Repeat("é", 2048)
	if e := p.Validate(); e != nil {
		t.Fatal(e)
	}
}

func TestRecursivePolicyRequiresBoundedExplicitOptIn(t *testing.T) {
	p := DefaultPolicy()
	if p.MaxDepth != 1 {
		t.Fatal("default permits recursion")
	}
	for depth := 1; depth <= 4; depth++ {
		p.MaxDepth = depth
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	p.MaxDepth = 0
	if p.Validate() == nil {
		t.Fatal("accepted unbounded depth")
	}
	profile := DefaultProfile()
	profile.Tools = []string{"delegation.request"}
	if err := profile.Validate(); err != nil {
		t.Fatal(err)
	}
}
