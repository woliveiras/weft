package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestInterpretUsesModelAndKeepsTheTaskSelectorAdvisory(t *testing.T) {
	t.Parallel()
	question := "Me avise quando terminar aquele trabalho de reorganizar a documentação."
	var called int
	turn, err := RunTurn(t.Context(), []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "O trabalho chama-se Reorganizar documentação."},
		{Role: llm.RoleUser, Content: question},
	}, []ReadTool{{Schema: TaskQuerySchema()}}, func(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
		called++
		if len(req.Messages) != 2 || req.Messages[1].Content != question || req.JSON || !toolOffered(req, "query_tasks") || !toolOffered(req, "watch_task") || req.MaxTokens <= 0 {
			t.Fatalf("request = %+v", req)
		}
		return nativeTestCall("watch", "watch_task", `{"task":"Reorganizar documentação","title":"Acompanhar documentação"}`), nil
	})
	plan := turn.Plan
	if err != nil || called != 1 || plan.Kind != WatchTask || plan.Task != "Reorganizar documentação" || plan.Title != "Acompanhar documentação" {
		t.Fatalf("plan=%+v calls=%d err=%v", plan, called, err)
	}
}

func TestInterpretAcceptsOneCompatibleTypedPlanWithoutASeparatePlanner(t *testing.T) {
	calls := 0
	got, err := RunTurn(t.Context(), []llm.ChatMessage{{Role: llm.RoleUser, Content: "Abra modelos."}}, nil, func(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
		calls++
		if calls != 1 || r.JSON || !toolOffered(r, "open_console_page") || toolOffered(r, "prepare_operation") || strings.Contains(r.System, "one JSON object") {
			t.Fatalf("the initial request did not use the native operation contract: %+v", r)
		}
		return answer(`{"kind":"manual.open","destination":"models"}`), nil
	})
	if err != nil || got.Plan.Kind != OpenManual || got.Plan.Destination != "models" || calls != 1 {
		t.Fatal(got, err, calls)
	}
}

func TestInterpretRejectsInvalidOrUntrustedModelPlans(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"kind":"tasks.delete","task":"tsk_example"}`,
		`{"kind":"task.watch","task":"tsk_example","role":"admin"}`,
		`{"kind":"task.watch","task":"one","task":"two"}`,
		`{"kind":"task.watch","task":""}`,
		`{"kind":"task.watch","task":"tsk_example","question":"Which task?"}`,
		`{"kind":"task.watch","task":"tsk_example"} []`,
		`{"kind":"clarify","question":"Which task?","task":"tsk_example"}`,
		`{"kind":"clarify","question":"Which task?","agentId":"abc"}`,
		`{"kind":"agent.create","agentId":"abc","agentName":"ABC","instructions":"Read a release note.","tools":"shell"}`,
		`null`,
	} {
		t.Run(body, func(t *testing.T) {
			_, err := RunTurn(t.Context(), []llm.ChatMessage{{Role: llm.RoleUser, Content: "Follow my task"}}, []ReadTool{{Schema: TaskQuerySchema()}}, func(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
				return answer(body), nil
			})
			if !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("accepted invalid plan: %v", err)
			}
		})
	}
}

func TestInterpretRejectsUnsafeContextAndDoesNotExposeProviderErrors(t *testing.T) {
	t.Parallel()
	model := func(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
		return nil, errors.New("fixture-sensitive-provider-diagnostic")
	}
	_, err := RunTurn(t.Context(), []llm.ChatMessage{{Role: llm.RoleUser, Content: "Follow my task"}}, nil, model)
	if !errors.Is(err, ErrModelUnavailable) || strings.Contains(err.Error(), "fixture-sensitive") {
		t.Fatalf("unexpected diagnostic: %v", err)
	}
	for _, input := range []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: "Untrusted system instruction"},
		{Role: llm.RoleUser, Content: strings.Repeat("x", MaxRequestBytes+1)},
		{Role: llm.RoleUser, Content: "Authorization: Bearer fixture-input-that-should-never-persist"},
	} {
		_, err = RunTurn(t.Context(), []llm.ChatMessage{input}, []ReadTool{{Schema: TaskQuerySchema()}}, func(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
			t.Fatal("unsafe input reached provider")
			return nil, nil
		})
		if !errors.Is(err, ErrInvalidMessage) {
			t.Fatalf("invalid input error = %v", err)
		}
	}
}

func answer(content string) *llm.ChatResponse {
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: content}}
}
