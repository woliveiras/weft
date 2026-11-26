package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
)

type chatFollowUpFixture struct{ chatAgentFixture }

func (f chatFollowUpFixture) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if req.Messages[len(req.Messages)-1].Content != "Where can I see the agent I created?" {
		return f.chatAgentFixture.Chat(ctx, req)
	}
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant && m.Content == "Weft observed catalog agent release-reader at version 1." {
			return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: `{"kind":"manual.open","destination":"agents"}`}}, nil
		}
	}
	return nil, fmt.Errorf("missing observed catalog reference")
}

func TestChatFollowUpCarriesConfirmedAgentIntoNextRequest(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: chatFollowUpFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const route = "/api/v1/chat/sessions"
	const id = "abc2e80ce56b4b50a83981d63b1a811b"
	var result chat.Session
	if status := call(t, srv, "POST", route, adminTok, "application/json", `{"requestId":"`+id+`","text":"Create a release reader."}`, &result); status != http.StatusOK || len(result.Messages) != 2 || result.Messages[1].Review == nil {
		t.Fatalf("proposal %d", status)
	}
	r := result.Messages[1].Review
	if status := call(t, srv, "POST", route+"/"+id+"/operations/"+r.ID+"/confirm", adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, r.Digest), &result); status != http.StatusOK || result.Messages[1].Review.State != chat.ReviewCreated {
		t.Fatalf("confirm %d %+v", status, result)
	}
	if status := call(t, srv, "POST", route+"/"+id+"/messages", adminTok, "application/json", `{"requestId":"a222e80ce56b4b50a83981d63b1a811b","text":"Where can I see the agent I created?"}`, &result); status != http.StatusOK || result.Messages[len(result.Messages)-1].Destination != "agents" {
		t.Fatalf("follow-up %d %+v", status, result)
	}
}

type inventoryFixture struct{ chatAgentFixture }

func (inventoryFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant && m.Content == "Weft listed workflows in this order: 1=task-fixture." {
			return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: `{"kind":"task.create","taskTitle":"Review examples","workflow":"task-fixture","request":"Check the examples.","outcome":"understand"}`}}, nil
		}
	}
	return nil, fmt.Errorf("missing ordered inventory")
}
func TestChatCanUseFirstWorkflowFromItsOwnInventory(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: inventoryFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); c != 201 {
		t.Fatal(c)
	}
	const route = "/api/v1/chat/sessions"
	const id = "a836e80ce56b4b50a83981d63b1a811b"
	var session chat.Session
	if c := call(t, srv, "POST", route, operatorTok, "application/json", `{"requestId":"`+id+`","text":"/inspect workflows"}`, &session); c != 200 || session.Messages[1].Inventory == nil {
		t.Fatal(c, session)
	}
	if c := call(t, srv, "POST", route+"/"+id+"/messages", operatorTok, "application/json", `{"requestId":"b346e80ce56b4b50a83981d63b1a811b","text":"Use the first one to prepare a task that checks the examples."}`, &session); c != 200 || len(session.Messages) != 4 || session.Messages[3].Review == nil || session.Messages[3].Review.TaskDraft.Input.GraphID != "task-fixture" {
		t.Fatal(c, session)
	}
}
