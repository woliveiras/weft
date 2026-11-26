package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/projectconfig"
)

func TestChatProjectSettingsUseExistingRepositoryAndPreserveUnrequestedFields(t *testing.T) {
	srv, app := setupApp(t, nil)
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-qm", "initial"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("prepare disposable Git repository: %v (%s)", err, output)
		}
	}
	m := projectconfig.Manager{Store: app.Store}
	project, err := m.Create(t.Context(), projectconfig.Input{Name: "Documentation project", Path: repo}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	const previousCommand = "printf 'existing check'"
	project, err = m.SetChecks(t.Context(), project.ID, []domain.Check{{Name: "docs", Command: previousCommand}}, project.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const base = "/api/v1/chat/sessions"
	propose := func(i int, input, document string) chat.Session {
		t.Helper()
		if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: installationChatFixture{document: document}}}, "chat-fixture"); err != nil {
			t.Fatal(err)
		}
		var session chat.Session
		body := fmt.Sprintf(`{"requestId":"%032x","text":%q}`, i, input)
		if code := call(t, srv, "POST", base, adminTok, "application/json", body, &session); code != http.StatusOK || len(session.Messages) != 2 || session.Messages[1].Review == nil {
			t.Fatalf("project review: %d %+v", code, session)
		}
		return session
	}
	confirm := func(session chat.Session) *chat.OperationReview {
		t.Helper()
		review := session.Messages[1].Review
		var result chat.Session
		path := base + "/" + session.ID + "/operations/" + review.ID + "/confirm"
		if status := call(t, srv, "POST", path, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &result); status != http.StatusOK {
			t.Fatalf("confirmation: %d", status)
		}
		return result.Messages[1].Review
	}
	mode := propose(1, "Set the registered project to default to sandbox.", yamlObject("Project", project.ID, "  executionMode: sandbox"))
	preview, err := json.Marshal(mode)
	if err != nil || strings.Contains(string(preview), project.Path) || strings.Contains(string(preview), previousCommand) {
		t.Fatalf("existing source or check leaked into conversation: %s %v", preview, err)
	}
	if receipt := confirm(mode); receipt.State != chat.ReviewApplied || len(receipt.ConfigReceipts) != 1 || receipt.ConfigReceipts[0].Result != "applied" {
		t.Fatalf("mode not applied: %+v", receipt)
	}
	after, err := m.Get(t.Context(), project.ID)
	if err != nil || after.ExecutionMode != "sandbox" || after.Checks[0].Command != previousCommand || after.Path != project.Path || after.Head != project.Head {
		t.Fatalf("mode change lost project settings: %+v %v", after, err)
	}
	const nextCheck = "  checks:\n    - name: docs\n      command: printf docs\n      timeoutSec: 20"
	stale := propose(2, "Set the project's docs check to printf docs with a 20 second timeout.", yamlObject("Project", project.ID, nextCheck))
	if _, err := m.SetExecutionPreference(t.Context(), project.ID, "workflow", after.Revision, "admin"); err != nil {
		t.Fatal(err)
	}
	if receipt := confirm(stale); receipt.State != chat.ReviewConflict {
		t.Fatalf("stale project review: %+v", receipt)
	}
	fresh := propose(3, "Set the project's docs check to printf docs with a 20 second timeout.", yamlObject("Project", project.ID, nextCheck))
	for range 2 {
		if receipt := confirm(fresh); receipt.State != chat.ReviewApplied || len(receipt.ConfigReceipts) != 1 {
			t.Fatalf("check not applied: %+v", receipt)
		}
	}
	final, err := m.Get(t.Context(), project.ID)
	if err != nil || final.ExecutionMode != "workflow" || len(final.Checks) != 1 || final.Checks[0].Command != "printf docs" || final.Checks[0].TimeoutSec != 20 || final.Revision != after.Revision+2 {
		t.Fatalf("wrong native result: %+v %v", final, err)
	}
	if got, err := m.List(t.Context()); err != nil || len(got) != 1 || got[0].ID != project.ID {
		t.Fatalf("source lookup duplicated the project: %+v %v", got, err)
	}
}
