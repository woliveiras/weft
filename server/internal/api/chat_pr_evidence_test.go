package api

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/scm"
)

func TestChatPRDiffRequiresTheFullPinnedRevision(t *testing.T) {
	p := chat.PRAnalysis{Number: 418, Head: "head", Base: "base", HeadRef: "feature", BaseRef: "main"}
	current := scm.ReviewSnapshot{Number: 418, HeadSHA: "head", BaseSHA: "base", Head: "feature", Base: "main", State: "open"}
	if !sameChatPRRevision(p, current) {
		t.Fatal("the exact reviewed source did not match")
	}
	for _, test := range []struct {
		name   string
		change func(*scm.ReviewSnapshot)
	}{
		{"number", func(s *scm.ReviewSnapshot) { s.Number++ }},
		{"head", func(s *scm.ReviewSnapshot) { s.HeadSHA = "other" }},
		{"base", func(s *scm.ReviewSnapshot) { s.BaseSHA = "other" }},
		{"head ref", func(s *scm.ReviewSnapshot) { s.Head = "other" }},
		{"base ref", func(s *scm.ReviewSnapshot) { s.Base = "other" }},
		{"draft", func(s *scm.ReviewSnapshot) { s.Draft = true }},
		{"closed", func(s *scm.ReviewSnapshot) { s.State = "closed" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			other := current
			test.change(&other)
			if sameChatPRRevision(p, other) {
				t.Fatal("the revised source still matched")
			}
		})
	}
}
