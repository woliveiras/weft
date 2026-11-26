package api_test

import (
	"context"
	"fmt"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/executioncontrol"
	"github.com/woliveiras/weft/server/internal/llm"
	"net/http"
	"testing"
	"time"
)

type stopChildFixture struct{ chatAgentFixture }

func (stopChildFixture) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: `{"kind":"execution.cancel","executionId":"child","executionScope":"child"}`}}, nil
}

func TestChatExecutionCancelSeparatesRequestFromChildSettlement(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: stopChildFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	root := domain.Execution{ID: "root", GraphID: "workflow", RunID: "first", Status: domain.ExecRunning, StartedAt: time.Now()}
	child := root
	child.ID = "child"
	child.ParentExecutionID = root.ID
	for _, e := range []*domain.Execution{&root, &child} {
		if err := app.Store.PutExecution(t.Context(), e); err != nil {
			t.Fatal(err)
		}
	}
	const route = "/api/v1/chat/sessions"
	const id = "bf35e80ce56b4b50a83981d63b1a811b"
	const body = `{"requestId":"` + id + `","text":"Cancel the delegated child execution."}`
	var v chat.Session
	if code := call(t, srv, "POST", route, viewerTok, "application/json", body, &v); code != 200 || len(v.Messages) != 2 || v.Messages[1].Review != nil {
		t.Fatal("viewer", code)
	}
	if code := call(t, srv, "POST", route, operatorTok, "application/json", body, &v); code != 200 || v.Messages[1].Review == nil {
		t.Fatal("operator", code)
	}
	r := v.Messages[1].Review
	for range 2 {
		if code := call(t, srv, "POST", route+"/"+id+"/operations/"+r.ID+"/confirm", operatorTok, "application/json", fmt.Sprintf(`{"digest":%q}`, r.Digest), &v); code != http.StatusOK || v.Messages[1].Review.State != chat.ReviewAccepted {
			t.Fatal("accepted", code, v.Messages[1].Review)
		}
	}
	manager := executioncontrol.Manager{Store: app.Store}
	if found, err := manager.Requested(t.Context(), "root"); found != nil || err != nil {
		t.Fatal("sibling scope", found, err)
	}
	if found, err := manager.Requested(t.Context(), "child"); found == nil || found.Operation != r.ID || err != nil {
		t.Fatal("receipt", found, err)
	}
	changed := *r.ExecutionCancel
	changed.Run = "different"
	if _, err := manager.CancelReviewed(t.Context(), changed, "operator", "another"); err == nil {
		t.Fatal("stale scope accepted")
	}
	child.Status = domain.ExecCancelled
	if err := app.Store.PutExecution(t.Context(), &child); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "GET", route+"/"+id, operatorTok, "", "", &v); code != 200 || v.Messages[1].Review.State != chat.ReviewApplied || !v.Messages[1].Unread {
		t.Fatal("observation", code, v.Messages[1])
	}
}
