package api_test

import (
	"fmt"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestChatPickerReadsWithoutSessionAndFollowsOnlySelectedTask(t *testing.T) {
	srv, app := setupApp(t, nil)
	manager := taskconfig.Manager{Store: app.Store}
	var tasks [2]*taskconfig.Draft
	for i := range tasks {
		task, err := manager.SavePrompt(t.Context(), taskconfig.PromptInput{Request: "Review release", Settings: &taskconfig.PromptSettings{Title: "Twin reviews"}}, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		tasks[i] = task
	}
	var matches struct {
		Items   []struct{ ID, Label string }
		Partial bool
	}
	if status := call(t, srv, "GET", "/api/v1/chat/candidates?area=tasks&q=Twin", viewerTok, "", "", &matches); status != 200 || matches.Partial || len(matches.Items) != 2 {
		t.Fatal(status, matches)
	}
	var sessions struct{ Items []jsonChatSummary }
	const path = "/api/v1/chat/sessions"
	if status := call(t, srv, "GET", path, viewerTok, "", "", &sessions); status != 200 || len(sessions.Items) != 0 {
		t.Fatal(status, sessions)
	}
	body := fmt.Sprintf(`{"requestId":"aa000000000000000000000000000001","text":"/follow %s\nCheck the release."}`, tasks[1].ID)
	for range 2 {
		var session chat.Session
		if status := call(t, srv, "POST", path, viewerTok, "application/json", body, &session); status != 200 || len(session.Messages) != 2 || len(session.Watches) != 1 || session.Watches[0].Task.ID != tasks[1].ID {
			t.Fatal(status, session)
		}
	}
	if status := call(t, srv, "GET", "/api/v1/chat/candidates?area=administration", viewerTok, "", "", nil); status != 400 {
		t.Fatal(status)
	}
}
