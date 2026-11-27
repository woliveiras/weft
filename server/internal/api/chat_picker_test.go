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
		task, err := manager.SavePrompt(t.Context(), taskconfig.PromptInput{Request: "Review release", Settings: &taskconfig.PromptSettings{Title: "Twin · reviews"}}, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		tasks[i] = task
	}
	var matches struct {
		Items []struct {
			ID, Label string
			Task      *chat.TaskCandidate
		}
		Partial bool
	}
	if status := call(t, srv, "GET", "/api/v1/chat/candidates?area=tasks&q=Twin", viewerTok, "", "", &matches); status != 200 || matches.Partial || len(matches.Items) != 2 {
		t.Fatal(status, matches)
	}
	for _, match := range matches.Items {
		if match.Task == nil || match.Task.Title != "Twin · reviews" || match.Task.Status != "draft" {
			t.Fatal("browse lost structured task fields", match)
		}
	}
	var sessions struct{ Items []jsonChatSummary }
	const path = "/api/v1/chat/sessions"
	if status := call(t, srv, "GET", path, viewerTok, "", "", &sessions); status != 200 || len(sessions.Items) != 0 {
		t.Fatal(status, sessions)
	}
	const stale = "aa000000000000000000000000000009"
	if status := call(t, srv, "POST", path, viewerTok, "application/json", `{"requestId":"`+stale+`","text":"/follow tsk_gone"}`, nil); status != 400 {
		t.Fatal(status)
	}
	if status := call(t, srv, "GET", path+"/"+stale, viewerTok, "", "", nil); status != 404 {
		t.Fatal(status)
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

func TestChatPickerPartialSearchAndExactResourceReplay(t *testing.T) {
	srv, app := setupApp(t, nil)
	manager := taskconfig.Manager{Store: app.Store}
	var selected *taskconfig.Draft
	for range chat.InspectionLimit + 1 {
		task, err := manager.SavePrompt(t.Context(), taskconfig.PromptInput{Request: "Check the release", Settings: &taskconfig.PromptSettings{Title: "Release review"}}, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		selected = task
	}
	var result struct {
		Items []struct {
			ID, Label string
			Task      *chat.TaskCandidate
		}
		Partial bool
	}
	const lookup = "/api/v1/chat/candidates?area=tasks&q="
	if status := call(t, srv, "GET", lookup+"Release", viewerTok, "", "", &result); status != 200 || !result.Partial || len(result.Items) != chat.InspectionLimit {
		t.Fatal(status, result)
	}
	if status := call(t, srv, "GET", lookup+selected.ID, viewerTok, "", "", &result); status != 200 || result.Partial || len(result.Items) != 1 || result.Items[0].ID != selected.ID {
		t.Fatal(status, result)
	}
	if status := call(t, srv, "GET", lookup+"Release", "", "", "", nil); status != 401 {
		t.Fatal(status)
	}
	if task := result.Items[0].Task; task == nil || task.Title != selected.Title || task.Status != selected.Status {
		t.Fatal("exact lookup lost task metadata", result.Items[0])
	}
	const sessionID = "aa000000000000000000000000000002"
	const path = "/api/v1/chat/sessions"
	text := "/lookup tasks " + selected.ID
	body := fmt.Sprintf(`{"requestId":%q,"text":%q}`, sessionID, text)
	var session chat.Session
	if status := call(t, srv, "POST", path, viewerTok, "application/json", body, &session); status != 200 || len(session.Messages) != 2 || session.Messages[1].Text != result.Items[0].Label || len(session.Watches) != 0 {
		t.Fatal(status, session)
	}
	if err := manager.DeleteDraft(t.Context(), selected.ID, selected.Revision, "fixture"); err != nil {
		t.Fatal(err)
	}
	// A different resource can even have the missing ID as its title.
	if _, err := manager.SavePrompt(t.Context(), taskconfig.PromptInput{Request: "Replacement", Settings: &taskconfig.PromptSettings{Title: selected.ID}}, "fixture"); err != nil {
		t.Fatal(err)
	}
	if status := call(t, srv, "POST", path, viewerTok, "application/json", body, &session); status != 200 || len(session.Messages) != 2 || session.Messages[1].Text != result.Items[0].Label {
		t.Fatal(status, session)
	}
	if status := call(t, srv, "POST", path, viewerTok, "application/json", fmt.Sprintf(`{"requestId":"aa000000000000000000000000000003","text":%q}`, text), nil); status != 400 {
		t.Fatal(status)
	}
}
