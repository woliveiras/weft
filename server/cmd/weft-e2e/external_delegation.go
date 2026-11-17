//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/tools"
)

// Real HTTP external-runtime/callback boundary; only inference is deterministic.
func prepareExternalDelegation(ctx context.Context, a *app.App, callbackBase string) (func(), error) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" {
			w.WriteHeader(200)
			return
		}
		var in runtime.TurnInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Callback == nil {
			http.Error(w, "invalid turn", 400)
			return
		}
		callback, err := url.Parse(in.Callback.URL)
		if err != nil {
			http.Error(w, "invalid callback", 400)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), "POST", callbackBase+callback.Path+"/tools/delegation.request", strings.NewReader(`{"template":"analysis","objective":"Summarize fixture text"}`))
		if err != nil {
			http.Error(w, "callback request failed", 500)
			return
		}
		req.Header.Set("Authorization", "Bearer "+in.Callback.Token)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, "callback unavailable", 502)
			return
		}
		defer response.Body.Close()
		var result tools.Result
		if response.StatusCode != 200 {
			http.Error(w, fmt.Sprintf("callback status %d", response.StatusCode), 502)
			return
		}
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			http.Error(w, "invalid callback result", 502)
			return
		}
		_ = json.NewEncoder(w).Encode(runtime.TurnResult{Summary: result.Content})
	}))
	for _, id := range []string{"external-parent-approve", "external-parent-reject", "external-parent-cancel"} {
		graph := &domain.Graph{Metadata: domain.GraphMetadata{ID: id, Name: id}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "parent", Runtime: domain.RuntimeRef{Kind: domain.RuntimeExternal, Endpoint: agent.URL}, Model: domain.ModelRef{Provider: "e2e", Name: "fixture"}, Loop: domain.LoopSpec{Mode: domain.LoopOnce}, Tools: []domain.ToolGrant{{Name: "delegation.request"}}}}}}
		if _, _, err := a.Engine.SaveGraph(ctx, graph, 0, "fixture"); err != nil {
			agent.Close()
			return nil, err
		}
	}
	return agent.Close, nil
}
