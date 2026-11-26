package api_test

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

func TestChatEvalAgentCreation(t *testing.T) {
	const (
		catalogPath = "/api/v1/agents"
		agentID     = "release-reviewer"
		name        = "Revisor de Releases"
		question    = "Crie um agente reutilizável chamado Revisor de Releases, com o ID release-reviewer. Ele deve revisar notas de versão, destacar riscos de compatibilidade e checar as instruções de atualização."
	)
	e := newChatEval(t, func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
		if last := req.Messages[len(req.Messages)-1]; last.Role != llm.RoleUser || last.Content != question {
			return nil, fmt.Errorf("unexpected inference during the catalog review or its confirmation")
		}
		return evalTool("create_catalog_agent", map[string]string{"agentId": agentID, "agentName": name,
			"instructions": "Revise as notas de versão. Destaque riscos de compatibilidade e confira as instruções de atualização."})
	})
	catalog := func() []agentcatalog.Summary {
		t.Helper()
		var entries []agentcatalog.Summary
		if status := call(t, e.srv, "GET", catalogPath, viewerTok, "", "", &entries); status != http.StatusOK {
			t.Fatalf("list catalog: %d", status)
		}
		return entries
	}
	before := catalog()
	session := e.ask(t, "", adminTok, question)
	review := session.Messages[len(session.Messages)-1].Review
	if review == nil || review.State != chat.ReviewAwaiting || review.AgentCreation == nil || review.Digest == "" {
		t.Fatalf("missing catalog review: %+v", session.Messages[len(session.Messages)-1])
	}
	def := review.Definition
	if review.AgentID != agentID || def.Name != name || def.Model.Provider != e.provider || def.Model.Name != e.model || def.Runtime.Kind != domain.RuntimeNative || !def.ReadOnly || len(def.Tools) != 0 {
		t.Fatalf("incorrect proposed agent or execution scope: %+v", review.AgentCreation)
	}
	text := strings.ToLower(def.Prompt)
	releaseNotes := strings.Contains(text, "notas de versão") || strings.Contains(text, "notas de lançamento") || strings.Contains(text, "release notes")
	compatibility := strings.Contains(text, "compat") || strings.Contains(text, "breaking changes")
	upgrade := strings.Contains(text, "atualiza") || strings.Contains(text, "upgrad") || strings.Contains(text, "updat")
	if !releaseNotes || !compatibility || !upgrade {
		t.Fatalf("proposed instructions lost part of the release review goal: %q", def.Prompt)
	}
	if after := catalog(); !reflect.DeepEqual(after, before) {
		t.Fatalf("proposal changed the catalog before confirmation: before=%+v after=%+v", before, after)
	}
	observed := e.calls()
	if len(observed) == 0 || len(observed) > 2 {
		t.Fatalf("expected a native creation with at most one correction, got %d calls", len(observed))
	}
	for _, invocation := range observed {
		if invocation.Name != "create_catalog_agent" {
			t.Fatalf("unexpected operation for catalog creation: %s", invocation.Name)
		}
	}
	plan, recognized, err := chat.NativeOperationPlan(observed[len(observed)-1])
	if err != nil || !recognized || plan.Kind != chat.CreateAgent || plan.Agent.ID != agentID || plan.Agent.Name != name || plan.Agent.Instructions != def.Prompt {
		t.Fatalf("final native operation did not propose the reviewed agent: %+v, %v", plan, err)
	}

	confirm := e.ask(t, session.ID, adminTok, "Confirme esta proposta.")
	assertReceipt := func(s chat.Session) {
		t.Helper()
		for _, message := range s.Messages {
			if r := message.Review; r != nil && r.ID == review.ID && r.AgentCreation != nil && r.AgentID == agentID && r.State == chat.ReviewCreated && r.ResultVersion == 1 {
				return
			}
		}
		t.Fatalf("missing creation receipt for the approved operation: %+v", s.Messages)
	}
	assertReceipt(confirm)
	endpoint := "/api/v1/chat/sessions/" + session.ID + "/operations/" + review.ID + "/confirm"
	var replay chat.Session
	if status := call(t, e.srv, "POST", endpoint, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &replay); status != http.StatusOK {
		t.Fatalf("replay of the same confirmation: %d", status)
	}
	assertReceipt(replay)

	after := catalog()
	if len(after) != len(before)+1 {
		t.Fatalf("expected one new catalog entry after replay: before=%+v after=%+v", before, after)
	}
	var saved agentcatalog.Agent
	if status := call(t, e.srv, "GET", catalogPath+"/"+agentID, viewerTok, "", "", &saved); status != http.StatusOK || saved.ID != agentID || len(saved.Versions) != 1 {
		t.Fatalf("expected one durable agent version after replay: status=%d catalog=%+v", status, saved)
	}
	version := saved.Versions[0]
	if version.Version != 1 || version.Operation != review.ID || !reflect.DeepEqual(version.Definition, def) || len(e.calls()) != len(observed) {
		t.Fatalf("version or prompt differs from the confirmed proposal: version=%+v calls=%d", version, len(e.calls()))
	}
}
