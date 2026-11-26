//go:build e2e

package main

import (
	"errors"

	"github.com/woliveiras/weft/server/internal/llm"
)

// These scripted model replies exercise the real API, worker and store.
// An unexpected prompt is a fixture error so a test cannot pass by echoing it.
func consoleFixtureResponse(request *llm.ChatRequest) (*llm.ChatResponse, error) {
	if len(request.Messages) == 0 {
		return nil, errors.New("missing Chat fixture input")
	}
	last := request.Messages[len(request.Messages)-1]
	if last.Role != llm.RoleUser {
		return nil, errors.New("expected a Chat fixture user message")
	}
	var content string
	switch last.Content {
	case "Set the task budget to two dollars with an eighty percent warning.":
		content = `{"kind":"installation.plan","document":"apiVersion: weft.io/v1\nkind: CostPolicy\nmetadata: {name: installation}\nspec: {taskRunBudgetMicros: 2000000, warningPercent: 80}","title":"Review task budget"}`
	case "Tell replica zero of the reader in the guidance sample to summarize the warnings on its next turn.":
		content = `{"kind":"node.guide","guideWorkflow":"guidance-sample","guideNode":"reader","guideReplica":"0","guidance":"Summarize the warnings.","title":"Guide the reader"}`
	case "Set local instructions for reader in instruction-sample to review test changes.":
		content = `{"kind":"workflow.instructions","instructionWorkflow":"instruction-sample","instructionNode":"reader","localInstructions":"Review test changes.","title":"Review workflow instructions"}`
	case "Revise o PR 418.":
		content = `{"kind":"pr.analyze","prNumber":"418","prWorkflow":"manual-review","prConnection":"pr-fixture","title":"Review a pull request"}`
	case "Open the connections list for OAuth setup.":
		content = `{"kind":"manual.open","destination":"connections","title":"Set up a connection"}`
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
		return nil, errors.New("unknown Chat fixture request")
	}
	return &llm.ChatResponse{
		Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: content},
		Usage:   llm.Usage{InputTokens: 24, OutputTokens: 18},
	}, nil
}
