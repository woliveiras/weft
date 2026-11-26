package api_test

import (
	"context"
	"fmt"
	"github.com/woliveiras/weft/server/internal/api"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/llm"
)

type chatAgentFixture struct{}

func (chatAgentFixture) Name() string { return "chat-fixture" }

func (chatAgentFixture) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }

func (chatAgentFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if !req.JSON || req.Model != "fixture" || len(req.Messages) == 0 {
		return nil, fmt.Errorf("unexpected request for the Chat fixture")
	}
	id := "release-reader"
	if strings.Contains(req.Messages[len(req.Messages)-1].Content, "change calendar") {
		id = "calendar-reader"
	}
	return &llm.ChatResponse{
		Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
			`{"kind":"agent.create","agentId":%q,"agentName":"Reader","instructions":"Summarize the reviewed notes."}`, id)},
		Usage: llm.Usage{InputTokens: 30, OutputTokens: 20},
	}, nil
}

func TestChatAgentReviewCreatesOneCatalogVersion(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatAgentFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	const setting = `{"provider":"chat-fixture","name":"fixture"}`
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(setting), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	if status := call(t, srv, "PUT", "/api/v1/cost/prices", adminTok, "application/json",
		`{"currency":"USD","unpriced":"refuse","prices":[]}`, nil); status != http.StatusOK {
		t.Fatalf("fixture price policy: %d", status)
	}
	request := func(id, text string) chat.Session {
		t.Helper()
		var session chat.Session
		if status := call(t, srv, "POST", "/api/v1/chat/sessions", adminTok, "application/json", fmt.Sprintf(`{"requestId":%q,"text":%q}`, id, text), &session); status != http.StatusOK {
			t.Fatalf("request: %d", status)
		}
		if len(session.Messages) != 2 || session.Messages[1].Review == nil {
			t.Fatalf("missing review: %+v", session)
		}
		return session
	}
	requestReview := request("2a154ae9cee7436ca37f848ef78c410e", "Create a release reader to summarize notes.")
	review := requestReview.Messages[1].Review
	if review.State != chat.ReviewAwaiting || !review.Definition.ReadOnly || len(review.Definition.Tools) != 0 {
		t.Fatalf("invalid reviewed settings: %+v", review)
	}
	agentPath := "/api/v1/agents/" + review.AgentID
	if status := call(t, srv, "GET", agentPath, adminTok, "", "", nil); status != http.StatusNotFound {
		t.Fatalf("operation ran before confirmation: %d", status)
	}
	confirmPath := "/api/v1/chat/sessions/" + requestReview.ID + "/operations/" + review.ID + "/confirm"
	if status := call(t, srv, "POST", confirmPath, viewerTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), nil); status != http.StatusForbidden {
		t.Fatalf("viewer confirmed: %d", status)
	}
	if status := call(t, srv, "POST", confirmPath, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, strings.Repeat("a", 64)), nil); status != http.StatusConflict {
		t.Fatalf("wrong review digest confirmed: %d", status)
	}

	send := func(id, text string) chat.Session {
		t.Helper()
		var session chat.Session
		path := "/api/v1/chat/sessions/" + requestReview.ID + "/messages"
		if status := call(t, srv, "POST", path, adminTok, "application/json", fmt.Sprintf(`{"requestId":%q,"text":%q}`, id, text), &session); status != http.StatusOK {
			t.Fatalf("message: %d", status)
		}
		return session
	}
	second := send("2a154ae9cee7436ca37f848ef78c420e", "Create a change calendar reader.")
	declined := second.Messages[len(second.Messages)-1].Review
	if declined == nil {
		t.Fatal("second proposal missing")
	}
	pending := send("2a154ae9cee7436ca37f848ef78c430e", "Confirm this proposal")
	if pending.Messages[1].Review.State != chat.ReviewAwaiting ||
		!strings.Contains(pending.Messages[len(pending.Messages)-1].Text, "does not identify one active proposal") {
		t.Fatalf("ambiguous confirmation: %+v", pending.Messages)
	}
	declinePath := "/api/v1/chat/sessions/" + requestReview.ID + "/operations/" + declined.ID + "/decline"
	if status := call(t, srv, "POST", declinePath, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, declined.Digest), nil); status != http.StatusOK {
		t.Fatalf("decline: %d", status)
	}
	if status := call(t, srv, "GET", "/api/v1/agents/"+declined.AgentID, adminTok, "", "", nil); status != http.StatusNotFound {
		t.Fatalf("declined review created an agent: %d", status)
	}
	conversation := send("2a154ae9cee7436ca37f848ef78c440e", "Confirme esta proposta.")
	if conversation.Messages[1].Review.State != chat.ReviewCreated {
		t.Fatalf("text confirmation: %+v", conversation.Messages[1].Review)
	}
	for range 2 {
		var settled chat.Session
		if status := call(t, srv, "POST", confirmPath, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &settled); status != http.StatusOK ||
			settled.Messages[1].Review.State != chat.ReviewCreated || settled.Messages[1].Review.ResultVersion != 1 {
			t.Fatalf("confirmation: %d %+v", status, settled.Messages)
		}
	}
	var added agentcatalog.Agent
	if status := call(t, srv, "GET", agentPath, adminTok, "", "", &added); status != http.StatusOK || len(added.Versions) != 1 || added.Versions[0].Operation != review.ID {
		t.Fatalf("catalog creation: %d %+v", status, added)
	}

	stale := request("792b1e80ce56b4b50a83981d63b1a911", "Create a reader to summarize the change calendar.")
	review = stale.Messages[1].Review
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(setting), 1, "fixture"); err != nil {
		t.Fatal(err)
	}
	var result chat.Session
	confirmPath = "/api/v1/chat/sessions/" + stale.ID + "/operations/" + review.ID + "/confirm"
	if status := call(t, srv, "POST", confirmPath, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &result); status != http.StatusOK ||
		result.Messages[1].Review.State != chat.ReviewConflict {
		t.Fatalf("stale model review: %d %+v", status, result.Messages)
	}
	if status := call(t, srv, "GET", "/api/v1/agents/"+review.AgentID, adminTok, "", "", nil); status != http.StatusNotFound {
		t.Fatalf("stale review created an agent: %d", status)
	}

	if status := call(t, srv, "PUT", "/api/v1/cost/budgets", adminTok, "application/json",
		`{"budgets":[{"scope":"user","id":"admin","limitMicros":100}]}`, nil); status != http.StatusOK {
		t.Fatalf("fixture budget: %d", status)
	}
	var budgetBlocked chat.Session
	if status := call(t, srv, "POST", "/api/v1/chat/sessions", adminTok, "application/json",
		`{"requestId":"2a154ae9cee7436ca37f848ef78c450e","text":"Create a change calendar reader."}`, &budgetBlocked); status != http.StatusOK ||
		len(budgetBlocked.Messages) != 2 || budgetBlocked.Messages[0].State != "unavailable" || budgetBlocked.Messages[1].Review != nil {
		t.Fatalf("unpriced with budget: %d %+v", status, budgetBlocked.Messages)
	}
}

func TestHistoricalChatOperationUsesCurrentRoleOfSamePrincipal(t *testing.T) {
	srv, app := setupApp(t, nil)
	deps := app.API.Deps
	auth, err := api.NewAuthenticator(config.Auth{Tokens: []config.Token{{Name: "admin", Token: operatorTok, Role: config.RoleOperator}}})
	if err != nil {
		t.Fatal(err)
	}
	deps.Auth = auth
	downgraded := httptest.NewServer(api.New(deps).Handler())
	defer downgraded.Close()
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: chatAgentFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const id = "3e6251d203394743abbc6250fc5fc3ff"
	const route = "/api/v1/chat/sessions"
	var original chat.Session
	if code := call(t, srv, "POST", route, adminTok, "application/json", `{"requestId":"`+id+`","text":"Create a release reader."}`, &original); code != 200 || len(original.Messages) != 2 || original.Messages[1].Review == nil {
		t.Fatal(code, original)
	}
	var lower chat.Session
	if code := call(t, downgraded, "GET", route+"/"+id, operatorTok, "", "", &lower); code != 200 || len(lower.Messages) != 2 || lower.Messages[1].Review != nil || lower.Messages[0].Text != original.Messages[0].Text {
		t.Fatal(code, lower)
	}
	var restored chat.Session
	if code := call(t, srv, "GET", route+"/"+id, adminTok, "", "", &restored); code != 200 || restored.Messages[1].Review == nil {
		t.Fatal(code, restored)
	}
}
