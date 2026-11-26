//go:build e2e

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
)

// These scripted model replies exercise the real API, worker and store.
// An unexpected prompt is a fixture error so a test cannot pass by echoing it.
func consoleFixtureResponse(request *llm.ChatRequest) (*llm.ChatResponse, error) {
	if len(request.Messages) == 0 {
		return nil, errors.New("missing Chat fixture input")
	}
	last := request.Messages[len(request.Messages)-1]
	if last.Role == llm.RoleTool && last.Name == "query_tasks" {
		var observation chat.TaskQueryResult
		if err := json.Unmarshal([]byte(last.Content), &observation); err != nil {
			return nil, err
		}
		if observation.Mode != "count" || observation.Status != "succeeded" {
			return nil, errors.New("unexpected task query in browser fixture")
		}
		return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf("Tarefas concluídas com sucesso: **%d**.", observation.Total)}}, nil
	}
	if last.Role != llm.RoleUser {
		return nil, errors.New("expected a Chat fixture user message")
	}
	if !request.JSON && last.Content != "Quantas tarefas foram finalizadas com sucesso?" {
		return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "plan-fixture", Name: "prepare_operation", Arguments: json.RawMessage(`{}`),
		}}}}, nil
	}
	var content string
	switch last.Content {
	case "Quantas tarefas foram finalizadas com sucesso?":
		return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "query-fixture", Name: "query_tasks", Arguments: json.RawMessage(`{"mode":"count","status":"succeeded","title":"Tarefas concluídas"}`),
		}}}}, nil
	case "Open the session detail fixture.":
		content = `{"kind":"manual.open","destination":"agents","title":"Review the registered workflows and retained evidence for the quarterly editorial release across multiple repositories"}`
	case "Draft a comparison of the registered chat project.":
		content = `{"kind":"task.create","taskTitle":"Compare registered project","workflow":"draft-project-fixture","request":"Compare the examples.","outcome":"understand","repositories":[{"project":"Draft repository fixture","alias":"examples"}]}`
	case "Start the repository checklist task.":
		content = `{"kind":"task.start","task":"Repository checklist fixture"}`
	case "Start the prepared release sandbox task.":
		content = `{"kind":"task.start","task":"Release sandbox fixture"}`
	case "Prepare the release sandbox task environment.":
		content = `{"kind":"task.prepare_environment","task":"Release sandbox fixture"}`
	case "Build the environment for the shared build workflow.":
		content = `{"kind":"workflow.prepare_environment","environmentWorkflow":"chat-shared-build","title":"Build shared environment"}`
	case "Correct the release checklist draft to review dates.":
		content = `{"kind":"task.edit","task":"Correctable release checklist","newRequest":"Review the release dates."}`
	case "Set the task budget to two dollars with an eighty percent warning.":
		content = `{"kind":"installation.plan","document":"apiVersion: weft.io/v1\nkind: CostPolicy\nmetadata: {name: installation}\nspec: {taskRunBudgetMicros: 2000000, warningPercent: 80}","title":"Review task budget"}`
	case "Tell replica zero of the reader in the guidance sample to summarize the warnings on its next turn.":
		content = `{"kind":"node.guide","guideWorkflow":"guidance-sample","guideNode":"reader","guideReplica":"0","guidance":"Summarize the warnings.","title":"Guide the reader"}`
	case "Set local instructions for reader in instruction-sample to review test changes.":
		content = `{"kind":"workflow.instructions","instructionWorkflow":"instruction-sample","instructionNode":"reader","localInstructions":"Review test changes.","title":"Review workflow instructions"}`
	case "Revise o PR 418.", "Revise o PR 418 em team/repo.":
		content = `{"kind":"pr.analyze","prNumber":"418","title":"Review a pull request"}`
	case "Open the connections list for OAuth setup.":
		content = `{"kind":"manual.open","destination":"connections","title":"Set up a connection"}`
	case "Create an editorial skill for checking release notes.":
		content = `{"kind":"skill.save","skillAction":"create","skillId":"editorial-notes","skillTitle":"Editorial notes","skillBody":"Check the release notes for dates.","title":"Prepare an editorial skill"}`
	case "Where can I inspect the model budget?":
		content = `{"kind":"manual.open","destination":"budgets","title":"Inspect model budget"}`
	case "Let me know when the release notes review is complete.":
		content = `{"kind":"task.watch","task":"Release notes for Chat fixture","title":"Follow release notes"}`
	case "Show me the current state of the release notes review.":
		content = `{"kind":"task.inspect","task":"Release notes for Chat fixture","title":"Inspect release notes"}`
	case "Follow the documentation check until it finishes.":
		content = `{"kind":"task.watch","task":"Documentation check for Chat fixture","title":"Follow documentation check"}`
	case "Create a reusable agent called Release reader to summarize release notes.":
		content = `{"kind":"agent.create","agentId":"release-reader","agentName":"Release reader","instructions":"Summarize release notes.","title":"Create release reader"}`
	case "Create a workflow named Editorial check using version one of the Editorial reader.":
		content = `{"kind":"workflow.create","workflowId":"editorial-check","workflowName":"Editorial check","agent":"editorial-reader","agentVersion":"1","title":"Prepare editorial workflow"}`
	case "Create a draft task on Editorial check to summarize the style notes.":
		content = `{"kind":"task.create","taskTitle":"Summarize style notes","workflow":"editorial-check","workflowVersion":"1","request":"Summarize the style notes.","outcome":"understand","title":"Prepare an editorial task"}`
	case "Start the saved archive review task.":
		content = `{"kind":"task.start","task":"Archive review task fixture","title":"Start archive review"}`
	case "Approve the pending input of the editorial gate task.":
		content = `{"kind":"approval.decide","task":"Editorial gate task fixture","approvalDecision":"approve","title":"Review editorial gate"}`
	case "Follow the cancellation test task.":
		content = `{"kind":"task.watch","task":"Cancellation fixture task","title":"Follow cancellation fixture"}`
	case "Cancel the running cancellation test task.":
		content = `{"kind":"task.cancel","task":"Cancellation fixture task"}`
	case "Set the editorial authorization task to workspace access.":
		content = `{"kind":"task.authorize","task":"Editorial authorization task fixture","taskAuthorizationMode":"workspace","title":"Review editorial task authorization"}`
	case "Raise the current allowance for the budget report task to 2.004005 USD.":
		content = `{"kind":"budget.raise","budgetScope":"run","budgetTarget":"Budget report task fixture","budgetAmount":"2.004005","budgetCurrency":"USD","title":"Review budget report allowance"}`
	case "Set three fixed replicas for reader in chat-scale-fixed.":
		content = `{"kind":"workflow.scale","scaleWorkflow":"chat-scale-fixed","scaleNode":"reader","scaleSize":"3","scaleMode":"fixed","title":"Review fixed replicas"}`
	case "Set the reader autoscale maximum to two in chat-scale-auto.":
		content = `{"kind":"workflow.scale","scaleWorkflow":"chat-scale-auto","scaleNode":"reader","scaleSize":"2","scaleMode":"autoscale","title":"Review autoscale ceiling"}`
	case "Pause future issue intake for Chat issue intake fixture.":
		content = `{"kind":"trigger.control","triggerTarget":"Chat issue intake fixture","triggerAction":"pause","title":"Review issue intake pause"}`
	case "Resume future issue intake for Chat issue intake fixture.":
		content = `{"kind":"trigger.control","triggerTarget":"Chat issue intake fixture","triggerAction":"resume"}`
	case "Start the publication workflow.":
		content = `{"kind":"workflow.control","controlWorkflow":"chat-publication","controlAction":"start","title":"Control publication workflow"}`
	case "Pause new turns for the reviewer node in the publication workflow.":
		content = `{"kind":"workflow.control","controlWorkflow":"chat-publication","controlNode":"reviewer","controlAction":"pause"}`
	case "Prepare a retry of my failed documentation task.":
		content = `{"kind":"task.retry","task":"Failed documentation task fixture","title":"Retry documentation task"}`
	case "Publish new shared instructions for Publishing reader to check revised notes.":
		content = `{"kind":"agent.publish","publishAgent":"publishing-reader","sharedInstructions":"Check the revised notes.","title":"Publish shared reader instructions"}`
	case "Adopt version two for the reader node in chat-pinned-reader.":
		content = `{"kind":"workflow.adopt_agent","adoptWorkflow":"chat-pinned-reader","adoptNode":"reader","adoptVersion":"2"}`
	default:
		id, ok := strings.CutPrefix(last.Content, "Cancel just the delegated execution ")
		if !ok {
			return nil, errors.New("unknown Chat fixture request")
		}
		content = fmt.Sprintf(`{"kind":"execution.cancel","executionId":%q,"executionScope":"child"}`, id)
	}
	return &llm.ChatResponse{
		Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: content},
		Usage:   llm.Usage{InputTokens: 24, OutputTokens: 18},
	}, nil
}
