package api_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
)

func TestChatDiscoveryResumesWorkflowActionOnSelectedID(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: chatControlsFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"payments-reader", "mobile-reader"} {
		body := fmt.Sprintf(`{"metadata":{"id":%q,"name":"Review workflow"},"spec":{"nodes":[{"id":"reviewer","runtime":{"kind":"echo"}}]}}`, id)
		if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", body, nil); c != 201 {
			t.Fatal(c)
		}
	}
	const path = "/api/v1/chat/sessions"
	ask := func(suffix, token string, n int, text string) chat.Session {
		t.Helper()
		var v chat.Session
		if c := call(t, srv, "POST", path+suffix, token, "application/json", fmt.Sprintf(`{"requestId":"%032x","text":%q}`, n, text), &v); c != 200 {
			t.Fatal(c)
		}
		return v
	}
	v := ask("", operatorTok, 1, "Start the Review workflow.")
	m := v.Messages[1]
	if m.State != "discovery" || m.Discovery != nil || m.Review != nil || !strings.Contains(m.Text, "payments-reader") || !strings.Contains(m.Text, "mobile-reader") {
		t.Fatal(m)
	}
	var restored chat.Session
	if c := call(t, srv, "GET", path+"/"+v.ID, operatorTok, "", "", &restored); c != 200 || restored.Messages[1].Text != m.Text {
		t.Fatal(c)
	}
	other := ask("", operatorTok, 2, "/choose 1")
	if other.Messages[1].Review != nil || !strings.Contains(other.Messages[1].Text, "Start a resource search") {
		t.Fatal(other)
	}
	if c := call(t, srv, "POST", path+"/"+v.ID+"/messages", viewerTok, "application/json", `{"requestId":"00000000000000000000000000000003","text":"1"}`, nil); c != 404 {
		t.Fatalf("foreign choice %d", c)
	}
	replied := ask("/"+v.ID+"/messages", operatorTok, 4, "o do payments")
	r := replied.Messages[len(replied.Messages)-1].Review
	if r == nil || r.WorkflowControl == nil || r.WorkflowControl.Scope.WorkflowID != "payments-reader" || r.WorkflowControl.Action != "start" || r.State != chat.ReviewAwaiting {
		t.Fatalf("selected review %+v", r)
	}
	record, err := app.Store.GetGraphRecord(t.Context(), "payments-reader")
	if err != nil || record.DesiredState == "running" {
		t.Fatal(record, err)
	}
}

func TestChatFindReportsAnIncompleteInventoryAndRejectsStalePick(t *testing.T) {
	srv, _ := setupApp(t, nil)
	for i := range 9 {
		body := fmt.Sprintf(`{"metadata":{"id":"discover-%d","name":"Discovery"},"spec":{"nodes":[{"id":"reader","runtime":{"kind":"echo"}}]}}`, i)
		if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", body, nil); c != 201 {
			t.Fatal(c)
		}
	}
	const path = "/api/v1/chat/sessions"
	var v chat.Session
	if c := call(t, srv, "POST", path, viewerTok, "application/json", `{"requestId":"00000000000000000000000000000044","text":"/find workflows discovery"}`, &v); c != 200 || v.Messages[1].State != "discovery" || !strings.Contains(v.Messages[1].Text, "Search incomplete") || strings.Contains(v.Messages[1].Text, "9. ") {
		t.Fatal(c, v)
	}
	// Remove a fixture resource after the alternatives have been rendered.
	if c := call(t, srv, "DELETE", "/api/v1/graphs/discover-0", adminTok, "", "", nil); c != 200 && c != 204 {
		t.Fatal(c)
	}
	if c := call(t, srv, "POST", path+"/"+v.ID+"/messages", viewerTok, "application/json", `{"requestId":"00000000000000000000000000000045","text":"discover-0"}`, &v); c != 200 || !strings.Contains(v.Messages[3].Text, "no longer available") || v.Messages[3].Review != nil {
		t.Fatal(c, v.Messages)
	}
}
