package chat

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/llm"
)

const clarifyOperationTool = "ask_clarifying_question"

type operationTool struct {
	name, kind, description string
	arguments               []operationArgument
}

type operationArgument struct {
	name, description string
	required          bool
	values            []string
	items             []operationArgument
	maxItems          int
}

func optionalArgument(name, description string, values ...string) operationArgument {
	return operationArgument{name: name, description: description, values: values}
}

func requiredArgument(name, description string, values ...string) operationArgument {
	a := optionalArgument(name, description, values...)
	a.required = true
	return a
}

func nativeOperation(name, kind, description string, arguments ...operationArgument) operationTool {
	return operationTool{name, kind, description, append(arguments, optionalArgument("title", "Short, neutral conversation title in the user's language. Omit credentials, URLs, account names and sensitive identifiers."))}
}

var taskSelectorArgument = requiredArgument("task", "The task ID or name from the user's request or a verified Weft observation. Keep the supplied selector literal.")
var outcomes = []string{"understand", "implement", "investigate", "review", "freeform"}
var repositoryArguments = operationArgument{
	name: "repositories", description: "Requested registered repositories. Omit this array unless the user named projects.", maxItems: 8,
	items: []operationArgument{
		requiredArgument("project", "The stated registered project ID or name."),
		requiredArgument("alias", "Checkout alias, unique within the draft."),
		optionalArgument("commit", "Full commit SHA only when explicitly supplied."),
	},
}

// These tools produce plans only. The API resolves resources, checks the
// authenticated caller's permissions and prepares any required review.
var operationTools = []operationTool{
	nativeOperation("find_resources", FindResource, "Find named Weft resources and offer matches when needed. For a partial name attached to a specific operation, use that operation with the literal selector instead.",
		requiredArgument("inspectArea", "Resource type.", "tasks", "workflows", "nodes", "agents", "projects", "executions", "skills", "approvals", "triggers", "review-routes", "mcp", "scm", "issue-connections", "models", "plugins", "tools", "budgets"),
		optionalArgument("resourceQuery", "The user's search words. Omit to find candidates in the selected area.")),
	nativeOperation("watch_task", WatchTask, "Follow one existing task and report its outcome when it settles. Watching never starts or modifies the task.", taskSelectorArgument),
	nativeOperation("cancel_execution", CancelExecution, "Prepare a review for cooperative cancellation of an exact execution. Ask if the user has not distinguished the whole execution tree from a single child. This does not control the task or workflow.",
		requiredArgument("executionId", "Exact execution ID."), requiredArgument("executionScope", "Cancellation scope explicitly requested by the user.", "tree", "child")),
	nativeOperation("inspect_execution", InspectExecution, "Read the current state and tree of one execution. Ask for the ID if missing.", requiredArgument("executionId", "Exact execution ID.")),
	nativeOperation("inspect_task", InspectTask, "Read one task or return the link to its details. For a request for the link, URL or detail page of one task, set taskView to link and use its exact ID. Use the ID from the user's request or the immediately preceding verified task when the user refers to it. Ask for its ID if missing. Use query_tasks for counts, existence or filtered lists across tasks.",
		taskSelectorArgument, optionalArgument("taskView", "Set to link when the user wants the specific task's detail link. Omit when asking for state or results.", TaskLinkView)),
	nativeOperation("prepare_task_environment", PrepareTaskEnvironment, "Prepare a review for building a named task's saved environment. Starting its agents is a separate operation.", taskSelectorArgument),
	nativeOperation("prepare_workflow_environment", PrepareWorkflowEnvironment, "Prepare a review for building the current saved environment of a reusable workflow. Use for its normal build only. Reset, force, definition changes and starting agents are separate operations.",
		requiredArgument("environmentWorkflow", "The workflow ID or name stated by the user.")),
	nativeOperation("start_task", StartTask, "Prepare a review for starting an identified saved task. Use only for an explicit start request. A draft, inspection or watch does not authorize a start.", taskSelectorArgument),
	nativeOperation("cancel_task", CancelTask, "Prepare a review for interrupting the identified running task attempt. Workflow control and trigger intake have separate effects.", taskSelectorArgument),
	nativeOperation("set_task_authorization", AuthorizeTask, "Prepare a review for the user's explicit authorization choice on one task. A running task may only narrow its existing authority. Ask for a missing mode. Starting does not imply consent, and this does not edit project preferences or workflow grants.",
		taskSelectorArgument, requiredArgument("taskAuthorizationMode", "Mode explicitly chosen by the user.", "ask", "workspace", "full")),
	nativeOperation("decide_approval", DecideApproval, "Prepare a review for an explicit decision on one pending request. Supply either the approval ID or an identified task, never both. Never invent an answer or infer approval from opening, starting, or following a task.",
		optionalArgument("approvalId", "Exact pending approval ID."), optionalArgument("task", "Task ID or name when the request is identified by its task instead of the approval ID."),
		requiredArgument("approvalDecision", "Decision stated by the user.", "approve", "reject"), optionalArgument("approvalAnswer", "The user's exact answer to a pending question, if applicable."),
		optionalArgument("approvalComment", "The user's comment, if supplied.")),
	nativeOperation("retry_task", RetryTask, "Prepare a review for saving a linked retry draft of a failed task. This creates no execution and does not carry over authorization from the original attempt.", taskSelectorArgument),
	nativeOperation("edit_task_draft", CorrectTask, "Review one draft edit: text, complete channel payload, or one checkout replacement. For a checkout, ask for the existing alias and the new registered project. Keep all sibling bindings.",
		taskSelectorArgument, optionalArgument("newTitle", "Replacement task title."), optionalArgument("newRequest", "Replacement task request."), optionalArgument("newOutcome", "Replacement outcome.", outcomes...),
		optionalArgument("checkoutAlias", "Existing checkout alias to replace."), optionalArgument("replacementProject", "Explicit new registered project ID or unique name."),
		optionalArgument("newPayloadJson", "Complete channel input payload as a JSON string. Use for an explicitly requested replacement, never a patch or a credential.")),
	nativeOperation("create_task_draft", CreateTask, "Prepare a review for saving a task draft on a named workflow. Ask when the workflow or requested projects are unclear. This does not start a task, grant access or set up remote delivery.",
		requiredArgument("taskTitle", "Title for the draft."), requiredArgument("workflow", "The stated workflow ID or name."),
		requiredArgument("request", "The intended work."), requiredArgument("outcome", "Requested result of the task.", outcomes...),
		optionalArgument("workflowVersion", "Positive decimal version as a string, only if the user named it."), repositoryArguments),
	nativeOperation("create_catalog_agent", CreateAgent, "Prepare a review for a new reusable catalog agent. Use a confirmation to decide the existing proposal, never to create another proposal.",
		requiredArgument("agentId", "New agent ID using lowercase letters, digits and hyphens."), requiredArgument("agentName", "Display name for the new agent."),
		requiredArgument("instructions", "Concise agent instructions supplied by the request.")),
	nativeOperation("publish_agent_version", PublishAgent, "Prepare a review for publishing new shared catalog instructions. Existing workflow pins do not change. Ask which scope is intended if shared publication, workflow-local instructions or one-time guidance are ambiguous.",
		requiredArgument("publishAgent", "The stated catalog agent ID or name."), requiredArgument("sharedInstructions", "Complete replacement text for the shared instructions.")),
	nativeOperation("adopt_agent_version", AdoptAgentVersion, "Prepare a separate review to adopt an explicit catalog agent version on one workflow node. A catalog publication does not imply adoption. Ask if any target or version is missing.",
		requiredArgument("adoptWorkflow", "The stated workflow ID or name."), requiredArgument("adoptNode", "The stated node ID or name."),
		requiredArgument("adoptVersion", "Requested version as a decimal string, or latest only if explicitly requested.")),
	nativeOperation("create_workflow", CreateWorkflow, "Prepare a review for a stopped task workflow using one existing catalog agent. Ask which agent if ambiguous. This creates no agent or running workflow.",
		requiredArgument("workflowId", "New workflow ID using lowercase letters, digits and hyphens."), requiredArgument("workflowName", "Display name of the workflow."),
		requiredArgument("agent", "The stated existing catalog agent ID or name."), optionalArgument("agentVersion", "Named version as a decimal string, or latest only if explicitly requested.")),
	nativeOperation("control_workflow", ControlWorkflow, "Prepare a review for a workflow or named node runtime change. Pause stops new turns, stop drains and kill interrupts active work. Kill must be explicitly intended. Clarify ambiguous requests to disable a reviewer, including whether the target is future trigger intake, new turns, drain or interruption. Task runs and triggers have separate controls.",
		optionalArgument("controlWorkflow", "The stated workflow ID or name. Omit if the user identified only the node."),
		optionalArgument("controlNode", "The stated node ID or name for a node-level request."), requiredArgument("controlAction", "Requested runtime change.", "start", "pause", "resume", "stop", "kill")),
	nativeOperation("scale_workflow_node", ScaleWorkflowNode, "Prepare a review of one node's replica count or explicit autoscale ceiling. Fixed zero gracefully stops the node and must be intended. Ask if the target, absolute number or mode is unclear. This does not change the cluster mode or start the workflow.",
		requiredArgument("scaleWorkflow", "The stated workflow ID or name."), requiredArgument("scaleNode", "The stated node ID or name."),
		requiredArgument("scaleSize", "Absolute nonnegative integer as a decimal string."), requiredArgument("scaleMode", "Fixed replica count or autoscale ceiling.", "fixed", "autoscale")),
	nativeOperation("control_trigger", ControlTrigger, "Prepare a review to pause or resume future issue intake for one trigger. Already admitted work continues. Ask if a workflow, its agents or PR route could be the intended target.",
		requiredArgument("triggerTarget", "The stated issue trigger ID or name."), requiredArgument("triggerAction", "Change to future issue intake.", "pause", "resume")),
	nativeOperation("plan_installation", PlanInstallation, "Prepare a declarative installation plan for administrator review. Ask about missing settings. Use native credential setup for secrets. Never set force, claim consent, or claim a change was applied. For an existing project's defaults or checks, use its exact ID and include only the specified executionMode and/or a complete replacement check list with exact commands.",
		requiredArgument("document", "One or more weft.io/v1 YAML documents. Supported kinds: InferencePlugin, ModelProvider, PriceCatalog, CostPolicy, Budget, NetworkPolicy, MCPConnection, SCMConnection, IssueConnection, IssueWorkflowMapping, JiraCloudProfile, JiraCloudWorkflowMapping, Project, Graph, PullRequestReviewRoute and IssueTrigger."),
		optionalArgument("intakeConsent", "Use review for an explicit issue or PR intake activation request so the administrator sees separate consent scope.", "review")),
	nativeOperation("edit_workflow_instructions", EditInstructions, "Prepare a review of local instructions for one node in a saved workflow. Ask whether workflow-local, shared catalog or one-time guidance is intended if the scope is ambiguous.",
		requiredArgument("instructionWorkflow", "Exact workflow ID."), requiredArgument("instructionNode", "Exact node ID."),
		requiredArgument("localInstructions", "Complete replacement text. Use an empty string only for an explicit request to clear local instructions.")),
	nativeOperation("guide_workflow_node", GuideNode, "Prepare a review of one operator message for a later matching turn of a running workflow node. Ask for any missing target, or which scope is intended if it might be a durable instruction change.",
		requiredArgument("guideWorkflow", "Exact workflow ID."), requiredArgument("guideNode", "Exact node ID."),
		requiredArgument("guideReplica", "Replica number as a string, or any only if explicitly selected."), requiredArgument("guidance", "The requested one-time operator message.")),
	nativeOperation("analyze_pull_request", AnalyzePR, "Prepare a review for analyzing an existing PR. This does not publish a result. For a bare PR number omit all scope selectors and allow discovery. Reuse a selected connection only when the user refers to it. Never infer a repository or write permission.",
		optionalArgument("prNumber", "Positive PR number as a decimal string. Supply the number or a PR URL."), optionalArgument("prUrl", "Full PR URL supplied by the user."),
		optionalArgument("prRepository", "Explicitly named owner/repository."), optionalArgument("prWorkflow", "Explicitly named reviewer workflow."),
		optionalArgument("prNode", "Explicitly named reviewer node."), optionalArgument("prConnection", "Explicitly named connection, or a verified selection the user referred to.")),
	nativeOperation("inspect_console", InspectConsole, "Read current console facts in the conversation. Use query_tasks for task collections, counts and existence. Use query_spend for monthly costs, model calls, token usage or breakdowns. Use open_console_page only when the user asks for navigation or a protected manual control.",
		requiredArgument("inspectArea", "Console area to inspect.", "tasks", "workflows", "agents", "projects", "skills", "approvals", "activity", "triggers", "review-routes", "mcp", "scm", "issue-connections", "models", "chat-model", "plugins", "tools", "network", "retention", "delegation", "usage", "prices", "budgets")),
	nativeOperation("raise_current_budget", RaiseBudget, "Prepare a review for raising one existing allowance. This may resume held work under the current budget policy. Ask if scope, amount or currency is unclear. Use plan_installation for recurring or default limits.",
		requiredArgument("budgetScope", "Scope of the existing ledger.", "run", "installation", "user", "project", "workflow"),
		optionalArgument("budgetTarget", "Task ID or name for a run, budget ID for user/project/workflow, omitted for installation."),
		requiredArgument("budgetAmount", "Absolute new decimal limit as a string, never a relative increment."), requiredArgument("budgetCurrency", "ISO currency code stated by the user.")),
	nativeOperation("manage_skill", SaveSkill, "Prepare a review to manage an identified instruction skill. New skills are disabled. Activation and default scope are separate. Only delete if explicitly requested.",
		requiredArgument("skillId", "Skill ID."), requiredArgument("skillAction", "Requested change.", "create", "revise", "enable", "disable", "make-default", "remove-default", "delete"),
		optionalArgument("skillTitle", "Display title for create or revise. Omit for all other changes."), optionalArgument("skillBody", "Complete body for create or revise. Omit for all other changes.")),
	nativeOperation("open_console_page", OpenManual, "Open a named console page for navigation or an operation that requires manual controls. Use protected setup for credentials and never request them in conversation. Opening a page does not perform the operation. Budgets shows limits, usage shows spending, and prices shows per-model token rates.",
		requiredArgument("destination", "Named page, never a URL. Use approvals for protected previews, tasks for remote delivery, project-import for repository import and resources for cleanup.",
			"tasks", "workflows", "agents", "projects", "activity", "approvals", "models", "chat-model", "plugins", "usage", "budgets", "prices", "connections", "connection-setup", "skills", "administration", "configuration-export", "task-editor", "project-import", "source-control-setup", "delegation", "network", "retention", "resources", "tools")),
	nativeOperation(clarifyOperationTool, Clarify, "Ask one focused question in the user's language if the requested target, scope, permission, cost or action is unclear. Do not guess.",
		requiredArgument("question", "The focused question for the operator.")),
	nativeOperation("unsupported_request", Unsupported, "Use when the requested outcome is outside the supported Weft operations."),
}

// OperationToolSchemas declares the operations the model can propose. Listing
// a tool grants no authority to execute it.
func OperationToolSchemas() []llm.ToolSchema {
	schemas := make([]llm.ToolSchema, 0, len(operationTools))
	for _, operation := range operationTools {
		parameters, _ := json.Marshal(operationObjectSchema(operation.arguments))
		schemas = append(schemas, llm.ToolSchema{Name: operation.name, Description: operation.description, Parameters: parameters})
	}
	return schemas
}

func operationObjectSchema(arguments []operationArgument) map[string]any {
	properties := make(map[string]any, len(arguments))
	var required []string
	for _, arg := range arguments {
		schema := map[string]any{"type": "string", "description": arg.description}
		if len(arg.values) > 0 {
			schema["enum"] = arg.values
		}
		if arg.items != nil {
			schema["type"] = "array"
			schema["items"] = operationObjectSchema(arg.items)
			schema["minItems"], schema["maxItems"] = 1, arg.maxItems
		}
		properties[arg.name] = schema
		if arg.required {
			required = append(required, arg.name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// NativeOperationPlan recognizes only the closed operation catalog. A
// recognized malformed call returns ErrInvalidPlan, leaving the turn runner
// free to report or correct its arguments without resolving or executing it.
func NativeOperationPlan(call llm.ToolCall) (plan Plan, recognized bool, err error) {
	index := slices.IndexFunc(operationTools, func(op operationTool) bool { return op.name == call.Name })
	if index < 0 {
		return Plan{}, false, nil
	}
	if len(call.Arguments) > MaxRequestBytes || !utf8.Valid(call.Arguments) {
		return Plan{}, true, ErrInvalidPlan
	}
	operation := operationTools[index]
	arguments, err := decodeOperationArguments(call.Arguments, operation.arguments)
	if err != nil {
		return Plan{}, true, err
	}
	arguments["kind"], err = json.Marshal(operation.kind)
	if err != nil {
		return Plan{}, true, err
	}
	body, err := json.Marshal(arguments)
	if err != nil {
		return Plan{}, true, err
	}
	plan, err = decodePlan(string(body))
	return plan, true, err
}

func decodeOperationArguments(raw json.RawMessage, fields []operationArgument) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, ErrInvalidPlan
	}
	arguments := make(map[string]json.RawMessage, len(fields))
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || arguments[key] != nil {
			return nil, ErrInvalidPlan
		}
		field := slices.IndexFunc(fields, func(a operationArgument) bool { return a.name == key })
		var value json.RawMessage
		if field < 0 || decoder.Decode(&value) != nil || !validOperationArgument(value, fields[field]) {
			return nil, ErrInvalidPlan
		}
		arguments[key] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalidPlan
	}
	for _, field := range fields {
		if field.required && arguments[field.name] == nil {
			return nil, ErrInvalidPlan
		}
	}
	return arguments, nil
}

func validOperationArgument(raw json.RawMessage, argument operationArgument) bool {
	if argument.items == nil {
		var value *string
		return json.Unmarshal(raw, &value) == nil && value != nil && (len(argument.values) == 0 || slices.Contains(argument.values, *value))
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || len(items) == 0 || len(items) > argument.maxItems {
		return false
	}
	for _, item := range items {
		if _, err := decodeOperationArguments(item, argument.items); err != nil {
			return false
		}
	}
	return true
}
