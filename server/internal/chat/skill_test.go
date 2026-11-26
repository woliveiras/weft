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

func TestSkillLifecycleOnlyChangesRequestedSetting(t *testing.T) {
	input := instructions.Input{ID: "release", Title: "Release notes", Description: "Description", Body: "Use the template."}
	for _, action := range []string{"enable", "disable", "make-default", "remove-default", "delete"} {
		before := input
		before.Enabled = action != "enable"
		before.Global = action != "make-default"
		old := &instructions.Skill{Version: instructions.Version{Revision: 2, Input: before}}
		d := SkillDraft{ID: before.ID, Action: action}
		r, err := NewSkillReview("owner", "session", d, old, time.Now())
		if err != nil {
			t.Fatal(action, err)
		}
		want := before
		switch action {
		case "enable", "disable", "delete":
			want.Enabled = action == "enable"
		case "make-default", "remove-default":
			want.Global = action == "make-default"
		}
		if r.Skill.After != want || *r.Skill.Before != before || r.Skill.Delete != (action == "delete") {
			t.Fatal(action, r.Skill)
		}
		d.Body = "replace content"
		if d.Valid() {
			t.Fatal("unexpected content permitted in lifecycle action")
		}
	}
}
