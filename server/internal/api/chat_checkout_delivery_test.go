package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type editCheckoutFixture struct{ chatAgentFixture }

func (editCheckoutFixture) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "replace", Name: "edit_task_draft", Arguments: json.RawMessage(`{"task":"Read code","checkoutAlias":"docs","replacementProject":"replacement"}`)}}}}, nil
}

func TestChatConfirmsExplicitReplacementDeliveryAgainstCurrentOrigin(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "fixture", Type: "mock"}, Provider: editCheckoutFixture{}}}, "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	if status := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); status != 201 {
		t.Fatal(status)
	}
	var in taskconfig.Input
	if err := json.Unmarshal([]byte(taskBody), &in); err != nil {
		t.Fatal(err)
	}
	in.ChannelID, in.MessageType, in.PayloadJSON = "input", "request", `{"keep":["custom"]}`
	var replacement scm.Input
	var replacementID string
	for _, alias := range []string{"docs", "app", "replacement"} {
		r, remote := memberInput(t, srv, alias, map[string]string{"file": alias})
		token := "synthetic-s49"
		conn := scm.Input{Name: alias, Kind: "github", Repository: "test/" + alias, GitURL: "file://" + remote, Token: &token}
		c, err := app.SCM.Save(t.Context(), conn, "fixture", false)
		if err != nil {
			t.Fatal(err)
		}
		r.SCMConnection, r.Repository, r.TargetBranch, r.ConnectionRevision = c.Name, c.Repository, "main", c.UpdatedAt.UTC().Format(time.RFC3339Nano)
		if alias == "replacement" {
			replacement = conn
			replacementID = r.ProjectID
		} else {
			in.Repositories = append(in.Repositories, r)
		}
	}
	manager := taskconfig.Manager{Store: app.Store}
	d, err := manager.Save(t.Context(), "", in, 0, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	const route = "/api/v1/chat/sessions"
	sequence := 0
	propose := func(token, suffix string) chat.Session {
		t.Helper()
		sequence++
		body := fmt.Sprintf(`{"requestId":"%032x","text":%q}`, sequence, "Replace docs with replacement "+suffix)
		var session chat.Session
		if status := call(t, srv, "POST", route, token, "application/json", body, &session); status != 200 || len(session.Messages) != 2 {
			t.Fatal(status, session)
		}
		return session
	}
	confirm := func(s chat.Session) *chat.OperationReview {
		t.Helper()
		r := s.Messages[1].Review
		var saved chat.Session
		if status := call(t, srv, "POST", route+"/"+s.ID+"/operations/"+r.ID+"/confirm", operatorTok, "application/json", fmt.Sprintf(`{"digest":%q}`, r.Digest), &saved); status != 200 {
			t.Fatal(status)
		}
		return saved.Messages[1].Review
	}
	for _, suffix := range []string{"", "connection=replacement", "connection=docs branch=main", "connection=missing branch=main"} {
		session := propose(operatorTok, suffix)
		if m := session.Messages[1]; m.Review != nil || !strings.Contains(m.Text, "branch=") {
			t.Fatal(suffix, m)
		}
	}
	const choice = "connection=replacement branch=release/v2"
	if m := propose(viewerTok, choice).Messages[1]; m.Review != nil {
		t.Fatal("viewer received a draft review")
	}
	stale := propose(operatorTok, choice)
	if r := stale.Messages[1].Review; r == nil || r.TaskCorrection == nil || r.TaskCorrection.Repository == nil {
		t.Fatal(stale.Messages)
	}
	replacement.Token = nil
	replacement.BranchPrefix = "review/"
	if _, err := app.SCM.Save(t.Context(), replacement, "fixture", true); err != nil {
		t.Fatal(err)
	}
	if r := confirm(stale); r.State != chat.ReviewConflict {
		t.Fatal("applied stale destination", r)
	}
	before, err := manager.Get(t.Context(), d.ID)
	if err != nil || before.Revision != d.Revision || !reflect.DeepEqual(before.Repositories, d.Repositories) {
		t.Fatal(before, err)
	}
	session := propose(operatorTok, choice)
	change := session.Messages[1].Review.TaskCorrection.Repository
	if change.Before.ProjectID != d.Repositories[0].ProjectID || change.Before.Repository != "test/docs" || change.After.ProjectID != replacementID || change.After.Repository != "test/replacement" || change.After.TargetBranch != "release/v2" {
		t.Fatal(change)
	}
	for range 2 {
		if r := confirm(session); r.State != chat.ReviewApplied || r.ResultVersion != 2 {
			t.Fatal(r)
		}
	}
	got, err := manager.Get(t.Context(), d.ID)
	if err != nil || got.PayloadJSON != in.PayloadJSON || got.Revision != 2 || !reflect.DeepEqual(got.Repositories, []domain.TaskRepository{change.After, in.Repositories[1]}) {
		t.Fatal(got, err)
	}
}
