package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

type chatWorkflowFixture struct{}

func (chatWorkflowFixture) Name() string { return "chat-fixture" }

func (chatWorkflowFixture) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }

func (chatWorkflowFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if !req.JSON || req.Model != "fixture" || len(req.Messages) == 0 {
		return nil, fmt.Errorf("unexpected model request")
	}
	target, version := "release-notes", "1"
	switch last := req.Messages[len(req.Messages)-1].Content; {
	case strings.Contains(last, "without a version"):
		version = ""
	case strings.Contains(last, "editor collision"):
		target = "editor-collision"
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
		`{"kind":"workflow.create","workflowId":%q,"workflowName":"Release notes","agent":"Release reader","agentVersion":%q}`, target, version)}}, nil
}

func TestChatWorkflowCreationPinsCatalogVersionAndChecksAbsence(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatWorkflowFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	const setting = `{"provider":"chat-fixture","name":"fixture"}`
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(setting), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	catalog := agentcatalog.Manager{Store: app.Store}
	definition := domain.AgentDefinition{
		Name: "Release reader", Prompt: "Read the original notes.", ReadOnly: true,
		Runtime: domain.RuntimeRef{Kind: "native"}, Model: domain.ModelRef{Provider: "chat-fixture", Name: "fixture"},
	}
	if _, err := catalog.Publish(t.Context(), "release-reader", 0, definition, "fixture"); err != nil {
		t.Fatal(err)
	}
	definition.Prompt = "Read only the revised notes."
	if _, err := catalog.Publish(t.Context(), "release-reader", 1, definition, "fixture"); err != nil {
		t.Fatal(err)
	}
	request := func(id int, token, text string) chat.Session {
		t.Helper()
		var session chat.Session
		body := fmt.Sprintf(`{"requestId":%q,"text":%q}`, fmt.Sprintf("%032x", id), text)
		if status := call(t, srv, "POST", "/api/v1/chat/sessions", token, "application/json", body, &session); status != http.StatusOK || len(session.Messages) != 2 {
			t.Fatalf("request: HTTP %d %+v", status, session)
		}
		return session
	}
	decide := func(session chat.Session, token, decision string) (int, chat.Session) {
		t.Helper()
		review := session.Messages[1].Review
		var result chat.Session
		path := fmt.Sprintf("/api/v1/chat/sessions/%s/operations/%s/%s", session.ID, review.ID, decision)
		return call(t, srv, "POST", path, token, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &result), result
	}
	viewer := request(1, viewerTok, "Please make a release notes workflow with version one of Release reader.")
	if message := viewer.Messages[1]; message.Review != nil || !strings.Contains(message.Text, "administrator") {
		t.Fatalf("viewer received an executable draft: %+v", message)
	}
	ambiguous := request(2, adminTok, "Create a release notes workflow with Release reader without a version.")
	if message := ambiguous.Messages[1]; message.Review != nil || !strings.Contains(message.Text, "Which version") {
		t.Fatalf("ambiguous version: %+v", message)
	}
	pending := request(3, adminTok, "Use version one of Release reader to make a release notes workflow.")
	review := pending.Messages[1].Review
	if review == nil || review.Workflow == nil || review.State != chat.ReviewAwaiting ||
		review.Workflow.Graph.Spec.Nodes[0].Agent.Version != 1 || review.AgentCreation != nil {
		t.Fatalf("unexpected workflow review: %+v", review)
	}
	const graphPath = "/api/v1/graphs/release-notes"
	if status := call(t, srv, "GET", graphPath, adminTok, "", "", nil); status != http.StatusNotFound {
		t.Fatalf("workflow changed during planning: %d", status)
	}
	if status, _ := decide(pending, viewerTok, "confirm"); status != http.StatusForbidden {
		t.Fatalf("viewer confirmed workflow: %d", status)
	}
	definition.Prompt = "A newer version published after review."
	if _, err := catalog.Publish(t.Context(), "release-reader", 2, definition, "fixture"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		status, result := decide(pending, adminTok, "confirm")
		if status != http.StatusOK || result.Messages[1].Review.State != chat.ReviewCreated || result.Messages[1].Review.ResultVersion != 1 {
			t.Fatalf("confirmation: %d %+v", status, result.Messages)
		}
	}
	var saved struct {
		Graph  domain.Graph       `json:"graph"`
		Record domain.GraphRecord `json:"record"`
	}
	if status := call(t, srv, "GET", graphPath, adminTok, "", "", &saved); status != http.StatusOK {
		t.Fatalf("saved workflow: %d", status)
	}
	spec := saved.Graph.Spec
	if saved.Graph.Metadata.Version != 1 || saved.Record.State != domain.GraphStopped || saved.Record.DesiredState != domain.GraphStopped ||
		len(spec.Nodes) != 1 || spec.Nodes[0].Agent == nil || spec.Nodes[0].Agent.ID != "release-reader" ||
		spec.Nodes[0].Agent.Version != 1 || spec.Nodes[0].Prompt != "Read the original notes." || spec.Task.ChannelID != "requests" {
		t.Fatalf("wrong saved target or state: %+v %+v", saved.Graph, saved.Record)
	}

	collision := request(4, adminTok, "Create a release notes workflow to test an editor collision, using version one.")
	if collision.Messages[1].Review == nil {
		t.Fatal("collision proposal missing")
	}
	const manual = `{"metadata":{"id":"editor-collision","name":"Saved manually"},"spec":{"nodes":[{"id":"a","runtime":{"kind":"echo"}}]}}`
	if status := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", manual, nil); status != http.StatusCreated {
		t.Fatalf("manual creation: %d", status)
	}
	status, result := decide(collision, adminTok, "confirm")
	if status != http.StatusOK || result.Messages[1].Review.State != chat.ReviewConflict {
		t.Fatalf("changed target was not fenced: %d %+v", status, result.Messages)
	}
	var manualRecord struct{ Graph domain.Graph }
	if status := call(t, srv, "GET", "/api/v1/graphs/editor-collision", adminTok, "", "", &manualRecord); status != http.StatusOK ||
		manualRecord.Graph.Metadata.Version != 1 || manualRecord.Graph.Metadata.Name != "Saved manually" {
		t.Fatalf("manual workflow was changed: %d %+v", status, manualRecord)
	}
}
