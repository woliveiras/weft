package api_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
)

type chatWorkflowBuildFixture struct{ chatAgentFixture }

func (chatWorkflowBuildFixture) Chat(_ context.Context, request *llm.ChatRequest) (*llm.ChatResponse, error) {
	var target string
	if _, err := fmt.Sscanf(request.Messages[len(request.Messages)-1].Content, "Build the environment for %s", &target); err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
		`{"kind":"workflow.prepare_environment","environmentWorkflow":%q}`, target)}}, nil
}

func TestChatWorkflowEnvironmentBuildsReviewedVersionAndNeverStartsWorkflow(t *testing.T) {
	bin, log := fakeDaemonCLI(t)
	srv, app := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	app.Models.Add(llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, chatWorkflowBuildFixture{})
	ctx := t.Context()
	if _, err := app.Store.PutSetting(ctx, "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const workflow = "/api/v1/graphs/env-demo"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", envGraph, nil); code != http.StatusCreated {
		t.Fatalf("create %d", code)
	}
	const base = "/api/v1/chat/sessions"
	propose := func(i int, token string) chat.Session {
		t.Helper()
		var session chat.Session
		body := fmt.Sprintf(`{"requestId":"%032x","text":"Build the environment for env-demo"}`, i)
		if status := call(t, srv, "POST", base, token, "application/json", body, &session); status != http.StatusOK || len(session.Messages) != 2 {
			t.Fatalf("review: %d %+v", status, session)
		}
		return session
	}
	confirm := func(session chat.Session) *chat.OperationReview {
		t.Helper()
		review := session.Messages[1].Review
		if review == nil || review.WorkflowEnvironment == nil {
			t.Fatalf("no workflow build review: %+v", session.Messages[1])
		}
		path := fmt.Sprintf("%s/%s/operations/%s/confirm", base, session.ID, review.ID)
		var result chat.Session
		if code := call(t, srv, "POST", path, operatorTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &result); code != http.StatusOK {
			t.Fatalf("confirm: %d", code)
		}
		return result.Messages[1].Review
	}
	if denied := propose(1, viewerTok); denied.Messages[1].Review != nil {
		t.Fatal("viewer received an executable build review")
	}
	stale := propose(2, operatorTok)
	if view := stale.Messages[1].Review; view.WorkflowEnvironment.Version != 1 || view.WorkflowEnvironment.Workflow != "env-demo" || view.WorkflowEnvironment.DefinitionHash == "" {
		t.Fatalf("review missing exact definition: %+v", view)
	}
	updated := strings.Replace(envGraph, `["go version"]`, `["go version", "go env"]`, 1)
	if code := call(t, srv, "PUT", workflow, adminTok, "application/yaml", updated, nil); code != http.StatusOK {
		t.Fatalf("manual edit: %d", code)
	}
	if r := confirm(stale); r.State != chat.ReviewConflict {
		t.Fatalf("stale definition accepted: %+v", r)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("stale review called the daemon: %v", err)
	}
	fresh := propose(3, operatorTok)
	r := confirm(fresh)
	if r.State != chat.ReviewQueued && r.State != chat.ReviewApplied {
		t.Fatalf("not accepted: %+v", r)
	}
	waitFor(t, 8*time.Second, "observe the reviewed workflow environment", func() bool {
		var session chat.Session
		return call(t, srv, "GET", base+"/"+fresh.ID, operatorTok, "", "", &session) == http.StatusOK && session.Messages[1].Review.State == chat.ReviewApplied
	})
	if r := confirm(fresh); r.State != chat.ReviewApplied {
		t.Fatalf("replay changed build outcome: %+v", r)
	}
	if reused := confirm(propose(4, operatorTok)); reused.State != chat.ReviewApplied {
		t.Fatalf("cached build could not be reused: %+v", reused)
	}
	var native environment.View
	if code := call(t, srv, "GET", workflow+"/environment", viewerTok, "", "", &native); code != http.StatusOK || !native.UpToDate ||
		native.BuildOperation != "" || native.Hash != r.WorkflowEnvironment.DefinitionHash {
		t.Fatalf("native build receipt: %d %+v", code, native)
	}
	graph, _, err := app.Engine.GetGraph(ctx, "env-demo", 0)
	if err != nil {
		t.Fatal(err)
	}
	internal, err := app.API.Environments.Get(ctx, graph)
	if err != nil || internal.BuildOperation != r.ID {
		t.Fatalf("build operation was not durably recorded: %+v %v", internal, err)
	}
	record, err := app.Store.GetGraphRecord(ctx, "env-demo")
	if err != nil || record.RunID != "" || record.DesiredState != domain.GraphStopped || record.Version != 2 {
		t.Fatalf("workflow unexpectedly ran or changed: %+v %v", record, err)
	}
	events, err := app.Store.ListEvents(ctx, storage.EventFilter{GraphID: "env-demo", Types: []domain.EventType{domain.EvEnvBuildStarted}})
	if err != nil || len(events) != 1 || events[0].Actor != "operator" {
		t.Fatalf("expected one audited operator build: %+v %v", events, err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	failure := strings.Replace(updated, "golang:1.26", "alpine:3.22", 1)
	if code := call(t, srv, "PUT", workflow, adminTok, "application/yaml", failure, nil); code != http.StatusOK {
		t.Fatalf("save failing fixture: %d", code)
	}
	failedBuild := propose(5, operatorTok)
	confirm(failedBuild)
	waitFor(t, 8*time.Second, "persist the failing build outcome", func() bool {
		var session chat.Session
		return call(t, srv, "GET", base+"/"+failedBuild.ID, operatorTok, "", "", &session) == http.StatusOK && session.Messages[1].Review.State == chat.ReviewIncomplete
	})
	if code := call(t, srv, "GET", workflow+"/environment", viewerTok, "", "", &native); code != http.StatusOK || native.State != environment.StateFailed || native.UpToDate {
		t.Fatalf("failed native build was reported ready: %d %+v", code, native)
	}
}
