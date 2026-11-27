package chat

import (
	"fmt"
	"strings"
	"testing"
)

func TestDiscoveryMatchingIsBoundedAndUsesDisplayedOrder(t *testing.T) {
	d := AskDiscovery(Plan{Kind: AnalyzePR}, "pr-repository", []Choice{{ID: "a", Label: "pagamentos/api · Corrigir estorno"}, {ID: "b", Label: "mobile/ios · Ajustar login"}}, "Which repository?", false).Discovery
	for input, want := range map[string]string{"2": "b", "use o primeiro": "a", "o do estorno": "a", "/choose 9": "", "opção 0": "", "mobile/ios": "b"} {
		c, recognized := d.Match(input)
		if !recognized || c.ID != want {
			t.Errorf("%q = %+v %v", input, c, recognized)
		}
	}
	if _, ok := d.Match("pause the second workflow"); ok {
		t.Fatal("reinterpreted a new action as a choice")
	}
	search := ResourceSearch{Query: "review"}
	for n := range 12 {
		id := fmt.Sprintf("review-%d", n)
		search.Add(id, "Review entry", id)
	}
	if !search.Incomplete() || len(search.Choices) != InspectionLimit {
		t.Fatal(search)
	}
	search.Add("review", "exact", "exact")
	if search.Incomplete() || len(search.Choices) != 1 || search.Choices[0].ID != "review" {
		t.Fatal(search)
	}
	unsafe := AskDiscovery(Plan{Kind: FindResource}, "projects", []Choice{{ID: "a", Label: "x\ny"}, {ID: "b", Label: "token=private"}}, "Choose", false)
	if !unsafe.Discovery.Incomplete || len(unsafe.Discovery.Choices) != 0 {
		t.Fatal("unsafe label retained")
	}
}

func TestDiscoveryUsesOnlyPrecedingConversationTurn(t *testing.T) {
	d := &Discovery{Area: "agents"}
	session := Session{Messages: []Message{{ID: "a", Role: "user"}, {ID: "reply-a", Role: "assistant", Discovery: d}, {ID: "notify", Role: "assistant"}, {ID: "b", Role: "user"}, {ID: "reply-b", Role: "assistant"}, {ID: "c", Role: "user"}}}
	if session.PriorDiscovery("b") != d || session.PriorDiscovery("c") != nil || session.PriorDiscovery("foreign") != nil {
		t.Fatal("misbound discovery")
	}
}

func TestDiscoveryNeverSendsCandidateLabelsOrPrivateContinuationToTheModel(t *testing.T) {
	d := AskDiscovery(Plan{Kind: AnalyzePR}, "pr-repository", []Choice{{ID: "a", Label: "untrusted-candidate-title"}}, "Which repo?", false)
	session := Session{Messages: []Message{{ID: "reply-a", Role: "assistant", Text: d.Text, State: "clarification", Discovery: d.Discovery}, {ID: "b", Role: "user", Text: "find a workflow"}}}
	for _, m := range session.Context("b") {
		if strings.Contains(m.Content, "untrusted-candidate-title") {
			t.Fatal("provider received a candidate title")
		}
	}
}

func TestTaskChoiceMetadataFollowsRankedMatch(t *testing.T) {
	search := ResourceSearch{Query: "review"}
	search.AddTask("tsk_first", "review first", TaskCandidate{Title: "review first", Status: "running"})
	search.AddTask("review", "review exact", TaskCandidate{Title: "review exact", Status: "blocked", ProjectID: "docs"})
	if len(search.Choices) != 1 || search.Choices[0].ID != "review" || *search.Choices[0].Task != (TaskCandidate{Title: "review exact", Status: "blocked", ProjectID: "docs"}) {
		t.Fatal(search.Choices)
	}
}
