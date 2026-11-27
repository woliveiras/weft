package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCapabilityMustMatchControllerAndExecution(t *testing.T) {
	const origin = "http://controller:8080"
	const path = "/internal/v1/executions/exe_1"
	tests := []string{origin + path, origin + path + "?relay=1", origin + path + "#x", origin + path + "/other", origin + path + "%2Fextra", "http://elsewhere" + path, "http://x@controller:8080" + path, origin + "/internal/v1/executions/exe_2"}
	for i, address := range tests {
		in := TurnInput{ExecutionID: "exe_1"}
		in.Callback.URL, in.Callback.Token = address, "fixture"
		_, err := bind(&in, map[string]bool{origin: true})
		if (err == nil) != (i == 0) {
			t.Errorf("address %q err %v", address, err)
		}
	}
}

func TestCallbackRejectsRedirectAndFailedToolWithoutDisclosingResponse(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	for _, status := range []int{200, 302, 307, 403, 502} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", target.URL)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"isError":true,"content":"private-fixture"}`))
		}))
		client := Client{base: s.URL, token: "fixture"}
		_, err := client.Tool(context.Background(), "filesystem.read", map[string]string{"path": "doc"})
		s.Close()
		if err == nil || strings.Contains(err.Error(), "private-fixture") || forwarded.Load() != 0 {
			t.Fatalf("status=%d err=%v forwarded=%d", status, err, forwarded.Load())
		}
	}
}

func TestConcurrentTurnClientsStayBoundAfterYield(t *testing.T) {
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var origins []string
	var controllers []*httptest.Server
	for _, id := range []string{"one", "two"} {
		controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/internal/v1/executions/"+id+"/tools/review.request" || r.Header.Get("Authorization") != "Bearer "+id {
				t.Error("callback crossed execution boundary")
			}
			arrived <- struct{}{}
			select {
			case <-r.Context().Done():
				return
			case <-release:
			}
			_, _ = w.Write([]byte(`{"yield":true}`))
		}))
		t.Cleanup(controller.Close)
		controllers = append(controllers, controller)
		origins = append(origins, controller.URL)
	}
	agent := func(ctx context.Context, in *TurnInput, c *Client) (TurnResult, error) {
		if _, err := c.Tool(ctx, "review.request", struct{}{}); !errors.Is(err, ErrYield) {
			return TurnResult{}, errors.New("missing yield")
		}
		if _, err := c.Chat(ctx, ChatRequest{}); !errors.Is(err, ErrYield) {
			return TurnResult{}, errors.New("continued after yield")
		}
		return TurnResult{Emits: []Emit{{Type: "discard"}}}, nil
	}
	handler, err := NewHandler(origins, "", map[string]Agent{"review": agent})
	if err != nil {
		t.Fatal(err)
	}
	responses := make(chan *httptest.ResponseRecorder, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	for i, id := range []string{"one", "two"} {
		go func() {
			in := TurnInput{Agent: "review", ExecutionID: id}
			in.Callback.URL, in.Callback.Token = controllers[i].URL+"/internal/v1/executions/"+id, id
			raw, err := json.Marshal(in)
			if err != nil {
				t.Error(err)
				return
			}
			req := httptest.NewRequestWithContext(ctx, "POST", "/v1/turn", bytes.NewReader(raw))
			out := httptest.NewRecorder()
			handler.ServeHTTP(out, req)
			responses <- out
		}()
	}
	for range 2 {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("turns did not overlap")
		}
	}
	close(release)
	for range 2 {
		select {
		case res := <-responses:
			if res.Code != 200 || res.Body.String() != "{}" {
				t.Fatal(res.Code, res.Body)
			}
		case <-ctx.Done():
			t.Fatal("turn did not settle")
		}
	}
}

func TestCatalogAndQueuedOutputUseExecutionScope(t *testing.T) {
	catalog := `[{"name":"filesystem.read","description":"read","parameters":{"type":"object"}}]`
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/executions/one/tools", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing capability")
		}
		_, _ = w.Write([]byte(catalog))
	})
	mux.HandleFunc("POST /internal/v1/executions/one/emit", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing capability")
		}
		var msg Emit
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Error(err)
		}
		if msg.Channel != "release" || msg.Target != "consumer" || msg.Metadata["key"] != "value" {
			t.Error("lost output routing")
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"queued": msg.Type == "qa.passed"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := Client{base: srv.URL + "/internal/v1/executions/one", token: "fixture"}
	list, err := c.ListTools(t.Context())
	if err != nil || len(list) != 1 || list[0].Name != "filesystem.read" || string(list[0].Parameters) != `{"type":"object"}` {
		t.Fatal(list, err)
	}
	msg := Emit{Type: "qa.passed", Channel: "release", Target: "consumer", Metadata: map[string]any{"key": "value"}}
	if err := c.Emit(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
	msg.Type = "invalid"
	if err := c.Emit(t.Context(), msg); err == nil {
		t.Fatal("missing acknowledgement accepted")
	}
	c.yielded.Store(true)
	if err := c.Emit(t.Context(), msg); !errors.Is(err, ErrYield) {
		t.Fatal(err)
	}
	if _, err := c.ListTools(t.Context()); !errors.Is(err, ErrYield) {
		t.Fatal(err)
	}
}

func TestTurnWireCarriesSelectionSignalsAndLocalUsage(t *testing.T) {
	var in TurnInput
	raw := `{"model":{"provider":"selected","name":"fixture"},"outputs":[{"channel":"release"}],"inputs":[{"metadata":{"hops":2}}],"pendingStop":true,"maxToolCalls":3}`
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	if in.Model.Provider != "selected" || in.Model.Name != "fixture" || !in.PendingStop || in.MaxToolCalls != 3 || in.Outputs[0].Channel != "release" || string(in.Inputs[0].Metadata["hops"]) != "2" {
		t.Fatal("lost turn context")
	}
	out, err := json.Marshal(TurnResult{Broadcast: "done", Signal: "confirm_stop", Usage: &Usage{InputTokens: 11}, ToolCalls: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"broadcast":"done"`, `"signal":"confirm_stop"`, `"inputTokens":11`, `"toolCalls":1`} {
		if !bytes.Contains(out, []byte(field)) {
			t.Fatal("lost field", field)
		}
	}
}
