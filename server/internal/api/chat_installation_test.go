package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
)

type installationChatFixture struct {
	chatAgentFixture
	document string
}

func (m installationChatFixture) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	raw, err := json.Marshal(map[string]string{"kind": "installation.plan", "document": m.document, "title": "Review cost settings"})
	if err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: string(raw)}}, nil
}

type chatConfigFailStore struct {
	*configFailStore
	storage.ChatIndex
}

func TestChatInstallationAppliesOnlyReviewedObjectsAndRecordsPartialResults(t *testing.T) {
	srv, app := setupApp(t, nil)
	doc := strings.Join([]string{
		yamlObject("CostPolicy", "installation", "  taskRunBudgetMicros: 2000000\n  warningPercent: 80"),
		yamlObject("Budget", "installation/*", "  scope: installation\n  id: '*'\n  limitMicros: 10000000"),
		yamlObject("NetworkPolicy", "installation", "  setupNetworks: [none]\n  runNetworks: [none]\n  egressHosts: []"),
	}, "---\n")
	model := installationChatFixture{document: doc}
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: model}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	fail := &configFailStore{Store: app.Store}
	app.API.Store = &chatConfigFailStore{fail, app.Store.(storage.ChatIndex)}
	request := func(id, token string) *chat.OperationReview {
		t.Helper()
		var s chat.Session
		if status := call(t, srv, "POST", "/api/v1/chat/sessions", token, "application/json", fmt.Sprintf(`{"requestId":%q,"text":"Set the reviewed task, network and budget defaults."}`, id), &s); status != http.StatusOK || len(s.Messages) != 2 {
			t.Fatalf("submit: %d %+v", status, s.Messages)
		}
		return s.Messages[1].Review
	}
	const id = "6b3dc34cbdc147559e2666145612f199"
	if r := request(id, viewerTok); r != nil {
		t.Fatal("viewer received an administrative plan")
	}
	r := request(id, adminTok)
	if r == nil || r.Configuration == nil || len(r.Configuration.Objects) != 3 {
		t.Fatalf("missing configuration review: %+v", r)
	}
	budgets := cost.Budgets{Store: app.Store}
	policy, _, err := budgets.Policy(ctx)
	if err != nil || policy.TaskRunBudgetMicros == 2000000 {
		t.Fatalf("dry plan effect or error: %+v %v", policy, err)
	}
	confirm := func(session string, review *chat.OperationReview) *chat.OperationReview {
		t.Helper()
		var s chat.Session
		path := "/api/v1/chat/sessions/" + session + "/operations/" + review.ID + "/confirm"
		if status := call(t, srv, "POST", path, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &s); status != 200 {
			t.Fatalf("confirm: %d", status)
		}
		return s.Messages[1].Review
	}
	fail.fail = true
	settled := confirm(id, r)
	if settled.State != chat.ReviewIncomplete || len(settled.ConfigReceipts) != 3 ||
		settled.ConfigReceipts[0].Result != "applied" || settled.ConfigReceipts[1].Result != "failed" || settled.ConfigReceipts[2].Result != "not-applied" {
		t.Fatalf("partial: %+v", settled)
	}
	fail.fail = false
	if confirm(id, r).State != chat.ReviewIncomplete {
		t.Fatal("confirmation replay repeated effects")
	}
	const second = "2b3dc34cbdc147559e2666145612f128"
	fresh := request(second, adminTok)
	if fresh == nil || fresh.Configuration.Objects[0].Action != "unchanged" {
		t.Fatalf("new plan omitted prior effect: %+v", fresh)
	}
	if status := call(t, srv, "PUT", "/api/v1/cost/policy", adminTok, "application/json", `{"taskRunBudgetMicros":3000000,"warningPercent":80}`, nil); status != 200 {
		t.Fatalf("manual edit: %d", status)
	}
	if got := confirm(second, fresh); got.State != chat.ReviewConflict {
		t.Fatalf("stale result: %+v", got)
	}
	const third = "3b3dc34cbdc147559e2666145612f128"
	final := confirm(third, request(third, adminTok))
	if final.State != chat.ReviewApplied || len(final.ConfigReceipts) != 3 {
		t.Fatalf("fresh apply: %+v", final)
	}
}
