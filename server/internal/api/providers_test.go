package api_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm/llmtest"
	"github.com/woliveiras/weft/server/internal/storage"
)

const providerKey = "sk-test-api"

type providerView struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	DefaultModel string `json:"defaultModel"`
	Default      bool   `json:"default"`
	Source       string `json:"source"`
	Editable     bool   `json:"editable"`
	Status       string `json:"status"`
	Credential   struct {
		Configured bool   `json:"configured"`
		Source     string `json:"source"`
	} `json:"credential"`
}

type apiError struct {
	Code    string `json:"code"`
	Error   string `json:"error"`
	Details struct {
		Errors []struct{ Path, Message string } `json:"errors"`
	} `json:"details"`
}

func providerBody(fake *llmtest.Server, name, key string) string {
	k := ""
	if key != "" {
		k = fmt.Sprintf(`,"apiKey":{"value":%q}`, key)
	}
	return fmt.Sprintf(`{"name":%q,"type":"openai","baseUrl":%q,"defaultModel":"fake-small","maxRetries":-1%s}`, name, fake.BaseURL(), k)
}

// enablePlugin turns an inference plugin on as an administrator.
func enablePlugin(t *testing.T, srv *httptest.Server, id string) {
	t.Helper()
	if c := call(t, srv, "PUT", "/api/v1/provider-plugins/"+id, adminTok, "application/json", `{"enabled":true}`, nil); c != 200 {
		t.Fatalf("enable plugin %s: %d", id, c)
	}
}

func rawCall(t *testing.T, method, url, tok, body string, hdr map[string]string) (int, http.Header, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b)
}

// An operator configures a provider through the API, tests it with a real
// inference, selects it for a node, and the node's turn reaches that
// provider with the stored credential - which never comes back out.
func TestProviderManagementEndToEnd(t *testing.T) {
	srv, a := setupApp(t, nil)
	fake := llmtest.New(t, providerKey)

	// Roles: viewers read, operators test, admins change.
	if c := call(t, srv, "POST", "/api/v1/providers", viewerTok, "application/json", providerBody(fake, "team", providerKey), nil); c != 403 {
		t.Fatalf("viewer create: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/providers", operatorTok, "application/json", providerBody(fake, "team", providerKey), nil); c != 403 {
		t.Fatalf("operator create: %d", c)
	}

	// Every plugin starts disabled: no instance can be created until an
	// administrator enables it, and only an administrator can.
	var disabled apiError
	if c := call(t, srv, "POST", "/api/v1/providers", adminTok, "application/json", providerBody(fake, "team", providerKey), &disabled); c != 422 ||
		len(disabled.Details.Errors) != 1 || disabled.Details.Errors[0].Path != "type" || !strings.Contains(disabled.Details.Errors[0].Message, "plugin is disabled") {
		t.Fatalf("create with disabled plugin: %d %+v", c, disabled)
	}
	for _, tok := range []string{viewerTok, operatorTok} {
		if c := call(t, srv, "PUT", "/api/v1/provider-plugins/openai", tok, "application/json", `{"enabled":true}`, nil); c != 403 {
			t.Fatalf("non-admin enable: %d", c)
		}
	}
	for body, want := range map[string]int{`{}`: 400, `{"enabled":"yes"}`: 400, `{"enabled":true,"x":1}`: 400} {
		if c := call(t, srv, "PUT", "/api/v1/provider-plugins/openai", adminTok, "application/json", body, nil); c != want {
			t.Fatalf("enable body %s: %d, want %d", body, c, want)
		}
	}
	if c := call(t, srv, "PUT", "/api/v1/provider-plugins/nope", adminTok, "application/json", `{"enabled":true}`, nil); c != 404 {
		t.Fatalf("unknown plugin: %d", c)
	}
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/provider-plugins/openai", adminTok, `{"enabled":true}`, map[string]string{"If-Match": `"999"`}); c != 409 {
		t.Fatalf("stale enable: %d", c)
	}
	enablePlugin(t, srv, "openai")

	// Validation is field-level.
	var bad apiError
	if c := call(t, srv, "POST", "/api/v1/providers", adminTok, "application/json", providerBody(fake, "Team Bad", ""), &bad); c != 422 || len(bad.Details.Errors) == 0 {
		t.Fatalf("invalid create: %d %+v", c, bad)
	}
	paths := map[string]bool{}
	for _, e := range bad.Details.Errors {
		paths[e.Path] = true
	}
	if !paths["name"] {
		t.Fatalf("missing name issue: %+v", bad)
	}
	if c, _, body := rawCall(t, "POST", srv.URL+"/api/v1/providers", adminTok, `{"name":"x","type":"mock","apiKey":"`+providerKey, nil); c != 400 || strings.Contains(body, providerKey) {
		t.Fatalf("malformed body must be rejected without echoing it: %d %s", c, body)
	}
	if c, _, _ := rawCall(t, "POST", srv.URL+"/api/v1/providers", adminTok, `{"name":"x","type":"mock","apiKeyEnv":"HOME"}`, nil); c != 400 {
		t.Fatalf("unknown fields (e.g. server env references) must be rejected: %d", c)
	}

	// Draft test before saving: inference through the real gateway path.
	var draft struct {
		OK        bool                   `json:"ok"`
		Draft     bool                   `json:"draft"`
		Inference struct{ Reply string } `json:"inference"`
	}
	if c := call(t, srv, "POST", "/api/v1/providers/test", adminTok, "application/json", providerBody(fake, "team", providerKey), &draft); c != 200 || !draft.OK || !draft.Draft || draft.Inference.Reply != "OK" {
		t.Fatalf("draft test: %d %+v", c, draft)
	}

	// Create.
	var v providerView
	code, hdr, body := rawCall(t, "POST", srv.URL+"/api/v1/providers", adminTok, providerBody(fake, "team", providerKey), nil)
	if code != 201 || strings.Contains(body, providerKey) || hdr.Get("ETag") == "" {
		t.Fatalf("create: %d %s etag=%q", code, body, hdr.Get("ETag"))
	}
	_ = json.Unmarshal([]byte(body), &v)
	if v.Source != "managed" || !v.Editable || !v.Credential.Configured || v.Credential.Source != "stored" || v.Status != "ready" {
		t.Fatalf("created view: %+v", v)
	}

	// The plugin list names the instance built from each plugin.
	var plugins []struct {
		ID        string   `json:"id"`
		Label     string   `json:"label"`
		Key       string   `json:"key"`
		Instances []string `json:"instances"`
	}
	if c := call(t, srv, "GET", "/api/v1/provider-plugins", viewerTok, "", "", &plugins); c != 200 {
		t.Fatalf("plugins: %d", c)
	}
	found := false
	for _, p := range plugins {
		if p.ID == "openai" {
			found = p.Label == "OpenAI" && p.Key == "required" && len(p.Instances) == 1 && p.Instances[0] == "team"
		}
	}
	if !found {
		t.Fatalf("openai plugin with instance team missing: %+v", plugins)
	}

	// Reads never contain the credential or its ciphertext.
	for _, path := range []string{"/api/v1/providers", "/api/v1/providers/team", "/api/v1/provider-settings", "/api/v1/provider-plugins"} {
		c, _, b := rawCall(t, "GET", srv.URL+path, viewerTok, "", nil)
		if c != 200 || strings.Contains(b, providerKey) || strings.Contains(b, `"v1.`) {
			t.Fatalf("GET %s: %d leaks credential material: %s", path, c, b)
		}
	}
	st, err := a.Store.GetSetting(t.Context(), "llm/providers")
	if err != nil || strings.Contains(string(st.Value), providerKey) {
		t.Fatalf("credential stored in clear text: %v %s", err, st.Value)
	}

	// Saved test: operators may run it; viewers may not.
	if c := call(t, srv, "POST", "/api/v1/providers/team/test", viewerTok, "", "", nil); c != 403 {
		t.Fatalf("viewer test: %d", c)
	}
	var res struct {
		OK     bool                `json:"ok"`
		Models struct{ Count int } `json:"models"`
	}
	if c := call(t, srv, "POST", "/api/v1/providers/team/test", operatorTok, "", "", &res); c != 200 || !res.OK || res.Models.Count != 2 {
		t.Fatalf("saved test: %d %+v", c, res)
	}

	// Strict concurrency with If-Match.
	var meta struct {
		Revision int64  `json:"revision"`
		Default  string `json:"default"`
		Secrets  struct {
			Enabled   bool   `json:"enabled"`
			KeySource string `json:"keySource"`
		} `json:"secrets"`
	}
	call(t, srv, "GET", "/api/v1/provider-settings", viewerTok, "", "", &meta)
	if meta.Revision != 2 || !meta.Secrets.Enabled || meta.Secrets.KeySource != "generated" {
		t.Fatalf("meta: %+v", meta)
	}
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/providers/team", adminTok, `{"type":"openai","baseUrl":"`+fake.BaseURL()+`","defaultModel":"fake-large","maxRetries":-1}`,
		map[string]string{"If-Match": `"99"`}); c != 409 {
		t.Fatalf("stale If-Match: %d", c)
	}
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/providers/team", adminTok, `{"type":"openai","baseUrl":"`+fake.BaseURL()+`","defaultModel":"fake-small","maxRetries":-1}`,
		map[string]string{"If-Match": fmt.Sprint(meta.Revision)}); c != 200 {
		t.Fatalf("update keeping the credential: %d", c)
	}

	// Select it for a native node and run a turn.
	graph := `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: provider-run}
spec:
  nodes:
    - id: writer
      prompt: Answer briefly.
      model: {provider: team}
      loop: {mode: event}
  channels:
    - {id: in, to: [writer]}
`
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", graph, nil); c != 201 {
		t.Fatalf("create graph: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/provider-run/start", adminTok, "", "", nil); c != 200 {
		t.Fatalf("start: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/provider-run/channels/in/messages", adminTok, "application/json", `{"type":"task","payload":{"text":"hello"}}`, nil); c != 201 {
		t.Fatalf("inject: %d", c)
	}
	waitFor(t, 10*time.Second, "a completed execution on the managed provider", func() bool {
		var xs []struct{ Status string }
		call(t, srv, "GET", "/api/v1/graphs/provider-run/executions?status=succeeded", viewerTok, "", "", &xs)
		return len(xs) > 0
	})
	if fake.Chats() < 3 || fake.LastAuthorization() != "Bearer "+providerKey {
		t.Fatalf("the node's turn did not reach the selected provider: chats=%d auth=%q", fake.Chats(), fake.LastAuthorization())
	}

	// A provider in use cannot be deleted; the default cannot either.
	var conflict apiError
	if c := call(t, srv, "DELETE", "/api/v1/providers/team", adminTok, "", "", &conflict); c != 409 || !strings.Contains(conflict.Error, "writer") {
		t.Fatalf("delete in use: %d %+v", c, conflict)
	}

	// Nothing recorded anywhere carries the credential.
	evs, err := a.Store.ListEvents(t.Context(), storage.EventFilter{Limit: 5000})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if strings.Contains(string(ev.Data), providerKey) {
			t.Fatalf("event %s carries the credential: %s", ev.Type, ev.Data)
		}
	}
	types := map[domain.EventType]int{}
	for _, ev := range evs {
		types[ev.Type]++
	}
	if types[domain.EvProviderChanged] != 3 || types[domain.EvProviderTested] != 2 { // enable plugin, create, update
		t.Fatalf("provider audit events: %v", types)
	}
}

// Nodes cannot start on a provider that does not exist; saving such a graph
// only warns, so graphs can be prepared before providers are configured.
func TestStartPreflightRejectsUnknownProvider(t *testing.T) {
	srv := setup(t)
	graph := `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: needs-provider}
spec:
  nodes:
    - {id: a, prompt: hi, model: {provider: not-configured}}
  channels:
    - {id: in, to: [a]}
`
	var rep struct {
		Valid    bool
		Warnings []struct{ Path, Message string }
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/validate", viewerTok, "application/yaml", graph, &rep); c != 200 || !rep.Valid || len(rep.Warnings) != 1 || rep.Warnings[0].Path != "spec.nodes[0].model.provider" {
		t.Fatalf("validate: %d %+v", c, rep)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", graph, nil); c != 201 {
		t.Fatalf("save with a warning: %d", c)
	}
	var bad struct {
		Code    string
		Details struct {
			Errors []struct{ Path, Message string }
		}
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/needs-provider/start", operatorTok, "", "", &bad); c != 422 || len(bad.Details.Errors) != 1 ||
		bad.Details.Errors[0].Path != "spec.nodes[0].model.provider" || !strings.Contains(bad.Details.Errors[0].Message, "not-configured") {
		t.Fatalf("start: %d %+v", c, bad)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/needs-provider/nodes/a/start", operatorTok, "", "", &bad); c != 422 {
		t.Fatalf("node start: %d", c)
	}
	var st struct{ DesiredState string }
	call(t, srv, "GET", "/api/v1/graphs/needs-provider/status", viewerTok, "", "", &st)
	if st.DesiredState == "running" {
		t.Fatal("a rejected start must not change the desired state")
	}
}

// Without an installation key, credentials are refused with an actionable
// error instead of being stored in clear text.
func TestProviderCredentialsRequireInstallationKey(t *testing.T) {
	srv, a := setupApp(t, func(c *config.Config) {
		c.Secrets = config.Secrets{KeyFile: t.TempDir() + "/absent.key", GenerateKeyFile: false}
	})
	fake := llmtest.New(t, providerKey)
	enablePlugin(t, srv, "openai")
	var e apiError
	if c := call(t, srv, "POST", "/api/v1/providers", adminTok, "application/json", providerBody(fake, "team", providerKey), &e); c != 409 || e.Code != "secret_key_missing" || !strings.Contains(e.Error, "WEFT_SECRET_KEY") {
		t.Fatalf("create without key: %d %+v", c, e)
	}
	if st, err := a.Store.GetSetting(t.Context(), "llm/providers"); err != nil || strings.Contains(string(st.Value), `"team"`) {
		t.Fatalf("no provider must be stored: %v", err)
	}
	var meta struct {
		Secrets struct{ Enabled bool } `json:"secrets"`
	}
	call(t, srv, "GET", "/api/v1/provider-settings", viewerTok, "", "", &meta)
	if meta.Secrets.Enabled {
		t.Fatal("meta must say credentials cannot be stored")
	}
}

func waitFor(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// External runtimes (LangGraph and friends) reach the managed provider only
// through the per-execution LLM callback: the TurnInput they receive names
// the provider and model but carries no credential.
func TestExternalRuntimeUsesManagedProviderThroughGateway(t *testing.T) {
	srv, a := setupApp(t, nil)
	fake := llmtest.New(t, providerKey)
	enablePlugin(t, srv, "openai")
	if c := call(t, srv, "POST", "/api/v1/providers", adminTok, "application/json", providerBody(fake, "team", providerKey), nil); c != 201 {
		t.Fatalf("create provider: %d", c)
	}
	prices := `{"currency":"USD","prices":[{"provider":"team","model":"*","inputMicrosPerMTok":1000000,"outputMicrosPerMTok":2000000}]}`
	if c := call(t, srv, "PUT", "/api/v1/cost/prices", adminTok, "application/json", prices, nil); c != 200 {
		t.Fatalf("prices: %d", c)
	}

	type seen struct {
		input string
		reply string
		err   error
	}
	turns := make(chan seen, 4)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" {
			w.WriteHeader(200)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var in struct {
			Model    struct{ Provider string }   `json:"model"`
			Callback struct{ URL, Token string } `json:"callback"`
		}
		_ = json.Unmarshal(body, &in)
		s := seen{input: string(body)}
		// The callback URL names the configured internal address; reach the
		// same path on the test server.
		u, _ := url.Parse(in.Callback.URL)
		req, _ := http.NewRequest("POST", srv.URL+u.Path+"/llm/chat", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer "+in.Callback.Token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			var cr struct {
				Message struct{ Content string } `json:"message"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&cr)
			resp.Body.Close()
			s.reply = cr.Message.Content
			if resp.StatusCode != 200 {
				s.err = fmt.Errorf("llm callback: HTTP %d", resp.StatusCode)
			}
		}
		s.err = errors.Join(s.err, err)
		turns <- s
		// Runtime-local inference is reported separately from the callback.
		_ = json.NewEncoder(w).Encode(map[string]any{"summary": s.reply, "usage": map[string]int{"inputTokens": 100, "outputTokens": 50}})
	}))
	t.Cleanup(agent.Close)

	graph := fmt.Sprintf(`
apiVersion: weft.io/v1
kind: Graph
metadata: {name: external-run}
spec:
  nodes:
    - id: lg
      runtime: {kind: external, framework: langgraph, agent: writer, endpoint: %q}
      model: {provider: team}
      loop: {mode: event}
  channels:
    - {id: in, to: [lg]}
`, agent.URL)
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", graph, nil); c != 201 {
		t.Fatalf("create graph: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/external-run/start", adminTok, "", "", nil); c != 200 {
		t.Fatalf("start: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/external-run/channels/in/messages", adminTok, "application/json", `{"type":"task","payload":{}}`, nil); c != 201 {
		t.Fatalf("inject: %d", c)
	}
	select {
	case s := <-turns:
		if s.err != nil || s.reply != "OK" {
			t.Fatalf("callback chat: %q %v", s.reply, s.err)
		}
		if strings.Contains(s.input, providerKey) || !strings.Contains(s.input, `"provider":"team"`) {
			t.Fatalf("TurnInput must name the provider and carry no credential: %s", s.input)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("external runtime never received a turn")
	}
	if fake.LastAuthorization() != "Bearer "+providerKey {
		t.Fatalf("gateway must attach the stored credential: %q", fake.LastAuthorization())
	}

	// The gateway priced the callback call; the runtime's own usage is a
	// separate, unpriced external record. Neither carries the credential.
	type record struct {
		Source       string                                  `json:"source"`
		Provider     string                                  `json:"provider"`
		Execution    string                                  `json:"execution"`
		Priced       bool                                    `json:"priced"`
		Estimated    bool                                    `json:"estimated"`
		PriceVersion int                                     `json:"priceVersion"`
		CostMicros   int64                                   `json:"costMicros"`
		Usage        struct{ InputTokens, OutputTokens int } `json:"usage"`
	}
	var got map[string]record
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		evs, _ := a.Store.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{domain.EvCostRecorded}})
		got = map[string]record{}
		for _, ev := range evs {
			if strings.Contains(string(ev.Data), providerKey) {
				t.Fatal("cost record carries the credential")
			}
			var r record
			_ = json.Unmarshal(ev.Data, &r)
			if ev.GraphID != "external-run" || ev.ExecutionID == "" || r.Execution != ev.ExecutionID {
				t.Fatalf("cost record attribution: %+v %s", ev, ev.Data)
			}
			got[r.Source] = r
		}
		if len(got) == 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	gw, ext := got["gateway"], got["external"]
	if gw.Provider != "team" || !gw.Priced || gw.Estimated || gw.PriceVersion != 1 || gw.Usage.InputTokens != 12 || gw.CostMicros != 12+2 {
		t.Fatalf("gateway record = %+v", gw)
	}
	if ext.Priced || ext.CostMicros != 0 || ext.Usage.InputTokens != 100 || ext.Usage.OutputTokens != 50 {
		t.Fatalf("external record = %+v", ext)
	}
}
