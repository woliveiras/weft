package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestHistoryKeepsCurrentRequestSeparateAfterProjectedReplies(t *testing.T) {
	for _, prior := range []Message{
		{Role: "assistant", Destination: "budgets", Text: "Open the console page."},
		{Role: "assistant", Destination: "usage", State: "grounded", Text: "Atlas cost 12 USD."},
		{Role: "assistant", Destination: "workflows", State: "grounded", Text: "The latest checkpoint is worker/r0/1."},
		{Role: "assistant", Review: &OperationReview{State: ReviewAwaiting}, Text: "private review details"},
		{Role: "assistant", Text: "private diagnostic details"},
	} {
		s := Session{Messages: []Message{{ID: "a", Role: "user", Text: "Previous question"}, prior, {ID: "b", Role: "user", Text: "Current question"}, {Role: "assistant", Text: "Old failed answer"}, {ID: "c", Role: "user", Text: "Later question"}}}
		turns := s.Context("b")
		if len(turns) != 3 || turns[1].Role != llm.RoleAssistant || turns[1].Content == "" || turns[2].Content != "Current question" {
			t.Fatalf("retry context %v", turns)
		}
		if strings.Contains(turns[1].Content, "private") {
			t.Fatal("private reply copied to model context")
		}
		if prior.State == "grounded" && turns[1].Content != prior.Text {
			t.Fatal("spend answer missing from context")
		}
	}
}

func TestBoundedHistoryStartsWithUserAtTurnLimit(t *testing.T) {
	var turns []llm.ChatMessage
	for i := 0; i < maxContextTurns; i++ {
		turns = append(turns, llm.ChatMessage{Role: llm.RoleUser, Content: fmt.Sprint(i)}, llm.ChatMessage{Role: llm.RoleAssistant, Content: "ok"})
	}
	turns = append(turns, llm.ChatMessage{Role: llm.RoleUser, Content: "Latest"})
	window, err := boundedHistory(turns)
	if err != nil || len(window) == 0 || window[0].Role != llm.RoleUser || window[len(window)-1].Content != "Latest" {
		t.Fatalf("invalid history window %v %v", window, err)
	}
}

func TestBoundedHistoryStartsWithUserAtByteLimit(t *testing.T) {
	full := strings.Repeat("x", MaxRequestBytes)
	turns := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: full},
		{Role: llm.RoleAssistant, Content: "Old answer"},
		{Role: llm.RoleUser, Content: full},
		{Role: llm.RoleAssistant, Content: "More recent answer"},
		{Role: llm.RoleUser, Content: full},
	}
	got, err := boundedHistory(turns)
	if err != nil || len(got) != 3 || got[0].Role != llm.RoleUser || got[1].Content != "More recent answer" || got[2].Content != full {
		t.Fatalf("invalid byte-limited history: length %d, error %v", len(got), err)
	}
}
