package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/delegation"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm/llmtest"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestExternalDelegationThroughAuthenticatedCallback(t *testing.T) {
	for _, mode := range []string{"automatic", "approve", "reject", "no-grant", "invalid", "provider-failure", "budget"} {
		t.Run(mode, func(t *testing.T) {
			srv, a := setupApp(t, nil)
			provider := llmtest.New(t, providerKey)
			provider.FailChat.Store(mode == "provider-failure")
			enablePlugin(t, srv, "openai")
			if c := call(t, srv, "POST", "/api/v1/providers", adminTok, "application/json", providerBody(provider, "team", providerKey), nil); c != 201 {
				t.Fatal(c)
			}
			manager := delegation.Manager{Store: a.Store}
			policy := delegation.DefaultPolicy()
			policy.ApprovalMode = "automatic"
			if mode == "approve" || mode == "reject" {
				policy.ApprovalMode = "required"
			}
			if mode == "budget" {
				policy.MaxTreeTokens = 1
			}
			if _, err := manager.SavePolicy(t.Context(), policy, 0, "admin"); err != nil {
				t.Fatal(err)
			}
			type outcome struct {
				input  runtime.TurnInput
				result tools.Result
				status int
				err    error
			}
			turns := make(chan outcome, 1)
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/health" {
					w.WriteHeader(200)
					return
				}
				var in runtime.TurnInput
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					http.Error(w, "invalid turn", 400)
					return
				}
				u, err := url.Parse(in.Callback.URL)
				if err != nil {
					http.Error(w, "invalid callback", 400)
					return
				}
				args := `{"template":"analysis","objective":"Summarize supplied text"}`
				if mode == "invalid" {
					args = `{"template":"analysis","objective":"work","tools":["shell.exec"]}`
				}
				req, err := http.NewRequestWithContext(r.Context(), "POST", srv.URL+u.Path+"/tools/delegation.request", strings.NewReader(args))
				if err != nil {
					http.Error(w, "request failed", 500)
					return
				}
				req.Header.Set("Authorization", "Bearer "+in.Callback.Token)
				resp, err := http.DefaultClient.Do(req)
				out := outcome{input: in, err: err}
				if err == nil {
					out.status = resp.StatusCode
					out.err = json.NewDecoder(resp.Body).Decode(&out.result)
					resp.Body.Close()
				}
				turns <- out
				_ = json.NewEncoder(w).Encode(runtime.TurnResult{Summary: out.result.Content})
			}))
			t.Cleanup(agent.Close)
			grants := "[delegation.request]"
			if mode == "no-grant" {
				grants = "[]"
			}
			graph := fmt.Sprintf("metadata: {name: external-delegation}\nspec:\n  nodes:\n    - id: parent\n      runtime: {kind: external, endpoint: %q}\n      model: {provider: team}\n      tools: %s\n      loop: {mode: once}\n      limits: {turnTimeout: 10s}\n", agent.URL, grants)
			if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", graph, nil); c != 201 {
				t.Fatal(c)
			}
			if c := call(t, srv, "POST", "/api/v1/graphs/external-delegation/start", adminTok, "", "", nil); c != 200 {
				t.Fatal(c)
			}
			if policy.ApprovalMode == "required" {
				deadline := time.Now().Add(5 * time.Second)
				decided := false
				for time.Now().Before(deadline) {
					approvals, err := a.Store.ListApprovals(t.Context(), storage.ApprovalFilter{GraphID: "external-delegation", Status: domain.ApprovalPending})
					if err != nil {
						t.Fatal(err)
					}
					for _, approval := range approvals {
						if approval.Kind == domain.ApprovalDelegation {
							if provider.Chats() != 0 {
								t.Fatal("child ran before consent")
							}
							if _, err := a.Approvals.Decide(t.Context(), approval.ID, mode == "approve", "operator", "", ""); err != nil {
								t.Fatal(err)
							}
							decided = true
							break
						}
					}
					if decided {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !decided {
					t.Fatal("no approval")
				}
			}
			select {
			case out := <-turns:
				if out.err != nil {
					t.Fatal(out.err)
				}
				if mode == "no-grant" {
					if out.status != 403 {
						t.Fatal(out.status)
					}
				} else if out.status != 200 {
					t.Fatal(out.status)
				}
				successful := mode == "automatic" || mode == "approve"
				if successful && (out.result.IsError || !strings.Contains(out.result.Content, "OK")) {
					t.Fatal(out.result)
				}
				if !successful && mode != "no-grant" && !out.result.IsError {
					t.Fatal("failure hidden", out.result)
				}
				if mode == "reject" || mode == "invalid" || mode == "budget" || mode == "no-grant" {
					if provider.Chats() != 0 {
						t.Fatal("denied model request")
					}
				}
				// Gateway results contain neither model credentials nor reusable callback tokens.
				raw, _ := json.Marshal(out.result)
				if strings.Contains(string(raw), providerKey) || strings.Contains(string(raw), out.input.Callback.Token) {
					t.Fatal("credential leaked")
				}
				if successful {
					budget, err := manager.Budget(t.Context(), out.input.ExecutionID)
					if err != nil || budget.Reserved != 0 || budget.Charged == 0 {
						t.Fatal(budget, err)
					}
				}
				deadline := time.Now().Add(3 * time.Second)
				for {
					x, err := a.Store.GetExecution(t.Context(), out.input.ExecutionID)
					if err != nil {
						t.Fatal(err)
					}
					if x.Status.Terminal() {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("parent not completed")
					}
					time.Sleep(10 * time.Millisecond)
				}
				u, _ := url.Parse(out.input.Callback.URL)
				if c := call(t, srv, "POST", u.Path+"/tools/delegation.request", out.input.Callback.Token, "application/json", `{}`, nil); c != 401 {
					t.Fatal("expired callback accepted", c)
				}
			case <-time.After(12 * time.Second):
				t.Fatal("no returned result")
			}
		})
	}
}
