package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type chatBudgetFixture struct{ chatAgentFixture }

func (chatBudgetFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var task string
	if _, err := fmt.Sscanf(req.Messages[len(req.Messages)-1].Content, "Raise the current budget for %s", &task); err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
		`{"kind":"budget.raise","budgetScope":"run","budgetTarget":%q,"budgetAmount":"2.004005","budgetCurrency":"USD"}`, task)}}, nil
}

func TestChatBudgetRaiseUsesNativeHoldAndResume(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprintf("blocked-by-another-ledger-%t", blocked), func(t *testing.T) {
			srv, app := setupApp(t, nil)
			app.Models.Add(llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, chatBudgetFixture{})
			ctx := t.Context()
			if _, err := app.Store.PutSetting(ctx, "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
				t.Fatal(err)
			}
			put := func(path, body string) {
				t.Helper()
				if code := call(t, srv, "PUT", path, adminTok, "application/json", body, nil); code != http.StatusOK {
					t.Fatalf("put %s: %d", path, code)
				}
			}
			put("/api/v1/cost/prices", `{"currency":"USD","prices":[{"type":"mock","model":"*","inputMicrosPerMTok":1000000,"outputMicrosPerMTok":1000000}]}`)
			put("/api/v1/cost/policy", `{"taskRunBudgetMicros":1}`)
			const graph = `{"metadata":{"id":"budget-report"},"spec":{"nodes":[{"id":"reader","prompt":"Summarise the report.","runtime":{"kind":"native"},"model":{"provider":"mock"},"loop":{"mode":"once"},"tools":[]}]}}`
			if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); code != http.StatusCreated {
				t.Fatalf("create workflow: %d", code)
			}
			var draft taskconfig.Draft
			if code := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", `{"title":"Read report","request":"Summarize the report.","outcome":"understand","graphId":"budget-report","graphVersion":1,"nodeId":"reader","resultNodeId":"reader"}`, &draft); code != http.StatusCreated {
				t.Fatalf("create task: %d", code)
			}
			if code := taskStart(t, srv.URL, draft.ID, adminTok, draft.Revision); code != http.StatusAccepted {
				t.Fatalf("start: %d", code)
			}
			budgets := cost.Budgets{Store: app.Store, Bus: app.Bus}
			var execution, runID string
			waitFor(t, 10*time.Second, "native budget holds the current run", func() bool {
				live, err := (taskconfig.Manager{Store: app.Store}).Get(ctx, draft.ID)
				if err != nil || live.ExecutionGraphID == "" {
					return false
				}
				execution = live.ExecutionGraphID
				record, err := app.Store.GetGraphRecord(ctx, execution)
				if err != nil || record.State != domain.GraphPaused || record.RunID == "" {
					return false
				}
				runID = record.RunID
				view, err := budgets.Ledger(ctx, cost.TaskRun, execution)
				return err == nil && view != nil && view.Paused && view.Reached
			})
			if blocked {
				if _, err := budgets.SetDefinitions(ctx, []cost.Definition{{Scope: cost.Workflow, ID: "budget-report", LimitMicros: 1}}, "admin", 0); err != nil {
					t.Fatal(err)
				}
				_, err := budgets.Reserve(ctx, cost.Target{Workflow: "budget-report", Execution: "test-charge"}, func(ctx context.Context) (cost.Catalogue, error) {
					catalogue, _, err := (cost.Prices{Store: app.Store}).Get(ctx)
					return catalogue, err
				}, "mock", "mock", &llm.ChatRequest{Model: "fixture", MaxTokens: 10})
				if !errors.Is(err, cost.ErrBudgetReached) {
					t.Fatalf("second ledger should already be exhausted: %v", err)
				}
			}
			const path = "/api/v1/chat/sessions"
			propose := func(index int, actor string) chat.Session {
				t.Helper()
				var session chat.Session
				if code := call(t, srv, "POST", path, actor, "application/json", fmt.Sprintf(`{"requestId":"%032x","text":"Raise the current budget for %s"}`, index, draft.ID), &session); code != http.StatusOK || len(session.Messages) != 2 {
					t.Fatalf("review: %d %+v", code, session)
				}
				return session
			}
			if proposed := propose(1, operatorTok); proposed.Messages[1].Review != nil {
				t.Fatal("operator received an administrative budget review")
			}
			proposed := propose(2, adminTok)
			review := proposed.Messages[1].Review
			if review == nil || review.BudgetRaise == nil || review.BudgetRaise.Target.ID != execution || review.BudgetRaise.Actor != "admin" || review.BudgetRaise.Limit != 2_004_005 {
				t.Fatalf("wrong prepared budget: %+v", review)
			}
			if ledger, err := budgets.Ledger(ctx, cost.TaskRun, execution); err != nil || ledger.Limit != 1 {
				t.Fatalf("proposal mutated budget: %+v %v", ledger, err)
			}
			confirm := fmt.Sprintf("%s/%s/operations/%s/confirm", path, proposed.ID, review.ID)
			body := fmt.Sprintf(`{"digest":%q}`, review.Digest)
			for range 2 {
				if code := call(t, srv, "POST", confirm, adminTok, "application/json", body, nil); code != http.StatusOK {
					t.Fatalf("confirm: %d", code)
				}
			}
			var outcome string
			waitFor(t, 12*time.Second, "Chat observes the exact resumed or blocked run", func() bool {
				var fetched chat.Session
				if code := call(t, srv, "GET", path+"/"+proposed.ID, adminTok, "", "", &fetched); code != http.StatusOK {
					t.Fatalf("get session: %d", code)
				}
				review := fetched.Messages[1].Review
				if result := review.BudgetResult; result != nil && len(result.Resumes) == 1 {
					if result.Resumes[0].Run != runID || result.Resumes[0].Graph != execution || result.Revision < 2 {
						t.Fatalf("wrong resumption: %+v", result)
					}
					outcome = result.Resumes[0].State
				}
				return review.State == chat.ReviewApplied
			})
			if blocked && outcome != "budget_reached" || !blocked && outcome != "resumed" && outcome != "run_ended" {
				t.Fatalf("unexpected resume status: %q", outcome)
			}
			ledger, err := budgets.Ledger(ctx, cost.TaskRun, execution)
			if err != nil || ledger.Limit != 2_004_005 || ledger.RaisedBy != "admin" {
				t.Fatalf("ledger: %+v %v", ledger, err)
			}
			policy, _, err := budgets.Policy(ctx)
			if err != nil || policy.TaskRunBudgetMicros != 1 {
				t.Fatalf("task default changed: %+v %v", policy, err)
			}
			events, err := app.Store.ListEvents(ctx, storage.EventFilter{GraphID: execution, Types: []domain.EventType{domain.EvBudgetRaised}})
			if err != nil || len(events) != 1 || events[0].Actor != "admin" {
				t.Fatalf("one human raise audit expected: %+v %v", events, err)
			}
		})
	}
}
