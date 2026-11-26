package chat

import (
	"github.com/woliveiras/weft/server/internal/instructions"
	"testing"
	"time"
)

func TestManagedSkillReviewPreservesScopeAndChecksContent(t *testing.T) {
	d := SkillDraft{Action: "create", ID: "review-fixture", Title: "Review", Body: "Check the changed tests."}
	r, err := NewSkillReview("operator", "session", d, nil, time.Now())
	if err != nil || r.Skill.After.Enabled || r.Skill.After.Global {
		t.Fatal(r, err)
	}
	old := &instructions.Skill{Version: instructions.Version{Revision: 4, Input: instructions.Input{ID: d.ID, Title: "Old", Body: "Prior.", Description: "Context.", Enabled: true, Global: true}}}
	d.Action = "revise"
	r, err = NewSkillReview("operator", "session", d, old, time.Now())
	if err != nil || !r.Skill.After.Global || !r.Skill.After.Enabled || r.Skill.Revision != 4 || r.Skill.Before.Body != "Prior." || r.Skill.After.Description != "Context." {
		t.Fatal(r, err)
	}
	d.Body = "password=unreviewable"
	if _, err := NewSkillReview("operator", "session", d, old, time.Now()); err == nil {
		t.Fatal("accepted sensitive text")
	}
	if _, err := decodePlan(`{"kind":"unsupported","skillId":"other"}`); err == nil {
		t.Fatal("mixed intents")
	}
}
