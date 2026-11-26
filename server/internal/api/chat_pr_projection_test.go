package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
)

func TestChatManualPRProjectionRequiresOperator(t *testing.T) {
	session := &chat.Session{Messages: []chat.Message{
		{Review: &chat.OperationReview{PR: &chat.PRAnalysis{Repository: "fixture/repo"}, PRSummary: "private fixture draft"}},
		{Review: &chat.OperationReview{PRPublication: &chat.PRCommentPublication{ExecutionID: "protected-fixture-execution"}}},
	}}
	for _, role := range []config.Role{config.RoleViewer, config.RoleOperator, config.RoleAdmin} {
		w := httptest.NewRecorder()
		writeChatSession(w, Identity{Role: role}, session)
		if strings.Contains(w.Body.String(), "private fixture draft") != role.Allows(config.RoleOperator) ||
			strings.Contains(w.Body.String(), "protected-fixture-execution") != role.Allows(config.RoleOperator) {
			t.Fatal(role)
		}
	}
}
