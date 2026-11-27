package factory

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/agents/eino/protocol"
)

func TestHeuristicPlanKeepsBulletsAndResearch(t *testing.T) {
	tasks, qs := heuristicPlan("- add a greet function\n- Which language?\n- add a CLI")
	if len(tasks) != 2 || tasks[1].Title != "add a CLI" || len(qs) != 1 || qs[0].Question != "Which language?" {
		t.Fatal(tasks, qs)
	}
	tasks, qs = heuristicPlan("Add an adder. Add a subtractor.")
	if len(tasks) != 2 || len(qs) != 0 {
		t.Fatal(tasks, qs)
	}
}

type emitted struct {
	Type    string
	Payload map[string]json.RawMessage
}

func sendTurn(t *testing.T, h http.Handler, origin, name, id, kind string, payload any) []emitted {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	in := protocol.TurnInput{Agent: name, ExecutionID: "exe_" + id, Inputs: []protocol.Message{{ID: id, Type: kind, Payload: raw}}}
	in.Callback.URL, in.Callback.Token = origin+"/internal/v1/executions/"+in.ExecutionID, "fixture"
	raw, err = json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/turn", bytes.NewReader(raw))
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	var result struct{ Emits []emitted }
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &result) != nil {
		t.Fatalf("%s on %s: %d %s", name, kind, res.Code, res.Body)
	}
	return result.Emits
}

func TestResearchRoundTripResumesSavedPlan(t *testing.T) {
	var saved []byte
	files := map[string]string{}
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/llm/chat"):
			var req protocol.ChatRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				t.Error("invalid request")
			}
			answer := "Use the source."
			if req.JSON {
				answer = `{"tasks":[{"title":"Implement"}],"questions":[{"question":"Read https://example.invalid/doc?"}]}`
			}
			_ = json.NewEncoder(w).Encode(protocol.ChatResponse{Message: protocol.ChatMessage{Role: "assistant", Content: answer}})
		case strings.HasSuffix(r.URL.Path, "/tools/filesystem.write"):
			var args struct{ Path, Content string }
			if json.NewDecoder(r.Body).Decode(&args) != nil {
				t.Error("invalid write")
			}
			files[args.Path] = args.Content
			_, _ = io.WriteString(w, `{"content":"ok"}`)
		case strings.HasSuffix(r.URL.Path, "/tools/http.fetch"):
			http.Error(w, "denied", 403)
		case strings.HasSuffix(r.URL.Path, "/output"):
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/state/plan:request"):
			if r.Method == "PUT" {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				saved = raw
				w.WriteHeader(204)
			} else if saved == nil {
				w.WriteHeader(404)
			} else {
				_, _ = w.Write(saved)
			}
		default:
			t.Errorf("unexpected callback %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer controller.Close()
	handler := func() http.Handler {
		planner, err := Planner(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		researcher, err := Researcher(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		h, err := protocol.NewHandler([]string{controller.URL}, "", map[string]protocol.Agent{"planner": planner, "researcher": researcher})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	h := handler()
	ask := sendTurn(t, h, controller.URL, "planner", "request", "feature.request", map[string]string{"title": "Build a feature"})
	if len(ask) != 1 || ask[0].Type != "research.question" || files["plans/request.md"] == "" {
		t.Fatal(ask, files)
	}
	done := sendTurn(t, h, controller.URL, "researcher", "research", "research.question", ask[0].Payload)
	if len(done) != 1 || done[0].Type != "research.done" || !strings.Contains(files["research/request-q1.md"], "Use the source.") {
		t.Fatal(done, files)
	}
	h = handler()
	for _, id := range []string{"final", "final", "duplicate"} {
		tasks := sendTurn(t, h, controller.URL, "planner", id, "research.done", done[0].Payload)
		if id == "duplicate" {
			if len(tasks) != 0 {
				t.Fatal(tasks)
			}
			continue
		}
		if len(tasks) != 1 || tasks[0].Type != "task.ready" || stringValue(tasks[0].Payload["task_id"]) != "request-t1" || string(tasks[0].Payload["research"]) != `["research/request-q1.md"]` {
			t.Fatal(tasks)
		}
	}
}
