package chat

import (
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestNativeOperationToolsCoverTheSupportedPlans(t *testing.T) {
	const task = `{"task":"Release notes"}`
	const commit = "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		name, arguments string
		plan            Plan
	}{
		{"find_resources", `{"inspectArea":"executions","resourceQuery":"review"}`, Plan{Kind: "resource.find", Area: "executions", Query: "review"}},
		{"watch_task", task, Plan{Kind: "task.watch", Task: "Release notes"}},
		{"cancel_execution", `{"executionId":"exe_details","executionScope":"child"}`, Plan{Kind: "execution.cancel", ExecutionID: "exe_details", ExecutionScope: "child"}},
		{"inspect_execution", `{"executionId":"exe_details"}`, Plan{Kind: "execution.inspect", ExecutionID: "exe_details"}},
		{"inspect_task", task, Plan{Kind: "task.inspect", Task: "Release notes"}},
		{"prepare_task_environment", task, Plan{Kind: "task.prepare_environment", Task: "Release notes"}},
		{"prepare_workflow_environment", `{"environmentWorkflow":"Review changes"}`, Plan{Kind: "workflow.prepare_environment", EnvironmentWorkflow: "Review changes"}},
		{"start_task", task, Plan{Kind: "task.start", Task: "Release notes"}},
		{"cancel_task", task, Plan{Kind: "task.cancel", Task: "Release notes"}},
		{"set_task_authorization", `{"task":"tsk_release","taskAuthorizationMode":"workspace"}`, Plan{Kind: "task.authorize", Task: "tsk_release", TaskAuthorizationMode: "workspace"}},
		{"decide_approval", `{"approvalId":"apr_123","approvalDecision":"approve","approvalAnswer":"Continue with the checklist"}`,
			Plan{Kind: "approval.decide", Approval: ApprovalDecisionDraft{Approval: "apr_123", Decision: "approve", Answer: "Continue with the checklist"}}},
		{"retry_task", task, Plan{Kind: "task.retry", Task: "Release notes"}},
		{"edit_task_draft", `{"task":"tsk_release","newTitle":"Release checklist","newOutcome":"review"}`,
			Plan{Kind: "task.edit", Task: "tsk_release", Correction: taskconfig.DraftText{Title: "Release checklist", Outcome: "review"}}},
		{"create_task_draft", `{"taskTitle":"Review docs","workflow":"doc-review","workflowVersion":"2","request":"Check the examples.","outcome":"review","repositories":[{"project":"documentation","alias":"docs","commit":"` + commit + `"}]}`,
			Plan{Kind: "task.create", TaskDraft: TaskDraft{Title: "Review docs", Workflow: "doc-review", WorkflowVersion: "2", Request: "Check the examples.", Outcome: "review",
				Repositories: []DraftRepositorySelector{{Project: "documentation", Alias: "docs", Commit: commit}}}}},
		{"create_catalog_agent", `{"agentId":"doc-reader","agentName":"Reader","instructions":"Read the examples."}`,
			Plan{Kind: "agent.create", Agent: AgentDraft{ID: "doc-reader", Name: "Reader", Instructions: "Read the examples."}}},
		{"publish_agent_version", `{"publishAgent":"doc-reader","sharedInstructions":"Check links."}`,
			Plan{Kind: "agent.publish", Publication: CatalogPublicationDraft{Agent: "doc-reader", Instructions: "Check links."}}},
		{"adopt_agent_version", `{"adoptWorkflow":"docs","adoptNode":"reader","adoptVersion":"3"}`,
			Plan{Kind: "workflow.adopt_agent", Adoption: AgentAdoptionDraft{Workflow: "docs", Node: "reader", Version: "3"}}},
		{"create_workflow", `{"workflowId":"doc-review","workflowName":"Documentation review","agent":"doc-reader","agentVersion":"1"}`,
			Plan{Kind: "workflow.create", Workflow: WorkflowDraft{ID: "doc-review", Name: "Documentation review", Agent: "doc-reader", AgentVersion: "1"}}},
		{"control_workflow", `{"controlNode":"reader","controlAction":"pause"}`, Plan{Kind: "workflow.control", Control: WorkflowControlDraft{Node: "reader", Action: "pause"}}},
		{"scale_workflow_node", `{"scaleWorkflow":"docs","scaleNode":"reader","scaleSize":"2","scaleMode":"fixed"}`,
			Plan{Kind: "workflow.scale", Scale: WorkflowScaleDraft{Workflow: "docs", Node: "reader", Size: "2", Mode: "fixed"}}},
		{"control_trigger", `{"triggerTarget":"release-issues","triggerAction":"resume"}`,
			Plan{Kind: "trigger.control", Trigger: TriggerControlDraft{Target: "release-issues", Action: "resume"}}},
		{"plan_installation", `{"document":"apiVersion: weft.io/v1\nkind: IssueTrigger","intakeConsent":"review"}`,
			Plan{Kind: "installation.plan", Document: "apiVersion: weft.io/v1\nkind: IssueTrigger", IntakeConsent: true}},
		{"edit_workflow_instructions", `{"instructionWorkflow":"docs","instructionNode":"reader","localInstructions":""}`,
			Plan{Kind: "workflow.instructions", Instructions: InstructionEdit{Graph: "docs", Node: "reader"}}},
		{"guide_workflow_node", `{"guideWorkflow":"docs","guideNode":"reader","guideReplica":"0","guidance":"Check broken links."}`,
			Plan{Kind: "node.guide", Guidance: Guidance{Graph: "docs", Node: "reader", Replica: "0", Text: "Check broken links."}}},
		{"analyze_pull_request", `{"prNumber":"418","prRepository":"team/docs"}`,
			Plan{Kind: "pr.analyze", PRSelector: PRSelector{Number: "418", Repository: "team/docs"}}},
		{"inspect_console", `{"inspectArea":"workflows"}`, Plan{Kind: "console.inspect", Area: "workflows"}},
		{"raise_current_budget", `{"budgetScope":"run","budgetTarget":"tsk_release","budgetAmount":"2.25","budgetCurrency":"USD"}`,
			Plan{Kind: "budget.raise", Budget: BudgetRaiseDraft{Scope: "run", Target: "tsk_release", Amount: "2.25", Currency: "USD"}}},
		{"manage_skill", `{"skillId":"release-style","skillAction":"enable"}`, Plan{Kind: "skill.save", Skill: SkillDraft{Action: "enable", ID: "release-style"}}},
		{"open_console_page", `{"destination":"chat-model","title":"  Model selection  "}`,
			Plan{Kind: "manual.open", Destination: "chat-model", Title: "Model selection"}},
		{"ask_clarifying_question", `{"question":"Qual workflow você quer usar?"}`, Plan{Kind: "clarify", Question: "Qual workflow você quer usar?"}},
		{"unsupported_request", `{}`, Plan{Kind: "unsupported"}},
	}

	schemas := OperationToolSchemas()
	if len(schemas) != len(tests) {
		t.Fatalf("declared tools=%d covered tools=%d", len(schemas), len(tests))
	}
	declared := make(map[string]llm.ToolSchema, len(schemas))
	providerToolName := regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	for _, schema := range schemas {
		if _, duplicate := declared[schema.Name]; duplicate || !providerToolName.MatchString(schema.Name) || schema.Description == "" {
			t.Fatalf("invalid or repeated tool declaration: %q", schema.Name)
		}
		declared[schema.Name] = schema
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			schema, ok := declared[tc.name]
			if !ok {
				t.Fatal("tool has no provider declaration")
			}
			var shape struct {
				Type                 string
				AdditionalProperties *bool
				Properties           map[string]json.RawMessage
				Required             []string
			}
			if err := json.Unmarshal(schema.Parameters, &shape); err != nil || shape.Type != "object" || shape.AdditionalProperties == nil || *shape.AdditionalProperties || shape.Properties["kind"] != nil || shape.Properties["title"] == nil {
				t.Fatalf("tool parameters must be a closed object with optional title: %s (%v)", schema.Parameters, err)
			}
			var args map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.arguments), &args); err != nil {
				t.Fatal(err)
			}
			for key := range args {
				if shape.Properties[key] == nil {
					t.Errorf("accepted parameter %s is missing from the schema", key)
				}
			}
			for _, key := range shape.Required {
				if args[key] == nil {
					t.Errorf("valid fixture omitted required parameter %s", key)
				}
			}
			plan, recognized, err := NativeOperationPlan(llm.ToolCall{Name: tc.name, Arguments: json.RawMessage(tc.arguments)})
			if !recognized || err != nil || !reflect.DeepEqual(plan, tc.plan) {
				t.Fatalf("decoded plan=%+v, want %+v (recognized=%t, err=%v)", plan, tc.plan, recognized, err)
			}
		})
	}
}

func TestNativeOperationToolsRejectUnreviewedAndInvalidArguments(t *testing.T) {
	const draft = `{"taskTitle":"Docs","workflow":"doc-review","request":"Check examples.","outcome":"review","repositories":`
	const repo = `{"project":"docs","alias":"docs"}`
	for _, tc := range []struct{ name, args string }{
		{"open_console_page", `{"destination":"https://example.invalid"}`},
		{"open_console_page", `{"destination":"tasks","destination":"approvals"}`},
		{"open_console_page", `{"destination":"tasks","\u0064estination":"approvals"}`},
		{"open_console_page", `{"destination":"tasks","kind":"task.start"}`},
		{"open_console_page", `{"destination":"tasks"} {}`},
		{"open_console_page", `{"destination":null}`},
		{"open_console_page", `{"destination":1}`},
		{"open_console_page", `{"Destination":"tasks"}`},
		{"open_console_page", `null`},
		{"open_console_page", `[]`},
		{"open_console_page", `{"destination":"tasks","title":"` + strings.Repeat("x", MaxRequestBytes) + `"}`},
		{"find_resources", `{"inspectArea":"network"}`},
		{"inspect_console", `{"inspectArea":"executions"}`},
		{"decide_approval", `{"approvalId":"apr_123","task":"tsk_release","approvalDecision":"approve"}`},
		{"decide_approval", `{"approvalId":"apr_123","approvalDecision":"reject","approvalAnswer":"Continue"}`},
		{"plan_installation", `{"document":"Configuration","intakeConsent":"apply"}`},
		{"plan_installation", `{"document":"Configuration","force":true}`},
		{"create_catalog_agent", `{"agentId":"doc-reader","agentName":"Reader","instructions":"Review docs","grants":["*"]}`},
		{"start_task", `{"task":"tsk_release","role":"admin","confirmed":true}`},
		{"start_task", `{"task":"Authorization: Bearer fixture-private-value"}`},
		{"cancel_execution", `{"executionId":"exe_test","executionScope":"everything"}`},
		{"cancel_execution", `{"executionId":"exe_test","task":"tsk_release","executionScope":"tree"}`},
		{"scale_workflow_node", `{"scaleWorkflow":"docs","scaleNode":"reader","scaleSize":"999","scaleMode":"fixed"}`},
		{"raise_current_budget", `{"budgetScope":"run","budgetAmount":"2.50","budgetCurrency":"USD"}`},
		{"manage_skill", `{"skillId":"release-style","skillAction":"enable","skillBody":"Change audience"}`},
		{"edit_workflow_instructions", `{"instructionWorkflow":"docs","instructionNode":"reader"}`},
		{"analyze_pull_request", `{"prNumber":"0"}`},
		{"analyze_pull_request", `{"prNumber":"418","prConnectionRevision":"forged"}`},
		{"create_task_draft", draft + `[{"project":"docs","project":"internal","alias":"docs"}]}`},
		{"create_task_draft", draft + `[{"project":"docs","alias":"docs","credentials":"synthetic"}]}`},
		{"create_task_draft", draft + `[{"project":"docs","alias":"docs","commit":null}]}`},
		{"create_task_draft", draft + `[{"project":"docs"}]}`},
		{"create_task_draft", draft + `[]}`},
		{"create_task_draft", draft + `[` + strings.Repeat(repo+",", 8) + repo + `]}`},
		{"ask_clarifying_question", `{"question":""}`},
		{"unsupported_request", `{"question":"Use the other operation."}`},
	} {
		plan, recognized, err := NativeOperationPlan(llm.ToolCall{Name: tc.name, Arguments: json.RawMessage(tc.args)})
		if !recognized || !errors.Is(err, ErrInvalidPlan) || plan.Kind != "" {
			t.Errorf("invalid %s arguments were not refused: %s (plan=%s, recognized=%t, error=%v)", tc.name, tc.args, plan.Kind, recognized, err)
		}
	}

	// Other read tools and unknown names are separate from operation proposals.
	for _, name := range []string{"query_tasks", "prepare_operation", "filesystem_write", "approve_review"} {
		plan, recognized, err := NativeOperationPlan(llm.ToolCall{Name: name, Arguments: json.RawMessage(`{}`)})
		if recognized || err != nil || plan.Kind != "" {
			t.Errorf("unregistered operation %q was recognized", name)
		}
	}
	plan, recognized, err := NativeOperationPlan(llm.ToolCall{Name: "unsupported_request", Arguments: json.RawMessage(`{"title":"https://example.invalid/private"}`)})
	if !recognized || err != nil || plan.Title != "" {
		t.Fatalf("title redaction diverged from decodePlan: %+v %t %v", plan, recognized, err)
	}
}
