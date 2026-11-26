package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/llm"
)

type chatScaleFixture struct{ chatAgentFixture }

func (chatScaleFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var workflow, size, mode string
	if _, err := fmt.Sscanf(req.Messages[len(req.Messages)-1].Content, "Scale reader in %s to %s using %s", &workflow, &size, &mode); err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
		`{"kind":"workflow.scale","scaleWorkflow":%q,"scaleNode":"reader","scaleSize":%q,"scaleMode":%q}`, workflow, size, mode)}}, nil
}

func TestChatScaleSeparatesRuntimeOverrideFromVersionedCeiling(t *testing.T) {
	srv, app := setupApp(t, nil)
	app.Models.Add(llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, chatScaleFixture{})
	ctx := t.Context()
	if _, err := app.Store.PutSetting(ctx, "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const template = `{"metadata":{"id":%q},"spec":{"nodes":[{"id":"reader","runtime":{"kind":"echo"},"loop":{"mode":"event"},"cluster":%s}, {"id":"observer","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],"channels":[{"id":"requests","to":["reader"]}]}}`
	for id, cluster := range map[string]string{"fixed-scale": `{"replicas":1}`, "auto-scale": `{"mode":"autoscale","minReplicas":1,"maxReplicas":3}`} {
		if status := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", fmt.Sprintf(template, id, cluster), nil); status != http.StatusCreated {
			t.Fatalf("create %s: %d", id, status)
		}
	}
	if code := call(t, srv, "POST", "/api/v1/graphs/fixed-scale/start", operatorTok, "", "", nil); code != http.StatusOK {
		t.Fatalf("start workflow fixture: %d", code)
	}
	const base = "/api/v1/chat/sessions"
	propose := func(index int, tok, id, size, mode string) chat.Session {
		t.Helper()
		body := fmt.Sprintf(`{"requestId":"%032x","text":%q}`, index, fmt.Sprintf("Scale reader in %s to %s using %s", id, size, mode))
		var session chat.Session
		if code := call(t, srv, "POST", base, tok, "application/json", body, &session); code != http.StatusOK || len(session.Messages) != 2 {
			t.Fatalf("propose %d: %d %+v", index, code, session)
		}
		return session
	}
	confirm := func(session chat.Session) *chat.OperationReview {
		t.Helper()
		review := session.Messages[1].Review
		if review == nil || review.WorkflowScale == nil {
			t.Fatalf("missing scale review: %+v", session.Messages[1])
		}
		path := fmt.Sprintf("%s/%s/operations/%s/confirm", base, session.ID, review.ID)
		var result chat.Session
		if code := call(t, srv, "POST", path, operatorTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &result); code != http.StatusOK {
			t.Fatalf("confirm: %d", code)
		}
		return result.Messages[1].Review
	}
	for _, denied := range []chat.Session{propose(1, viewerTok, "fixed-scale", "2", "fixed"), propose(2, operatorTok, "fixed-scale", "2", "autoscale"), propose(3, operatorTok, "auto-scale", "0", "autoscale")} {
		if denied.Messages[1].Review != nil {
			t.Fatalf("unsafe scope received review: %+v", denied.Messages[1])
		}
	}
	fixed := propose(4, operatorTok, "fixed-scale", "3", "fixed")
	if status, err := app.Engine.Status(ctx, "fixed-scale"); err != nil || status.Nodes[0].DesiredReplicas != 1 {
		t.Fatalf("proposing changed the count: %+v %v", status, err)
	}
	confirm(fixed)
	var settled *engine.WorkflowScaleObservation
	waitFor(t, 8*time.Second, "three active replicas in the reviewed run", func() bool {
		var result chat.Session
		if code := call(t, srv, "GET", base+"/"+fixed.ID, operatorTok, "", "", &result); code != http.StatusOK {
			t.Fatalf("read review: %d", code)
		}
		review := result.Messages[1].Review
		settled = review.ScaleResult
		return review.State == chat.ReviewApplied
	})
	if settled.State != "observed" || settled.Active != 3 || settled.Version != 1 {
		t.Fatalf("fixed result: %+v", settled)
	}
	if replay := confirm(fixed); replay.State != chat.ReviewApplied || *replay.ScaleResult != *settled {
		t.Fatalf("replay changed result: %+v", replay)
	}
	stale := propose(5, operatorTok, "fixed-scale", "4", "fixed")
	if code := call(t, srv, "PUT", "/api/v1/graphs/fixed-scale/nodes/reader/scale", operatorTok, "application/json", `{"replicas":2}`, nil); code != http.StatusOK {
		t.Fatalf("manual scale: %d", code)
	}
	if got := confirm(stale); got.State != chat.ReviewConflict {
		t.Fatalf("stale overwrote manual control: %+v", got)
	}
	if status, err := app.Engine.Status(ctx, "fixed-scale"); err != nil || status.Nodes[0].DesiredReplicas != 2 || status.Nodes[1].DesiredReplicas != 1 {
		t.Fatalf("wrong node or count: %+v %v", status, err)
	}
	stopped := propose(7, operatorTok, "fixed-scale", "0", "fixed")
	confirm(stopped)
	waitFor(t, 8*time.Second, "only the reviewed node drains to zero", func() bool {
		var session chat.Session
		if code := call(t, srv, "GET", base+"/"+stopped.ID, operatorTok, "", "", &session); code != http.StatusOK {
			t.Fatalf("read review: %d", code)
		}
		review := session.Messages[1].Review
		return review.State == chat.ReviewApplied && review.ScaleResult.State == "saved" && review.ScaleResult.Active == 0
	})
	if status, err := app.Engine.Status(ctx, "fixed-scale"); err != nil || status.DesiredState != domain.GraphRunning || !status.Nodes[0].Stopped || status.Nodes[1].Stopped {
		t.Fatalf("drain changed another node or the workflow: %+v %v", status, err)
	}
	auto := confirm(propose(6, operatorTok, "auto-scale", "2", "autoscale"))
	if auto.State != chat.ReviewApplied || auto.ScaleResult.Version != 2 || auto.ScaleResult.State != "saved" {
		t.Fatalf("autoscale result: %+v", auto)
	}
	graph, record, err := app.Engine.GetGraphDocument(ctx, "auto-scale", 0)
	if err != nil || graph.Node("reader").Cluster.MaxReplicas != 2 || record.DesiredState != domain.GraphStopped {
		t.Fatalf("autoscale unexpectedly activated or edited another field: %+v %+v %v", graph, record, err)
	}
	old, err := app.Store.GetGraph(ctx, "auto-scale", 1)
	if err != nil || old.Node("reader").Cluster.MaxReplicas != 3 {
		t.Fatalf("historical version: %+v %v", old, err)
	}
}
