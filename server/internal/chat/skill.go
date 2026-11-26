package chat

import (
	"time"

	"github.com/woliveiras/weft/server/internal/instructions"
)

const SaveSkill = "skill.save"

type SkillDraft struct {
	Action string
	ID     string
	Title  string
	Body   string
}

func (d SkillDraft) Valid() bool {
	return (d.Action == "create" || d.Action == "revise") && instructions.ID.MatchString(d.ID) && !instructions.IsBuiltin(d.ID) && shortLabel(d.Title) && safeText(d.Body, 3000) && instructions.Validate(instructions.Input{ID: d.ID, Title: d.Title, Body: d.Body}) == nil
}

type SkillChange struct {
	Before   *instructions.Input `json:"before,omitempty"`
	After    instructions.Input  `json:"after"`
	Revision int64               `json:"revision"`
}

func NewSkillReview(owner, session string, d SkillDraft, old *instructions.Skill, now time.Time) (*OperationReview, error) {
	if !d.Valid() || (d.Action == "create") != (old == nil) {
		return nil, ErrInvalidPlan
	}
	change := SkillChange{}
	if old != nil {
		if old.ID != d.ID || old.Deleted || old.Builtin || old.Revision < 1 || !shortLabel(old.Title) || !safeText(old.Description, 1000) || !safeText(old.Body, 3000) {
			return nil, ErrInvalidPlan
		}
		before := old.Input
		change.Before, change.After, change.Revision = &before, before, old.Revision
	}
	change.After.ID, change.After.Title, change.After.Body = d.ID, d.Title, d.Body
	return newReview(owner, session, now, OperationReview{Skill: &change})
}
