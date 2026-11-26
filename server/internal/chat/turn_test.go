package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
)

func taskCall(id, arguments string) *llm.ChatResponse {
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
		ID: id, Name: "query_tasks", Arguments: json.RawMessage(arguments), ProviderData: json.RawMessage(`{"signature":"opaque-fixture"}`),
	}}}, StopReason: "tool_calls"}
}

func TestTurnModelCanCompareTwoObservedResults(t *testing.T) {
	turns := []llm.ChatMessage{{Role: llm.RoleUser, Content: "O que foi mais frequente, sucesso ou falha?"}}
	catalog := []ReadTool{{Schema: TaskQuerySchema(), Read: func(_ context.Context, args json.RawMessage) (ToolObservation, error) {
		query, err := ParseTaskQuery(args)
		if err != nil {
			return ToolObservation{}, err
		}
		total := map[string]int{"succeeded": 11, "failed": 4}[query.Status]
		data, err := json.Marshal(TaskQueryResult{Mode: query.Mode, Status: query.Status, Total: total})
		return ToolObservation{Data: data, Title: "Resultados das tarefas", Destination: "tasks"}, err
	}}}
	step := 0
	result, err := RunTurn(t.Context(), turns, catalog, func(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
		step++
		if r.JSON || len(r.Tools) < 1 || r.Tools[0].Name != "query_tasks" || r.Messages[0].Content != turns[0].Content {
			t.Fatalf("step %d unexpected envelope", step)
		}
		last := r.Messages[len(r.Messages)-1]
		switch step {
		case 1:
			return taskCall("first", `{"mode":"count","status":"succeeded"}`), nil
		case 2, 3:
			wantID, wantTotal := "first", 11
			if step == 3 {
				wantID, wantTotal = "second", 4
			}
			origin := r.Messages[len(r.Messages)-2]
			if len(origin.ToolCalls) != 1 || string(origin.ToolCalls[0].ProviderData) != `{"signature":"opaque-fixture"}` || last.Role != llm.RoleTool || last.ToolCallID != wantID || !strings.Contains(last.Content, fmt.Sprintf(`"total":%d`, wantTotal)) {
				t.Fatalf("round %d: %+v", step, r.Messages)
			}
			if step == 2 {
				return taskCall("second", `{"mode":"count","status":"failed"}`), nil
			}
			return answer("Houve mais sucessos (11) do que falhas (4)."), nil
		default:
			return nil, errors.New("unexpected extra round")
		}
	})
	if err != nil || result.Text != "Houve mais sucessos (11) do que falhas (4)." || result.Plan.Kind != "" || result.Inventory != nil || step != 3 {
		t.Fatalf("result=%+v steps=%d err=%v", result, step, err)
	}
}

func TestTurnLimitsToolsAndRejectsUnverifiedAnswers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		want  error
		reads int
	}{
		{"unknown tool", ErrInvalidPlan, 0},
		{"more calls than allowed", ErrInvalidPlan, 3},
		{"duplicate in batch", ErrInvalidPlan, 0},
		{"tool unavailable", ErrReadUnavailable, 1},
		{"provider truncated", ErrTruncated, 0},
		{"filtered", ErrInvalidPlan, 0},
		{"unsafe final", ErrInvalidPlan, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			tool := ReadTool{Schema: TaskQuerySchema(), Read: func(context.Context, json.RawMessage) (ToolObservation, error) {
				reads++
				if tc.name == "tool unavailable" {
					return ToolObservation{}, ErrReadUnavailable
				}
				return ToolObservation{Data: json.RawMessage(`{"total":0}`)}, nil
			}}
			_, err := RunTurn(t.Context(), []llm.ChatMessage{{Role: llm.RoleUser, Content: "Quantas tarefas existem?"}}, []ReadTool{tool}, func(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
				resp := taskCall("id", `{"mode":"count"}`)
				switch tc.name {
				case "unknown tool":
					resp.Message.ToolCalls[0].Name = "write"
				case "duplicate in batch":
					resp.Message.ToolCalls = append(resp.Message.ToolCalls, resp.Message.ToolCalls[0])
				case "provider truncated":
					resp.StopReason = "length"
				case "filtered":
					resp.StopReason = "content_filter"
				case "unsafe final":
					if reads > 0 {
						return answer("Authorization: Bearer fixture-private-output-secret"), nil
					}
				case "more calls than allowed":
					if reads == 3 && len(r.Tools) != 0 {
						t.Fatal("tools offered after the read limit")
					}
				}
				return resp, nil
			})
			if !errors.Is(err, tc.want) || reads != tc.reads {
				t.Fatalf("read %d, err %v", reads, err)
			}
		})
	}
}

func TestTurnBroadTaskProposalRequestsNativeArguments(t *testing.T) {
	step := 0
	tool := ReadTool{Schema: TaskQuerySchema(), Read: func(context.Context, json.RawMessage) (ToolObservation, error) {
		return ToolObservation{Data: json.RawMessage(`{"mode":"exists","status":"running","total":0}`)}, nil
	}}
	got, err := RunTurn(t.Context(), []llm.ChatMessage{{Role: llm.RoleUser, Content: "Tem algo em andamento?"}}, []ReadTool{tool}, func(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
		step++
		switch step {
		case 1:
			return answer(`{"kind":"console.inspect","inspectArea":"tasks"}`), nil
		case 2:
			return taskCall("q", `{"mode":"exists","status":"running"}`), nil
		case 3:
			return answer("Nenhuma tarefa em andamento."), nil
		}
		return nil, errors.New("unexpected extra round")
	})
	if err != nil || step != 3 || got.Text != "Nenhuma tarefa em andamento." {
		t.Fatalf("%+v rounds=%d err=%v", got, step, err)
	}
}

func TestTurnLetsModelCorrectInvalidArgumentsBeforeAnswer(t *testing.T) {
	for _, correct := range []bool{false, true} {
		rounds := 0
		tool := ReadTool{Schema: TaskQuerySchema(), Read: func(_ context.Context, args json.RawMessage) (ToolObservation, error) {
			if _, err := ParseTaskQuery(args); err != nil {
				return ToolObservation{}, err
			}
			return ToolObservation{Data: json.RawMessage(`{"total":12}`)}, nil
		}}
		result, err := RunTurn(t.Context(), []llm.ChatMessage{{Role: llm.RoleUser, Content: "Quantas tiveram sucesso?"}}, []ReadTool{tool}, func(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
			rounds++
			switch rounds {
			case 1:
				return taskCall("invalid", `{"mode":"count","status":"success"}`), nil
			case 2:
				last := r.Messages[len(r.Messages)-1]
				if last.Role != llm.RoleTool || last.ToolCallID != "invalid" || !strings.Contains(last.Content, "invalid_arguments") {
					t.Fatal(last)
				}
				if correct {
					return taskCall("corrected", `{"mode":"count","status":"succeeded"}`), nil
				}
			}
			return answer("12 tarefas."), nil
		})
		if correct && (err != nil || rounds != 3 || result.Text != "12 tarefas.") {
			t.Fatal(result, err)
		}
		if !correct && (!errors.Is(err, ErrInvalidPlan) || result.Text != "") {
			t.Fatal("accepted answer without evidence", result, err)
		}
	}
}

func TestTurnHandsOtherOperationsToDomainPlanner(t *testing.T) {
	question := "Crie um agente para ler as notas de versão."
	step := 0
	got, err := RunTurn(t.Context(), []llm.ChatMessage{{Role: llm.RoleUser, Content: question}}, []ReadTool{{Schema: TaskQuerySchema()}}, func(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
		step++
		switch step {
		case 1:
			if len(r.Tools) != 2 || r.Tools[1].Name != "prepare_operation" || r.JSON {
				t.Fatal("missing native handoff")
			}
			return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{Name: "prepare_operation", ID: "plan", Arguments: json.RawMessage(`{}`)}}}}, nil
		case 2:
			if !r.JSON || len(r.Tools) != 0 || r.Messages[0].Content != question {
				t.Fatal("planner must receive only the trusted request and typed contract")
			}
			return answer(`{"kind":"clarify","question":"Qual nome você quer dar ao agente?"}`), nil
		}
		t.Fatal("unexpected extra call")
		return nil, nil
	})
	if err != nil || step != 2 || got.Plan.Kind != Clarify {
		t.Fatal(got, err)
	}
}
