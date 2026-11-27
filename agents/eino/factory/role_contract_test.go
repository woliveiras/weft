package factory

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/woliveiras/weft/agents/eino/protocol"
	"github.com/woliveiras/weft/agents/eino/protocol/protocoltest"
)

func TestModelObjectExtraction(t *testing.T) {
	for _, tc := range []struct{ reply, want string }{
		{`{"a":1}`, "1"}, {"Sure!\n```json\n{\"a\":[1,2]}\n```", "[1,2]"},
		{`prefix {"a":"}{"} suffix {"b":2}`, `"}{"`}, {`broken {"a":} then {"a":true}`, "true"},
		{"no json", ""}, {"", ""},
	} {
		var object map[string]json.RawMessage
		if ok := decodeObject(tc.reply, &object); ok != (tc.want != "") || string(object["a"]) != tc.want {
			t.Errorf("decode %q: %+v", tc.reply, object)
		}
	}
}

func TestDeveloperUsesScopedFilesAndEscalatesRework(t *testing.T) {
	agent, err := Developer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var saved []byte
	var writes []string
	asks := 0
	gateway := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/state/iter:task"):
			if r.Method == "PUT" {
				var err error
				saved, err = io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				w.WriteHeader(204)
			} else {
				_, _ = w.Write(saved)
			}
		case strings.HasSuffix(r.URL.Path, "/llm/chat"):
			msg := `broken {"files":} corrected {"files":[{"path":"src/a.go","content":"package feature"},{"path":"../elsewhere","content":"unsafe"}],"summary":"ready"}`
			_ = json.NewEncoder(w).Encode(protocol.ChatResponse{Message: protocol.ChatMessage{Role: "assistant", Content: msg}})
		case strings.HasSuffix(r.URL.Path, "/tools/filesystem.write"):
			var input struct{ Path string }
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			writes = append(writes, input.Path)
			_, _ = io.WriteString(w, `{"content":"written"}`)
		case strings.HasSuffix(r.URL.Path, "/tools/git.add"):
			w.WriteHeader(403)
		case strings.HasSuffix(r.URL.Path, "/ask"):
			asks++
			_, _ = io.WriteString(w, `{"answer":"ship"}`)
		default:
			t.Errorf("unexpected callback %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	for turnIndex, n := range []int{1, 2, 2, 3, 4, 5} {
		kind := "task.ready"
		if n > 1 {
			kind = "qa.failed"
		}
		out, err := protocoltest.Run(t, protocol.TurnInput{Agent: "developer", Inputs: []protocol.Message{{ID: "input-" + strconv.Itoa(n), Type: kind, Payload: json.RawMessage(`{"request_id":"r","task_id":"task","title":"Build"}`)}}}, agent, gateway)
		if err != nil || len(out.Emits) != 1 || out.Emits[0].Type != "code.ready" {
			t.Fatalf("turn %d: %+v %v", n, out, err)
		}
		raw, err := json.Marshal(out.Emits[0].Payload)
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			Iteration int
			Escalated bool
			Files     []string
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		var progress developmentProgress
		if err := json.Unmarshal(saved, &progress); err != nil {
			t.Fatal(err)
		}
		if p.Iteration != n || p.Escalated != (n == 5) || !slices.Equal(p.Files, []string{"src/a.go"}) || len(writes) != turnIndex+1 || writes[turnIndex] != "src/a.go" || progress.Iteration != n || progress.InputID != "input-"+strconv.Itoa(n) {
			t.Fatalf("turn %d: %+v %v", n, p, writes)
		}
	}
	if asks != 1 {
		t.Fatalf("human questions: %d", asks)
	}
}

func TestResearcherReportsDeniedHTTPAndCompletes(t *testing.T) {
	a, err := Researcher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var report, note string
	gateway := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/tools/http.fetch"):
			w.WriteHeader(403)
		case strings.HasSuffix(r.URL.Path, "/llm/chat"):
			_ = json.NewEncoder(w).Encode(protocol.ChatResponse{Message: protocol.ChatMessage{Role: "assistant", Content: "Local research"}})
		case strings.HasSuffix(r.URL.Path, "/output"):
			var data struct{ Text string }
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
				t.Error(err)
			}
			note = data.Text
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/tools/filesystem.write"):
			var data struct{ Content, Path string }
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
				t.Error(err)
			}
			report = data.Path + " " + data.Content
			_, _ = io.WriteString(w, `{"content":"ok"}`)
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	})
	out, err := protocoltest.Run(t, protocol.TurnInput{Agent: "researcher", Inputs: []protocol.Message{{Type: "research.question", Payload: json.RawMessage(`{"question":"Read https://example.invalid/doc","question_id":"q","request_id":"r"}`)}}}, a, gateway)
	if err != nil || len(out.Emits) != 1 || out.Emits[0].Type != "research.done" || !strings.Contains(note, "http.fetch not granted") || !strings.Contains(report, "research/q.md") || !strings.Contains(report, "Local research") {
		t.Fatalf("%+v, %s, %s, %v", out, note, report, err)
	}
}

func TestOfflineBatchThroughFactory(t *testing.T) {
	files := map[string]string{}
	gateway := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tool struct{ Path, Content string }
		switch {
		case strings.HasSuffix(r.URL.Path, "/llm/chat"):
			_ = json.NewEncoder(w).Encode(protocol.ChatResponse{Message: protocol.ChatMessage{Role: "assistant", Content: "MOCK: offline"}})
		case strings.HasSuffix(r.URL.Path, "/tools/filesystem.write"), strings.HasSuffix(r.URL.Path, "/tools/filesystem.read"):
			if err := json.NewDecoder(r.Body).Decode(&tool); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if strings.HasSuffix(r.URL.Path, ".write") {
				files[tool.Path] = tool.Content
			}
			_ = json.NewEncoder(w).Encode(protocol.ToolResult{Content: files[tool.Path]})
		case strings.Contains(r.URL.Path, "/state/iter:"):
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/tools/git.add"):
			w.WriteHeader(403)
		case strings.HasSuffix(r.URL.Path, "/tools/shell.exec"):
			_, _ = io.WriteString(w, `{"data":{"exitCode":0}}`)
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	})
	inputs := []protocol.Message{{ID: "req", Type: "feature.request", Payload: json.RawMessage(`"Add an adder. Add a subtractor."`)}, {Type: "ignored", Payload: json.RawMessage("42")}}
	for _, stage := range []struct {
		name, kind string
		build      func(context.Context) (protocol.Agent, error)
	}{{"planner", "task.ready", Planner}, {"developer", "code.ready", Developer}, {"reviewer", "review.approved", Reviewer}, {"qa", "qa.passed", QA}} {
		agent, err := stage.build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		out, err := protocoltest.Run(t, protocol.TurnInput{Agent: stage.name, Inputs: inputs}, agent, gateway)
		if err != nil || len(out.Emits) != 2 {
			t.Fatalf("%s: %+v %v", stage.name, out, err)
		}
		inputs = nil
		for i, msg := range out.Emits {
			body, err := json.Marshal(msg.Payload)
			var task struct {
				ID string `json:"task_id"`
			}
			if err != nil || json.Unmarshal(body, &task) != nil || task.ID != "req-t"+strconv.Itoa(i+1) || msg.Type != stage.kind {
				t.Fatalf("%s: %s %s", stage.name, msg.Type, body)
			}
			inputs = append(inputs, protocol.Message{Type: msg.Type, Payload: body})
		}
	}
}
