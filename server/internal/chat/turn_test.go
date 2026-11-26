package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
)

func taskCall(id, arguments string) *llm.ChatResponse {
	return nativeTestCall(id, "query_tasks", arguments)
}

func nativeTestCall(id, name, arguments string) *llm.ChatResponse {
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
		ID: id, Name: name, Arguments: json.RawMessage(arguments), ProviderData: json.RawMessage(`{"signature":"opaque-fixture"}`),
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
		{"unverified plain answer", ErrInvalidPlan, 0},
		{"more calls than allowed", ErrInvalidPlan, 3},
		{"duplicate in batch", ErrInvalidPlan, 0},
		{"missing call ID", ErrInvalidPlan, 0},
		{"oversized call ID", ErrInvalidPlan, 0},
		{"unsafe model arguments", ErrInvalidPlan, 0},
		{"read mixed with a proposal", ErrInvalidPlan, 0},
		{"proposal after a read", ErrInvalidPlan, 1},
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
				case "unverified plain answer":
					return answer("Não há tarefas."), nil
				case "duplicate in batch":
					resp.Message.ToolCalls = append(resp.Message.ToolCalls, resp.Message.ToolCalls[0])
				case "missing call ID":
					resp.Message.ToolCalls[0].ID = ""
				case "oversized call ID":
					resp.Message.ToolCalls[0].ID = strings.Repeat("x", 257)
				case "unsafe model arguments":
					return taskCall("id", `{"mode":"count","title":"Authorization: Bearer fixture-private-output-secret"}`), nil
				case "read mixed with a proposal":
					resp.Message.ToolCalls = append(resp.Message.ToolCalls, nativeTestCall("plan", "start_task", `{"task":"tsk_fixture"}`).Message.ToolCalls...)
				case "proposal after a read":
					if reads == 1 {
						if len(r.Tools) != 1 || r.Tools[0].Name != "query_tasks" {
							t.Fatal("operation tools should not be exposed after a read")
						}
						return nativeTestCall("plan", "start_task", `{"task":"tsk_fixture"}`), nil
					}
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
			return nativeTestCall("overview", "inspect_console", `{"inspectArea":"tasks"}`), nil
		case 2:
			if len(r.Tools) != 1 || r.Tools[0].Name != "query_tasks" {
				t.Fatal("task overview did not redirect to the native query")
			}
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
	for _, test := range []string{"corrected", "answer without evidence", "second invalid call"} {
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
				switch test {
				case "corrected":
					return taskCall("corrected", `{"mode":"count","status":"succeeded"}`), nil
				case "second invalid call":
					return taskCall("still-invalid", `{"mode":"count","status":"done"}`), nil
				}
			}
			return answer("12 tarefas."), nil
		})
		if test == "corrected" && (err != nil || rounds != 3 || result.Text != "12 tarefas.") {
			t.Fatal(result, err)
		}
		if test != "corrected" && (!errors.Is(err, ErrInvalidPlan) || result.Text != "" || rounds != 2) {
			t.Fatal("accepted answer without evidence or exceeded correction limit", result, err)
		}
	}
}

func TestTurnReturnsNativeProposalsInOneModelCall(t *testing.T) {
	for _, test := range []struct{ name, arguments, kind string }{
		{"create_catalog_agent", `{"agentId":"reader","agentName":"Reader","instructions":"Read the notes."}`, CreateAgent},
		{"open_console_page", `{"destination":"workflows"}`, OpenManual},
		{"inspect_console", `{"inspectArea":"projects"}`, InspectConsole},
		{"find_resources", `{"inspectArea":"nodes","resourceQuery":"reader"}`, FindResource},
		{"ask_clarifying_question", `{"question":"Qual agente?"}`, Clarify},
		{"unsupported_request", `{}`, Unsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			const question = "O que pode fazer com o leitor?"
			calls := 0
			got, err := RunTurn(t.Context(), []llm.ChatMessage{{Role: llm.RoleUser, Content: question}}, nil, func(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
				calls++
				if req.JSON || len(req.Messages) != 1 || req.Messages[0].Content != question || !toolOffered(req, test.name) || toolOffered(req, "prepare_operation") {
					t.Fatal("missing native operation contract or conversation")
				}
				return nativeTestCall("plan", test.name, test.arguments), nil
			})
			if err != nil || calls != 1 || got.Plan.Kind != test.kind || got.Text != "" || got.Grounded {
				t.Fatal(got, err, calls)
			}
		})
	}
}

func TestTurnMayCorrectOneNativeOperationOrAskForClarification(t *testing.T) {
	const bad = `{"agentId":"release-reader","instructions":"Read the notes."}`
	const good = `{"agentId":"release-reader","agentName":"Reader","instructions":"Read the notes."}`
	for _, test := range []struct {
		name, first, next, args, kind string
		calls                         int
	}{
		{"corrected", bad, "create_catalog_agent", good, CreateAgent, 2},
		{"clarification", bad, "ask_clarifying_question", `{"question":"Qual nome devo usar?"}`, Clarify, 2},
		{"invalid again", bad, "create_catalog_agent", bad, "", 2},
		{"different operation", bad, "start_task", `{"task":"tsk_fixture"}`, "", 2},
		{"typed response would bypass scoped correction", bad, "", `{"kind":"task.start","task":"tsk_fixture"}`, "", 2},
		{"unsafe original arguments", `{"agentId":"release-reader","instructions":"Authorization: Bearer fixture-sensitive-value"}`, "", "", "", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			history := []llm.ChatMessage{{Role: llm.RoleUser, Content: "Estamos tratando das notas de versão."}, {Role: llm.RoleUser, Content: "Crie um agente para lê-las."}}
			calls := 0
			got, err := RunTurn(t.Context(), history, nil, func(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
				calls++
				if calls == 1 {
					return nativeTestCall("invalid", "create_catalog_agent", test.first), nil
				}
				if calls > test.calls {
					t.Fatal("exceeded one correction")
				}
				if r.JSON || len(r.Tools) != 2 || !toolOffered(r, "create_catalog_agent") || !toolOffered(r, "ask_clarifying_question") || len(r.Messages) != 4 || r.Messages[0].Content != history[0].Content || r.Messages[1].Content != history[1].Content {
					t.Fatal("correction did not keep the original request and only the scoped tools")
				}
				origin, result := r.Messages[2], r.Messages[3]
				if origin.Role != llm.RoleAssistant || len(origin.ToolCalls) != 1 || string(origin.ToolCalls[0].ProviderData) != `{"signature":"opaque-fixture"}` || result.Role != llm.RoleTool || result.ToolCallID != "invalid" || !strings.Contains(result.Content, "invalid_arguments") {
					t.Fatal("correction was not grounded in its original call")
				}
				if test.next == "" {
					return answer(test.args), nil
				}
				return nativeTestCall("retry", test.next, test.args), nil
			})
			if calls != test.calls || got.Plan.Kind != test.kind || (test.kind == "" && !errors.Is(err, ErrInvalidPlan)) || (test.kind != "" && err != nil) {
				t.Fatal(got, err, calls)
			}
		})
	}
}

func toolOffered(request *llm.ChatRequest, name string) bool {
	return slices.ContainsFunc(request.Tools, func(tool llm.ToolSchema) bool { return tool.Name == name })
}
