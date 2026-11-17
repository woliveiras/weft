package llmconfig_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/llm/llmtest"
	_ "github.com/woliveiras/weft/server/internal/llm/providers"
	"github.com/woliveiras/weft/server/internal/llmconfig"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

var ctx = context.Background()

const secretKey = "sk-test"

func key(b byte) []byte { return bytes.Repeat([]byte{b}, secrets.KeySize) }

type env struct {
	store storage.Store
	reg   *llm.Registry
	box   *secrets.Box
	m     *llmconfig.Manager
	bus   *events.Bus
	fake  *llmtest.Server
	inUse map[string][]string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, err := secrets.NewBox(key(1))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{store: st, box: box, fake: llmtest.New(t, secretKey), inUse: map[string][]string{}}
	e.m = e.manager(t, box)
	e.enable(t, "openai", "ollama", "vllm")
	return e
}

func (e *env) manager(t *testing.T, box *secrets.Box) *llmconfig.Manager {
	t.Helper()
	static := []llm.ProviderConfig{{Name: "offline", Type: "mock", DefaultModel: "mock-1"}}
	reg, err := llm.NewRegistry(static, "offline")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewBus(e.store, log)
	m, err := llmconfig.New(llmconfig.Options{
		Store: e.store, Box: box, KeySource: secrets.SourceEnv, Registry: reg, Static: static, StaticDefault: "offline",
		Bus: bus, Log: log, TestTimeout: 2 * time.Second,
		InUse: func(_ context.Context, p string) ([]string, error) { return e.inUse[p], nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	e.reg, e.bus = reg, bus
	return m
}

// enable turns plugins on as an administrator.
func (e *env) enable(t *testing.T, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := e.m.SetPluginEnabled(ctx, id, true, "admin", 0); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *env) openaiInput(name string) llmconfig.ProviderInput {
	return llmconfig.ProviderInput{
		Name: name, Type: "openai", BaseURL: e.fake.BaseURL(), DefaultModel: "fake-small", MaxRetries: -1,
		APIKey: &llmconfig.CredentialUpdate{Value: secretKey},
	}
}

func issues(t *testing.T, err error) map[string]string {
	t.Helper()
	var ve *llmconfig.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	out := map[string]string{}
	for _, i := range ve.Errors {
		out[i.Path] = i.Message
	}
	return out
}

func TestCreateStoresSealedCredentialAndServesThroughRegistry(t *testing.T) {
	e := newEnv(t)
	v, err := e.m.Create(ctx, e.openaiInput("team-openai"), "alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != llmconfig.SourceManaged || !v.Editable || !v.Credential.Configured || v.Credential.Source != "stored" || v.Status != "ready" {
		t.Fatalf("view = %+v", v)
	}
	raw, err := e.store.GetSetting(ctx, llmconfig.SettingKey)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw.Value, []byte(secretKey)) || raw.UpdatedBy != "alice" {
		t.Fatalf("stored document leaks the credential or misses the author: %s", raw.Value)
	}
	b, _ := json.Marshal(e.m.List())
	if bytes.Contains(b, []byte(secretKey)) || bytes.Contains(b, []byte("v1.")) {
		t.Fatalf("list leaks credential material: %s", b)
	}
	// Agents select it by name; the gateway sends the decrypted key.
	p, model, err := e.reg.Resolve(domain.ModelRef{Provider: "team-openai"})
	if err != nil || model != "fake-small" {
		t.Fatalf("resolve: %v %q", err, model)
	}
	resp, err := e.reg.Chat(ctx, p.Name(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}})
	if err != nil || resp.Message.Content != "OK" {
		t.Fatalf("chat: %v %+v", err, resp)
	}
	if e.fake.LastAuthorization() != "Bearer "+secretKey {
		t.Fatalf("provider received %q", e.fake.LastAuthorization())
	}
	all, _ := e.store.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvProviderChanged}})
	var evs []*domain.Event
	for _, ev := range all {
		if ev.Actor != "admin" { // plugin activation by newEnv
			evs = append(evs, ev)
		}
	}
	if len(evs) != 1 || evs[0].Actor != "alice" || bytes.Contains(evs[0].Data, []byte(secretKey)) {
		t.Fatalf("audit events = %+v", evs)
	}
}

func TestCreateValidation(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.Create(ctx, e.openaiInput("dup"), "a", 0); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		in   llmconfig.ProviderInput
		path string
	}{
		"bad name":      {llmconfig.ProviderInput{Name: "Bad Name", Type: "mock"}, "name"},
		"reserved":      {llmconfig.ProviderInput{Name: "default", Type: "mock"}, "name"},
		"duplicate":     {llmconfig.ProviderInput{Name: "dup", Type: "mock"}, "name"},
		"config clash":  {llmconfig.ProviderInput{Name: "offline", Type: "mock"}, "name"},
		"unknown type":  {llmconfig.ProviderInput{Name: "x", Type: "gpt-cloud"}, "type"},
		"bad url":       {llmconfig.ProviderInput{Name: "x", Type: "ollama", BaseURL: "ftp://host"}, "baseUrl"},
		"url creds":     {llmconfig.ProviderInput{Name: "x", Type: "ollama", BaseURL: "http://u:p@host"}, "baseUrl"},
		"timeout":       {llmconfig.ProviderInput{Name: "x", Type: "mock", TimeoutSec: 99999}, "timeoutSec"},
		"retries":       {llmconfig.ProviderInput{Name: "x", Type: "mock", MaxRetries: 50}, "maxRetries"},
		"concurrency":   {llmconfig.ProviderInput{Name: "x", Type: "mock", Concurrency: -1}, "concurrency"},
		"clear and set": {llmconfig.ProviderInput{Name: "x", Type: "mock", APIKey: &llmconfig.CredentialUpdate{Value: "k", Clear: true}}, "apiKey"},
		"empty key":     {llmconfig.ProviderInput{Name: "x", Type: "mock", APIKey: &llmconfig.CredentialUpdate{Value: "  "}}, "apiKey"},
		"multiline":     {llmconfig.ProviderInput{Name: "x", Type: "mock", APIKey: &llmconfig.CredentialUpdate{Value: "a\nb"}}, "apiKey"},
		"hosted no key": {llmconfig.ProviderInput{Name: "x", Type: "openai"}, "apiKey"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := e.m.Create(ctx, c.in, "a", 0)
			if got := issues(t, err); got[c.path] == "" {
				t.Fatalf("want an issue at %q, got %v", c.path, got)
			}
		})
	}
	if names := e.reg.Names(); len(names) != 2 {
		t.Fatalf("rejected inputs must not reach the registry: %v", names)
	}
}

func TestWithoutEncryptionKeyCredentialsAreRefused(t *testing.T) {
	e := newEnv(t)
	m := e.manager(t, nil)
	_, err := m.Create(ctx, e.openaiInput("nokey"), "a", 0)
	if !errors.Is(err, secrets.ErrNoKey) || !strings.Contains(err.Error(), "WEFT_SECRET_KEY") {
		t.Fatalf("want actionable ErrNoKey, got %v", err)
	}
	if raw, err := e.store.GetSetting(ctx, llmconfig.SettingKey); err != nil || bytes.Contains(raw.Value, []byte("nokey")) {
		t.Fatalf("no provider must be stored: %v", err)
	}
	// Keyless backends can still be managed.
	if _, err := m.Create(ctx, llmconfig.ProviderInput{Name: "local", Type: "ollama", BaseURL: "http://ollama:11434"}, "a", 0); err != nil {
		t.Fatal(err)
	}
	if meta := m.Meta(); meta.Secrets.Enabled {
		t.Fatalf("meta must report secrets disabled: %+v", meta.Secrets)
	}
}

func TestUpdateKeepsReplacesAndClearsCredential(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.Create(ctx, e.openaiInput("p"), "a", 0); err != nil {
		t.Fatal(err)
	}
	// Omitted credential: kept.
	in := e.openaiInput("p")
	in.APIKey, in.DefaultModel = nil, "fake-large"
	v, err := e.m.Update(ctx, "p", in, "bob", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Credential.Configured || v.DefaultModel != "fake-large" || v.UpdatedBy != "bob" {
		t.Fatalf("after keep: %+v", v)
	}
	if _, err := e.reg.Chat(ctx, "p", &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "x"}}}); err != nil {
		t.Fatalf("kept credential must still authenticate: %v", err)
	}
	// Replaced credential: the provider uses the new value at once.
	in.APIKey = &llmconfig.CredentialUpdate{Value: "sk-rotated-999999"}
	if _, err := e.m.Update(ctx, "p", in, "bob", 0); err != nil {
		t.Fatal(err)
	}
	_, err = e.reg.Chat(ctx, "p", &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "x"}}})
	if err == nil || e.fake.LastAuthorization() != "Bearer sk-rotated-999999" {
		t.Fatalf("replaced credential not in use: %v %q", err, e.fake.LastAuthorization())
	}
	// Clearing is refused where the type needs a key ...
	in.APIKey = &llmconfig.CredentialUpdate{Clear: true}
	if got := issues(t, ignoreView(e.m.Update(ctx, "p", in, "bob", 0))); got["apiKey"] == "" {
		t.Fatalf("clearing a required key: %v", got)
	}
	// ... and allowed where it does not.
	in.Type = "vllm"
	v, err = e.m.Update(ctx, "p", in, "bob", 0)
	if err != nil || v.Credential.Configured || v.Credential.Source != "none" {
		t.Fatalf("after clear: %+v %v", v, err)
	}
	if _, err := e.m.Update(ctx, "p", llmconfig.ProviderInput{Name: "q", Type: "mock"}, "bob", 0); issues(t, err)["name"] == "" {
		t.Fatal("rename must be refused")
	}
	if _, err := e.m.Update(ctx, "offline", llmconfig.ProviderInput{Type: "mock"}, "bob", 0); issues(t, err)["name"] == "" {
		t.Fatal("config providers are read-only")
	}
	if _, err := e.m.Update(ctx, "ghost", llmconfig.ProviderInput{Type: "mock"}, "bob", 0); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("unknown provider: %v", err)
	}
}

func ignoreView(_ *llmconfig.ProviderView, err error) error { return err }

func TestStaleRevisionConflicts(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.Create(ctx, llmconfig.ProviderInput{Name: "a", Type: "mock"}, "u", 0); err != nil {
		t.Fatal(err)
	}
	rev := e.m.Meta().Revision
	if _, err := e.m.Create(ctx, llmconfig.ProviderInput{Name: "b", Type: "mock"}, "u", rev); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Create(ctx, llmconfig.ProviderInput{Name: "c", Type: "mock"}, "u", rev); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	if _, err := e.m.Get("c"); err == nil {
		t.Fatal("conflicting write must not apply")
	}
}

func TestDeleteAndDefault(t *testing.T) {
	e := newEnv(t)
	for _, n := range []string{"a", "b"} {
		if _, err := e.m.Create(ctx, llmconfig.ProviderInput{Name: n, Type: "mock", DefaultModel: n + "-model"}, "u", 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.m.SetDefault(ctx, "ghost", "u", 0); issues(t, err)["default"] == "" {
		t.Fatal("unknown default must be refused")
	}
	meta, err := e.m.SetDefault(ctx, "a", "u", 0)
	if err != nil || meta.Default != "a" {
		t.Fatalf("set default: %+v %v", meta, err)
	}
	if _, model, _ := e.reg.Resolve(domain.ModelRef{}); model != "a-model" {
		t.Fatalf("nodes without a provider must use the new default, got %q", model)
	}
	if err := e.m.Delete(ctx, "a", "u", 0); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("deleting the default: %v", err)
	}
	e.inUse["b"] = []string{"graph demo / node writer"}
	if err := e.m.Delete(ctx, "b", "u", 0); !errors.Is(err, storage.ErrConflict) || !strings.Contains(err.Error(), "node writer") {
		t.Fatalf("deleting a provider in use: %v", err)
	}
	delete(e.inUse, "b")
	if err := e.m.Delete(ctx, "b", "u", 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.reg.Resolve(domain.ModelRef{Provider: "b"}); err == nil {
		t.Fatal("deleted provider must be gone from the registry")
	}
	if err := e.m.Delete(ctx, "offline", "u", 0); issues(t, err)["name"] == "" {
		t.Fatal("config providers cannot be deleted")
	}
	if err := e.m.Delete(ctx, "b", "u", 0); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if meta, err := e.m.SetDefault(ctx, "", "u", 0); err != nil || meta.Default != "offline" {
		t.Fatalf("clearing the default returns to the config default: %+v %v", meta, err)
	}
}

func TestDefaultModelMatchesPrefixedListings(t *testing.T) {
	e := newEnv(t)
	e.fake.Models = []string{"models/gemini-3.5-flash", "models/gemini-flash-latest"}
	in := e.openaiInput("g")
	in.DefaultModel = "gemini-3.5-flash"
	if _, err := e.m.Create(ctx, in, "a", 0); err != nil {
		t.Fatal(err)
	}
	res, err := e.m.Test(ctx, "g", "op")
	if err != nil {
		t.Fatal(err)
	}
	if res.Models.DefaultModelListed == nil || !*res.Models.DefaultModelListed {
		t.Fatalf("a model listed as models/<id> must count as listed: %+v", res.Models)
	}
	other := e.openaiInput("h")
	other.DefaultModel = "gemini-9-ultra"
	if _, err := e.m.Create(ctx, other, "a", 0); err != nil {
		t.Fatal(err)
	}
	if res, _ = e.m.Test(ctx, "h", "op"); res.Models.DefaultModelListed == nil || *res.Models.DefaultModelListed {
		t.Fatalf("an unlisted model must still be reported: %+v", res.Models)
	}
}

func TestConnectionTestUsesInference(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.Create(ctx, e.openaiInput("p"), "a", 0); err != nil {
		t.Fatal(err)
	}
	res, err := e.m.Test(ctx, "p", "op")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Inference.OK || res.Inference.Reply != "OK" || !res.Models.OK || res.Models.Count != 2 ||
		res.Models.DefaultModelListed == nil || !*res.Models.DefaultModelListed || res.Model != "fake-small" {
		t.Fatalf("result = %+v", res)
	}
	if e.fake.Chats() != 2 || e.fake.LastChat()["model"] != "fake-small" {
		t.Fatalf("the test must send a real chat request, then a tool-calling one: %d %v", e.fake.Chats(), e.fake.LastChat())
	}
	// The fake answers tool offers in text, like a model without tool calling.
	if !res.Tools.Checked || res.Tools.OK || res.Tools.Reply != "OK" || !strings.Contains(res.Tools.Error, "tool calling") {
		t.Fatalf("tools without support: %+v", res.Tools)
	}
	e.fake.ToolCalls.Store(true)
	if res, err := e.m.Test(ctx, "p", "op"); err != nil || !res.OK || !res.Tools.OK || res.Tools.Error != "" {
		t.Fatalf("tools with support: %+v %v", res, err)
	}
	if v, _ := e.m.Get("p"); v.LastTest == nil || !v.LastTest.OK {
		t.Fatalf("last test must be shown: %+v", v)
	}
	// Static providers can be tested too.
	if res, err := e.m.Test(ctx, "offline", "op"); err != nil || !res.OK {
		t.Fatalf("static test: %+v %v", res, err)
	}
	if _, err := e.m.Test(ctx, "ghost", "op"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestConnectionTestFailuresAreRedactedAndBounded(t *testing.T) {
	e := newEnv(t)
	in := e.openaiInput("p")
	in.APIKey = &llmconfig.CredentialUpdate{Value: "sk-wrong-key"}
	if _, err := e.m.Create(ctx, in, "a", 0); err != nil {
		t.Fatal(err)
	}
	res, err := e.m.Test(ctx, "p", "op")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Inference.Error == "" || !strings.Contains(res.Inference.Error, "401") || res.Tools.Checked {
		t.Fatalf("wrong key (tools are not tried without inference): %+v", res)
	}
	b, _ := json.Marshal(res)
	evs, _ := e.store.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvProviderTested}})
	eb, _ := json.Marshal(evs)
	if bytes.Contains(b, []byte("sk-wrong-key")) || bytes.Contains(eb, []byte("sk-wrong-key")) || !bytes.Contains(b, []byte("[redacted]")) {
		t.Fatalf("credential echoed by the provider must be redacted: %s %s", b, eb)
	}
	if len(evs) != 1 || evs[0].Level != "warn" {
		t.Fatalf("failed test must be audited as a warning: %+v", evs)
	}

	in.APIKey = &llmconfig.CredentialUpdate{Value: secretKey}
	if _, err := e.m.Update(ctx, "p", in, "a", 0); err != nil {
		t.Fatal(err)
	}
	e.fake.FailChat.Store(true)
	if res, _ := e.m.Test(ctx, "p", "op"); res.OK || !res.Models.OK || !strings.Contains(res.Inference.Error, "500") {
		t.Fatalf("model listing alone must not pass a test: %+v", res)
	}
	e.fake.FailChat.Store(false)
	e.fake.Delay = 5 * time.Second
	start := time.Now()
	res, _ = e.m.Test(ctx, "p", "op")
	if res.OK || !strings.Contains(res.Inference.Error, "timed out") || time.Since(start) > 4*time.Second {
		t.Fatalf("timeout: %+v after %s", res.Inference, time.Since(start))
	}
}

func TestDraftTest(t *testing.T) {
	e := newEnv(t)
	res, err := e.m.TestDraft(ctx, e.openaiInput("not-saved"), "a")
	if err != nil || !res.OK || !res.Draft {
		t.Fatalf("draft: %+v %v", res, err)
	}
	if _, err := e.m.Get("not-saved"); err == nil {
		t.Fatal("a draft test must not save anything")
	}
	if _, err := e.m.Create(ctx, e.openaiInput("saved"), "a", 0); err != nil {
		t.Fatal(err)
	}
	draft := e.openaiInput("saved")
	draft.APIKey, draft.DefaultModel = nil, "fake-large"
	res, err = e.m.TestDraft(ctx, draft, "a")
	if err != nil || !res.OK || res.Model != "fake-large" {
		t.Fatalf("draft of a saved provider must reuse its credential: %+v %v", res, err)
	}
	if _, err := e.m.TestDraft(ctx, llmconfig.ProviderInput{Type: "openai", BaseURL: e.fake.BaseURL()}, "a"); issues(t, err)["apiKey"] == "" {
		t.Fatal("draft without a required key must be rejected")
	}
}

func TestKeyRotationAndLostKey(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.Create(ctx, e.openaiInput("p"), "a", 0); err != nil {
		t.Fatal(err)
	}
	// New primary key, old key kept as previous: values are re-sealed on load.
	rotated, _ := secrets.NewBox(key(2), key(1))
	m2 := e.manager(t, rotated)
	raw, _ := e.store.GetSetting(ctx, llmconfig.SettingKey)
	if !bytes.Contains(raw.Value, []byte("v1."+rotated.PrimaryKeyID()+".")) || raw.UpdatedBy != "system:key-rotation" {
		t.Fatalf("credential not re-sealed with the new key: %s", raw.Value)
	}
	if res, _ := m2.Test(ctx, "p", "op"); !res.OK {
		t.Fatalf("provider must work after rotation: %+v", res)
	}
	// The old key alone can no longer open it: explicit, actionable failure.
	lost, _ := secrets.NewBox(key(1))
	m3 := e.manager(t, lost)
	v, err := m3.Get("p")
	if err != nil || v.Status != "unavailable" || !strings.Contains(v.Error, "re-enter") || v.Credential.Error == "" {
		t.Fatalf("lost key view: %+v %v", v, err)
	}
	if _, _, err := e.reg.Resolve(domain.ModelRef{Provider: "p"}); !errors.Is(err, llm.ErrUnavailable) {
		t.Fatalf("agents selecting it must get the reason, not another provider: %v", err)
	}
	if _, _, err := e.reg.Resolve(domain.ModelRef{}); err != nil {
		t.Fatalf("other providers keep working: %v", err)
	}
	// Re-entering the credential repairs it.
	in := e.openaiInput("p")
	if v, err := m3.Update(ctx, "p", in, "a", 0); err != nil || v.Status != "ready" {
		t.Fatalf("repair: %+v %v", v, err)
	}
}

func TestRefreshPicksUpOtherInstances(t *testing.T) {
	e := newEnv(t)
	other := e.manager(t, e.box) // shares the store, has its own registry
	otherReg := e.reg
	if _, err := e.m.Create(ctx, llmconfig.ProviderInput{Name: "late", Type: "mock"}, "u", 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := otherReg.Resolve(domain.ModelRef{Provider: "late"}); err == nil {
		t.Fatal("precondition: the other instance has not refreshed yet")
	}
	if err := other.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := otherReg.Resolve(domain.ModelRef{Provider: "late"}); err != nil {
		t.Fatalf("after refresh: %v", err)
	}
}

func TestCorruptStoredDocumentFailsLoad(t *testing.T) {
	e := newEnv(t)
	cur, _ := e.store.GetSetting(ctx, llmconfig.SettingKey)
	if _, err := e.store.PutSetting(ctx, llmconfig.SettingKey, []byte("{not json"), cur.Revision, ""); err != nil {
		t.Fatal(err)
	}
	reg, _ := llm.NewRegistry(nil, "")
	m, _ := llmconfig.New(llmconfig.Options{Store: e.store, Registry: reg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := m.Load(ctx); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("corrupt settings must fail loudly: %v", err)
	}
}

func TestContextWindowIsValidatedAndKept(t *testing.T) {
	e := newEnv(t)
	in := e.openaiInput("p")
	in.ContextWindow = 512
	if _, err := e.m.Create(ctx, in, "a", 0); issues(t, err)["contextWindow"] == "" {
		t.Fatal("a window below 1024 tokens must be refused")
	}
	in.ContextWindow = 65536
	if _, err := e.m.Create(ctx, in, "a", 0); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.m.Get("p"); v.ContextWindow != 65536 {
		t.Fatalf("view: %+v", v)
	}
}

func TestPluginsStartDisabledAndDisablingKeepsInstancesUnavailable(t *testing.T) {
	e := newEnv(t)
	views := map[string]llmconfig.PluginView{}
	for _, v := range e.m.Plugins() {
		views[v.ID] = v
	}
	if v := views["anthropic"]; v.Enabled || v.EnabledBy != "" || v.UpdatedAt != nil {
		t.Fatalf("anthropic must start disabled: %+v", v)
	}
	if v := views["mock"]; !v.Enabled || v.EnabledBy != "config" || len(v.Instances) != 1 || v.Instances[0] != "offline" {
		t.Fatalf("mock is provided by configuration: %+v", v)
	}
	if v := views["openai"]; !v.Enabled || v.EnabledBy != "console" || v.UpdatedBy != "admin" || v.UpdatedAt == nil {
		t.Fatalf("openai enabled in the console: %+v", v)
	}
	if _, err := e.m.Create(ctx, llmconfig.ProviderInput{Name: "claude", Type: "anthropic", APIKey: &llmconfig.CredentialUpdate{Value: secretKey}}, "a", 0); !strings.Contains(issues(t, err)["type"], "plugin is disabled") {
		t.Fatalf("disabled plugin instance: %v", err)
	}
	if _, err := e.m.TestDraft(ctx, llmconfig.ProviderInput{Type: "anthropic", APIKey: &llmconfig.CredentialUpdate{Value: secretKey}}, "a"); !strings.Contains(issues(t, err)["type"], "plugin is disabled") {
		t.Fatalf("disabled plugin draft: %v", err)
	}
	if _, err := e.m.SetPluginEnabled(ctx, "mock", false, "admin", 0); !strings.Contains(issues(t, err)["enabled"], "server configuration") {
		t.Fatalf("config plugin disable: %v", err)
	}
	if _, err := e.m.SetPluginEnabled(ctx, "nope", true, "admin", 0); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("unknown plugin: %v", err)
	}
	if _, err := e.m.SetPluginEnabled(ctx, "openai", true, "admin", 999); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}

	if _, err := e.m.Create(ctx, e.openaiInput("team"), "a", 0); err != nil {
		t.Fatal(err)
	}
	v, err := e.m.SetPluginEnabled(ctx, "OpenAI", false, "bob", 0)
	if err != nil || v.Enabled || len(v.Instances) != 1 || v.UpdatedBy != "bob" {
		t.Fatalf("disable: %+v %v", v, err)
	}
	// The instance stays configured but resolves to an explicit reason
	// instead of being routed elsewhere.
	if _, _, err := e.reg.Resolve(domain.ModelRef{Provider: "team"}); !errors.Is(err, llm.ErrUnavailable) || !strings.Contains(err.Error(), "plugin is disabled") {
		t.Fatalf("resolve with disabled plugin: %v", err)
	}
	pv, _ := e.m.Get("team")
	if pv.Status != "unavailable" || !strings.Contains(pv.Error, "Inference plugins") {
		t.Fatalf("view: %+v", pv)
	}
	// Another instance (or a restart) sees the same state.
	other := e.manager(t, e.box)
	if p, _ := e.reg.Get("team"); llm.UnavailableReason(p) == nil {
		t.Fatalf("reloaded manager must keep the plugin disabled")
	}
	if _, err := other.SetPluginEnabled(ctx, "openai", true, "bob", 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.reg.Resolve(domain.ModelRef{Provider: "team"}); err != nil {
		t.Fatalf("re-enabled plugin: %v", err)
	}
	evs, _ := e.store.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvProviderChanged}})
	var actions []string
	for _, ev := range evs {
		var data map[string]any
		_ = json.Unmarshal(ev.Data, &data)
		if p, _ := data["plugin"].(string); p == "openai" && ev.Actor == "bob" {
			actions = append(actions, data["action"].(string))
		}
	}
	sort.Strings(actions)
	if strings.Join(actions, ",") != "plugin.disabled,plugin.enabled" {
		t.Fatalf("plugin audit = %v", actions)
	}
}

func TestModelGuidanceIsValidatedAndReachesTheRegistry(t *testing.T) {
	e := newEnv(t)
	in := e.openaiInput("p")
	in.ModelGuidance = "tiny"
	if _, err := e.m.Create(ctx, in, "a", 0); issues(t, err)["modelGuidance"] == "" {
		t.Fatal("an unknown guidance must be refused")
	}
	in.ModelGuidance = llm.GuidanceSmall
	if _, err := e.m.Create(ctx, in, "a", 0); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.m.Get("p"); v.ModelGuidance != llm.GuidanceSmall {
		t.Fatalf("view: %+v", v)
	}
	if g := e.reg.ModelGuidance("p"); g != llm.GuidanceSmall {
		t.Fatalf("registry guidance %q", g)
	}
}

// A Vertex AI provider keeps its project and region through the console:
// validated, stored, reloaded, and used by the registered provider, which
// authenticates with the server's Application Default Credentials (here
// the metadata server of a Google Cloud machine) and never with a stored key.
func TestVertexProjectAndRegionAreValidatedKeptAndUsed(t *testing.T) {
	metadata := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" {
			http.Error(w, "missing flavor", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"vertex-token","expires_in":3600}`)
	}))
	t.Cleanup(metadata.Close)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(metadata.URL, "http://"))
	var lastPath atomic.Value
	vertex := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath.Store(r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer vertex-token" {
			http.Error(w, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"publisherModels":[{"name":"publishers/google/models/gemini-test"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1}}`)
	}))
	t.Cleanup(vertex.Close)

	e := newEnv(t)
	e.enable(t, "gemini")
	in := llmconfig.ProviderInput{Name: "vertex", Type: "gemini", BaseURL: vertex.URL, DefaultModel: "gemini-test", MaxRetries: -1, Project: "weft-test", Region: "europe-west4"}

	for name, c := range map[string]struct {
		mutate func(*llmconfig.ProviderInput)
		path   string
	}{
		"project on a type without it": {func(i *llmconfig.ProviderInput) {
			i.Type, i.APIKey = "openai", &llmconfig.CredentialUpdate{Value: secretKey}
		}, "project"},
		"malformed region":  {func(i *llmconfig.ProviderInput) { i.Region = "Europe West" }, "region"},
		"vertex with a key": {func(i *llmconfig.ProviderInput) { i.APIKey = &llmconfig.CredentialUpdate{Value: "AIza-key"} }, "type"},
	} {
		t.Run(name, func(t *testing.T) {
			bad := in
			c.mutate(&bad)
			if _, err := e.m.Create(ctx, bad, "a", 0); issues(t, err)[c.path] == "" {
				t.Fatalf("want an issue at %q, got %v", c.path, err)
			}
		})
	}

	res, err := e.m.TestDraft(ctx, in, "a")
	if err != nil || !res.OK || !res.Models.OK {
		t.Fatalf("draft test: %+v %v", res, err)
	}
	if _, err := e.m.Create(ctx, in, "a", 0); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.m.Get("vertex"); v.Project != "weft-test" || v.Region != "europe-west4" || v.Credential.Configured {
		t.Fatalf("view: %+v", v)
	}
	var settings []string
	for _, ti := range llmconfig.Types() {
		if ti.Type == "gemini" {
			settings = ti.Settings
		}
	}
	if strings.Join(settings, ",") != "contextWindow,project,region" {
		t.Fatalf("gemini form settings = %v", settings)
	}

	// A fresh manager reads the stored provider back and serves it.
	m2 := e.manager(t, e.box)
	if v, _ := m2.Get("vertex"); v.Project != "weft-test" || v.Region != "europe-west4" {
		t.Fatalf("reloaded view: %+v", v)
	}
	p, ok := e.reg.Get("vertex")
	if !ok {
		t.Fatal("provider not registered")
	}
	resp, err := p.Chat(ctx, &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "Say OK"}}})
	if err != nil || resp.Message.Content != "OK" || resp.Usage.InputTokens != 3 {
		t.Fatalf("chat: %+v %v", resp, err)
	}
	if last, _ := lastPath.Load().(string); last != "/v1/projects/weft-test/locations/europe-west4/publishers/google/models/gemini-test:generateContent" {
		t.Fatalf("path = %s", last)
	}
}
