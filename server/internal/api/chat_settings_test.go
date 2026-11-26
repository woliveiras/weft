package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestChatModelSettingsAPI(t *testing.T) {
	srv, app := setupApp(t, nil)
	const path = "/api/v1/chat/settings"
	var settings struct {
		Model    struct{ Provider, Name string }
		Revision int64
		Status   string
	}
	if status := call(t, srv, "GET", path, viewerTok, "", "", &settings); status != 200 || settings.Status != "unconfigured" || settings.Revision != 0 {
		t.Fatalf("initial settings: %d %+v", status, settings)
	}
	defaultName := app.Models.DefaultName()
	defaultProvider, found := app.Models.Get(defaultName)
	if !found {
		t.Fatal("fixture has no default provider")
	}
	provider := "fixture-chat"
	defaultEntry := llm.Entry{Config: llm.ProviderConfig{Name: defaultName}, Provider: defaultProvider}
	selectedEntry := llm.Entry{Config: llm.ProviderConfig{Name: provider}, Provider: defaultProvider}
	if err := app.Models.ReplaceProviders([]llm.Entry{defaultEntry, selectedEntry}, defaultName); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"provider": provider, "name": "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	put := func(token, revision string, payload []byte) int {
		t.Helper()
		req, err := http.NewRequest("PUT", srv.URL+path, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		if revision != "" {
			req.Header.Set("If-Match", revision)
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if status := put(viewerTok, "0", body); status != 403 {
		t.Fatalf("viewer PUT = %d", status)
	}
	if status := put(adminTok, "", body); status != 400 {
		t.Fatalf("missing revision PUT = %d", status)
	}
	if status := put(adminTok, "0", []byte(`{"provider":"not-configured","name":"test"}`)); status != 400 {
		t.Fatalf("unknown provider PUT = %d", status)
	}
	withTokenOverride := append(bytes.Clone(body[:len(body)-1]), []byte(`,"maxTokens":8192}`)...)
	if status := put(adminTok, "0", withTokenOverride); status != 400 {
		t.Fatalf("unexpected provider parameter PUT = %d", status)
	}
	if status := put(adminTok, "0", body); status != 200 {
		t.Fatalf("admin PUT = %d", status)
	}
	if status := put(adminTok, "0", body); status != 409 {
		t.Fatalf("stale PUT = %d", status)
	}
	if status := call(t, srv, "GET", path, viewerTok, "", "", &settings); status != 200 || settings.Status != "configured" || settings.Revision != 1 || settings.Model.Provider != provider || settings.Model.Name != "fixture-model" {
		t.Fatalf("saved settings: %d %+v", status, settings)
	}
	if app.Models.DefaultName() != defaultName {
		t.Fatal("Chat selection changed the workflow default")
	}
	if err := app.Models.ReplaceProviders([]llm.Entry{defaultEntry}, defaultName); err != nil {
		t.Fatal(err)
	}
	if status := call(t, srv, "GET", path, viewerTok, "", "", &settings); status != 200 || settings.Status != "unavailable" || settings.Model.Provider != provider || settings.Revision != 1 {
		t.Fatalf("missing dependency must preserve the pinned model: %d %+v", status, settings)
	}
}
