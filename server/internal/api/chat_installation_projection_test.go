package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
)

func TestChatInstallationReviewIsHiddenAfterLossOfAdminRole(t *testing.T) {
	const privateField = "private-fixture-config"
	session := &chat.Session{Messages: []chat.Message{{ID: "request", Role: "user", Text: "Review configuration"}, {ID: "reply", Role: "assistant", Review: &chat.OperationReview{
		Configuration: &chat.ConfigReview{Document: privateField},
	}}}}
	for _, role := range []config.Role{config.RoleViewer, config.RoleOperator, config.RoleAdmin} {
		w := httptest.NewRecorder()
		writeChatSession(w, Identity{Role: role}, session)
		if strings.Contains(w.Body.String(), privateField) != (role == config.RoleAdmin) {
			t.Fatalf("projection for %s", role)
		}
	}
}
