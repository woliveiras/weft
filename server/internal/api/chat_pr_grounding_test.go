package api

import (
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
)

func TestGroundChatPRPlanPreservesExplicitRepositoryAndLink(t *testing.T) {
	const prURL = "https://scm.example/acme/mobile/pull/418"
	tests := []struct {
		name, input, proposedRepository  string
		wantRepository, wantURL, wantAsk string
	}{
		{name: "number only", input: "Revise o PR 418.", proposedRepository: "acme/desktop"},
		{name: "omitted scope", input: "Revise o PR 418 em acme/mobile.", wantRepository: "acme/mobile"},
		{name: "wrong scope", input: "Revise o PR 418 em `acme.org/mobile`.", proposedRepository: "acme/desktop", wantRepository: "acme.org/mobile"},
		{name: "valid link", input: "Review [PR 418](" + prURL + ").", wantURL: prURL},
		{name: "confirmed slug and link", input: "Review acme/mobile via " + prURL, wantRepository: "acme/mobile", wantURL: prURL},
		{name: "conflicting slug and link", input: "Review acme/desktop via " + prURL, wantAsk: "differs from the supplied link"},
		{name: "two repositories", input: "Review PR 418 in acme/mobile or acme/desktop.", wantAsk: "Which repository"},
		{name: "unrecognized link", input: "Review PR 418 at https://scm.example/acme/mobile/releases/418.", wantAsk: "recognize the supplied link"},
		{name: "incomplete link", input: "Review PR 418 at https://", wantAsk: "recognize the supplied link"},
		{name: "non-HTTPS link", input: "Review PR 418 at http://scm.example/acme/mobile/pull/418.", wantAsk: "recognize the supplied link"},
		{name: "link without scheme", input: "Review PR 418 at scm.example/acme/mobile/pull/418.", wantAsk: "recognize the supplied link"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			proposal := chat.Plan{Kind: chat.AnalyzePR, PRSelector: chat.PRSelector{Number: "418", Repository: tc.proposedRepository}}
			plan, question := groundChatPRPlan(proposal, tc.input, "request", &chat.Session{}, Identity{Role: config.RoleOperator})
			if tc.wantAsk != "" {
				if !strings.Contains(question, tc.wantAsk) {
					t.Fatalf("question = %q, want it to contain %q", question, tc.wantAsk)
				}
				return
			}
			if question != "" || plan.PRSelector.Number != "418" || plan.PRSelector.Repository != tc.wantRepository || plan.PRSelector.URL != tc.wantURL {
				t.Fatalf("selector = %#v, question = %q, want PR 418, repository %q and URL %q", plan.PRSelector, question, tc.wantRepository, tc.wantURL)
			}
		})
	}
}
