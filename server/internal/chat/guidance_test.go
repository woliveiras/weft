package chat

import (
	"testing"
	"time"
)

func TestGuidanceBindsMessageRunAndReplica(t *testing.T) {
	p, err := decodePlan(`{"kind":"node.guide","guideWorkflow":"review","guideNode":"reader","guideReplica":"any","guidance":"Summarize the warnings."}`)
	if err != nil {
		t.Fatal(err)
	}
	g := p.Guidance
	for _, value := range []string{"", "00", "-1", "0.5", "everyone"} {
		g.Replica = value
		if g.Valid() {
			t.Errorf("invalid replica accepted: %q", value)
		}
	}
	g.Replica, g.Run, g.Version = "0", "run-one", 2
	now := time.Now()
	r, err := NewGuidanceReview("operator", "session", g, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ValidateConfirmation("operator", "session", r.Digest, now); err != nil {
		t.Fatal(err)
	}
	r.Guidance.Run = "replacement"
	if err := r.ValidateConfirmation("operator", "session", r.Digest, now); err == nil {
		t.Fatal("changed run was accepted")
	}
}
