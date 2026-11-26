package api_test

import (
	"context"

	"encoding/json"
	"fmt"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/taskrun"
	"net/http"
	"time"

	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"

	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type chatBuildFixture struct{ chatAgentFixture }

func (chatBuildFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var command, selector string
	if _, err := fmt.Sscanf(req.Messages[len(req.Messages)-1].Content, "%s %s", &command, &selector); err != nil {
		return nil, err
	}
	kind := "task.prepare_environment"
	if command == "Start" {
		kind = "task.start"
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(`{"kind":%q,"task":%q}`, kind, selector)}}, nil
}

func TestChatEnvironmentBuildRequiresAdministratorAndSeparateTaskStart(t *testing.T) {
	bin, _ := fakeDaemonCLI(t)
	srv, app := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatBuildFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	var graph domain.Graph
	if err := json.Unmarshal([]byte(taskGraph), &graph); err != nil {
		t.Fatal(err)
	}
	graph.Spec.Environment = &domain.EnvironmentSpec{Image: "alpine:3.22"}
	graph.Spec.Nodes[0].Isolation.Mode = domain.IsolationDocker
	g, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", string(g), nil); code != 201 {
		t.Fatal(code)
	}
	var in taskconfig.Input
	if err := json.Unmarshal([]byte(taskBody), &in); err != nil {
		t.Fatal(err)
	}
	in.ExecutionMode, in.ResultNodeID = "sandbox", in.NodeID
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var source taskconfig.Draft
	if code := call(t, srv, "POST", "/api/v1/tasks", adminTok, "application/json", string(raw), &source); code != 201 {
		t.Fatal(code)
	}
	propose := func(index int, token string) chat.Session {
		t.Helper()
		input := fmt.Sprintf(`{"requestId":"%032x","text":"Prepare %s"}`, index, source.ID)
		var session chat.Session
		if status := call(t, srv, "POST", "/api/v1/chat/sessions", token, "application/json", input, &session); status != http.StatusOK || len(session.Messages) != 2 {
			t.Fatalf("proposal HTTP %d %+v", status, session)
		}
		return session
	}
	confirm := func(pending chat.Session) chat.Session {
		t.Helper()
		review := pending.Messages[1].Review
		var session chat.Session
		path := fmt.Sprintf("/api/v1/chat/sessions/%s/operations/%s/confirm", pending.ID, review.ID)
		if status := call(t, srv, "POST", path, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &session); status != http.StatusOK {
			t.Fatalf("confirmation HTTP %d", status)
		}
		return session
	}

	if x := propose(1, operatorTok); x.Messages[1].Review != nil {
		t.Fatal("operator can prepare")
	}
	pending := propose(2, adminTok)
	stale := propose(3, adminTok)
	if r := pending.Messages[1].Review; r == nil || r.Environment == nil || r.Environment.Task != source.ID {
		t.Fatal(pending.Messages)
	}
	if r := confirm(pending).Messages[1].Review; r.State != chat.ReviewQueued && r.State != chat.ReviewApplied {
		t.Fatal(r)
	}
	waitFor(t, 5*time.Second, "environment receipt", func() bool {
		var result chat.Session
		return call(t, srv, "GET", "/api/v1/chat/sessions/"+pending.ID, adminTok, "", "", &result) == 200 && result.Messages[1].Review.State == chat.ReviewApplied && result.Messages[1].Unread
	})
	if r := confirm(pending).Messages[1].Review; r.State != chat.ReviewApplied {
		t.Fatal(r)
	}
	if r := confirm(stale).Messages[1].Review; r.State != chat.ReviewConflict {
		t.Fatal(r)
	}
	var progress taskrun.View
	if code := call(t, srv, "GET", "/api/v1/tasks/"+source.ID+"/progress", viewerTok, "", "", &progress); code != 200 || len(progress.Executions) != 0 || progress.Task.Status != "environment_pending" || progress.Environment == nil || !progress.Environment.UpToDate {
		t.Fatal(code, progress)
	}
	var start chat.Session
	if code := call(t, srv, "POST", "/api/v1/chat/sessions", adminTok, "application/json", fmt.Sprintf(`{"requestId":"%032x","text":"Start %s"}`, 4, source.ID), &start); code != 200 {
		t.Fatal(code)
	}
	if r := start.Messages[1].Review; r == nil || r.TaskStart == nil || r.TaskStart.Build == nil || r.TaskStart.Build.Hash != progress.Environment.Hash {
		t.Fatal(start.Messages)
	}
	for range 2 {
		if r := confirm(start).Messages[1].Review; r.State != chat.ReviewAccepted {
			t.Fatal(r)
		}
	}
	var admitted taskconfig.Draft
	if code := call(t, srv, "GET", "/api/v1/tasks/"+source.ID, adminTok, "", "", &admitted); code != 200 || admitted.StartOperation != start.Messages[1].Review.ID || admitted.ExecutionGraphID != progress.Task.ExecutionGraphID {
		t.Fatal(code, admitted)
	}
}
