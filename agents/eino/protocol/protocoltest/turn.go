// Package protocoltest runs one Go agent turn against a test-owned HTTP gateway.
package protocoltest

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/woliveiras/weft/agents/eino/protocol"
)

// Run binds an isolated execution callback, then uses the production Protocol v1
// handler. The supplied gateway controls tool grants, state and model replies.
func Run(t testing.TB, in protocol.TurnInput, agent protocol.Agent, gateway http.Handler) (protocol.TurnResult, error) {
	t.Helper()
	if gateway == nil {
		return protocol.TurnResult{}, errors.New("supply a test gateway")
	}
	server := httptest.NewServer(gateway)
	defer server.Close()
	if in.ExecutionID == "" {
		in.ExecutionID = "fixture"
	}
	in.Callback.URL = server.URL + "/internal/v1/executions/" + in.ExecutionID
	in.Callback.Token = "fixture"
	handler, err := protocol.NewHandler([]string{server.URL}, "", map[string]protocol.Agent{in.Agent: agent})
	if err != nil {
		return protocol.TurnResult{}, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return protocol.TurnResult{}, err
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/turn", bytes.NewReader(raw))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		return protocol.TurnResult{}, &protocol.HTTPError{Status: recorder.Code}
	}
	var result protocol.TurnResult
	err = json.Unmarshal(recorder.Body.Bytes(), &result)
	return result, err
}
