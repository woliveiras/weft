package api_test

import (
	"net/http"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
)

func TestChatSessionAPIEnforcesOwnershipAndIdempotency(t *testing.T) {
	const id = "8921e80ce56b4b50a83981d63b1a911b"
	const sessions = "/api/v1/chat/sessions"
	const body = `{"requestId":"` + id + `","text":"Follow the release notes."}`
	srv := setup(t)
	var result chat.Session
	if code := call(t, srv, "POST", sessions, viewerTok, "application/json", body, &result); code != http.StatusOK {
		t.Fatalf("create=%d", code)
	}
	// Without a selected provider, the request is saved and explicitly retryable.
	if result.ID != id || len(result.Messages) != 2 || result.Messages[0].State != "unavailable" {
		t.Fatalf("saved request: %+v", result)
	}
	if code := call(t, srv, "POST", sessions, viewerTok, "application/json", body, &result); code != http.StatusOK || len(result.Messages) != 2 {
		t.Fatalf("replay=%d %+v", code, result)
	}
	if code := call(t, srv, "POST", sessions, viewerTok, "application/json", `{"requestId":"`+id+`","text":"Different target"}`, nil); code != http.StatusConflict {
		t.Fatalf("conflicting replay: %d", code)
	}
	route := sessions + "/" + id
	for _, req := range []struct{ method, path string }{
		{"GET", route}, {"POST", route + "/archive"}, {"POST", route + "/messages/" + id + "/retry"},
	} {
		if status := call(t, srv, req.method, req.path, adminTok, "", "", nil); status != http.StatusNotFound {
			t.Fatalf("administrator accessed another owner's session with %s %s: %d", req.method, req.path, status)
		}
	}
	var list struct {
		Items  []jsonChatSummary
		Shared bool
	}
	if code := call(t, srv, "GET", sessions, adminTok, "", "", &list); code != http.StatusOK || len(list.Items) != 0 || list.Shared {
		t.Fatalf("owner fencing: %d %+v", code, list)
	}
	if code := call(t, srv, "POST", route+"/archive", viewerTok, "", "", &result); code != http.StatusOK || !result.Archived {
		t.Fatalf("archive=%d %+v", code, result)
	}
	if code := call(t, srv, "GET", sessions+"?archived=true", viewerTok, "", "", &list); code != http.StatusOK ||
		len(list.Items) != 1 || list.Items[0].ID != id {
		t.Fatalf("archive list: %d %+v", code, list)
	}
}

type jsonChatSummary struct{ ID string }

func TestChatSessionAPILabelsSharedLocalHistory(t *testing.T) {
	srv, _ := setupApp(t, func(config *config.Config) { config.Auth.Disabled = true })
	const route = "/api/v1/chat/sessions"
	const id = "5561e80ce56b4b50a83981d63b1a811b"
	if code := call(t, srv, "POST", route, "", "application/json", `{"requestId":"`+id+`","text":"Inspect my task."}`, nil); code != http.StatusOK {
		t.Fatalf("local request=%d", code)
	}
	var list struct {
		Items  []jsonChatSummary
		Shared bool
	}
	if code := call(t, srv, "GET", route, "", "", "", &list); code != http.StatusOK || !list.Shared || len(list.Items) != 1 || list.Items[0].ID != id {
		t.Fatalf("local conversation list: %d %+v", code, list)
	}
}
