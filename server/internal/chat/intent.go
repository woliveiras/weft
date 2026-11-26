// Package chat implements the authenticated operator's conversational console.
package chat

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

const (
	MaxRequestBytes = 4096
	maxContextBytes = 12 << 10
	maxContextTurns = 8
	maxAnswerTokens = 2048

	OpenManual     = "manual.open"
	WatchTask      = "task.watch"
	InspectTask    = "task.inspect"
	CreateTask     = "task.create"
	StartTask      = "task.start"
	CreateAgent    = "agent.create"
	CreateWorkflow = "workflow.create"
	Clarify        = "clarify"
	Unsupported    = "unsupported"
)

var (
	ErrInvalidMessage   = errors.New("enter a message within the length limit without credentials")
	ErrInvalidPlan      = errors.New("the model did not return a supported operation")
	ErrModelUnavailable = errors.New("the chat model is unavailable")
	credentialPattern   = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+|bearer\s+[a-z0-9._=-]{12,}|(?:api[_ -]?key|access[_ -]?token|refresh[_ -]?token|password|client[_ -]?secret|credential|token)["']?\s*[:=]\s*\S+|-----BEGIN [A-Z ]*PRIVATE KEY-----|https?://[^\s/@]+:[^\s/@]+@|\b(?:sk-[a-z0-9_-]{16,}|gh[pousr]_[a-z0-9_]{16,}|AKIA[A-Z0-9]{16})\b)`)
)

// Plan proposes an operation or asks for clarification. A task selector is
// always resolved against the caller's permitted resources before it is used.
type Plan struct {
	Query                 string
	EnvironmentWorkflow   string
	Trigger               TriggerControlDraft
	Scale                 WorkflowScaleDraft
	ExecutionScope        string
	ExecutionID           string
	Budget                BudgetRaiseDraft
	Correction            taskconfig.DraftText
	Skill                 SkillDraft
	Area                  string
	PRSelector            PRSelector
	Kind                  string `json:"kind"`
	Task                  string `json:"task,omitempty"`
	Question              string `json:"question,omitempty"`
	Title                 string `json:"title,omitempty"`
	Document              string
	IntakeConsent         bool
	Destination           string `json:"destination,omitempty"`
	Agent                 AgentDraft
	Publication           CatalogPublicationDraft
	Adoption              AgentAdoptionDraft
	Workflow              WorkflowDraft
	Instructions          InstructionEdit
	Guidance              Guidance
	TaskDraft             TaskDraft
	TaskAuthorizationMode taskauth.Mode
	Approval              ApprovalDecisionDraft
	Control               WorkflowControlDraft
}

func safeText(text string, limit int) bool {
	return len(text) <= limit && utf8.ValidString(text) &&
		strings.IndexFunc(text, func(r rune) bool {
			return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t'
		}) < 0 && !credentialPattern.MatchString(text)
}

func ValidateMessage(text string) error {
	if strings.TrimSpace(text) == "" || !safeText(text, MaxRequestBytes) {
		return ErrInvalidMessage
	}
	return nil
}

func boundedHistory(turns []llm.ChatMessage) ([]llm.ChatMessage, error) {
	if len(turns) == 0 || turns[len(turns)-1].Role != llm.RoleUser {
		return nil, ErrInvalidMessage
	}
	start := len(turns)
	bytes := 0
	for start > 0 && len(turns)-start < maxContextTurns {
		message := turns[start-1]
		if (message.Role != llm.RoleUser && message.Role != llm.RoleAssistant) ||
			message.Name != "" || message.ToolCallID != "" || len(message.ToolCalls) != 0 ||
			ValidateMessage(message.Content) != nil {
			return nil, ErrInvalidMessage
		}
		if bytes+len(message.Content) > maxContextBytes {
			break
		}
		bytes += len(message.Content)
		start--
	}
	return turns[start:], nil
}

func decodePlan(body string) (Plan, error) {
	decoder := json.NewDecoder(strings.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return Plan{}, ErrInvalidPlan
	}
	var plan Plan
	seen := make(map[string]bool, 4)
	for decoder.More() {
		field, err := decoder.Token()
		key, ok := field.(string)
		if err != nil || !ok || seen[key] {
			return Plan{}, ErrInvalidPlan
		}
		seen[key] = true
		if key == "repositories" {
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&plan.TaskDraft.Repositories); err != nil || len(plan.TaskDraft.Repositories) == 0 {
				return Plan{}, ErrInvalidPlan
			}
			continue
		}
		var input *string
		if err := decoder.Decode(&input); err != nil || input == nil {
			return Plan{}, ErrInvalidPlan
		}
		value := strings.TrimSpace(*input)
		switch key {
		case "environmentWorkflow":
			plan.EnvironmentWorkflow = value
		case "triggerTarget":
			plan.Trigger.Target = value
		case "triggerAction":
			plan.Trigger.Action = value
		case "scaleWorkflow":
			plan.Scale.Workflow = value
		case "scaleNode":
			plan.Scale.Node = value
		case "scaleSize":
			plan.Scale.Size = value
		case "scaleMode":
			plan.Scale.Mode = value
		case "executionScope":
			plan.ExecutionScope = value
		case "executionId":
			plan.ExecutionID = value
		case "budgetScope":
			plan.Budget.Scope = value
		case "budgetTarget":
			plan.Budget.Target = value
		case "budgetAmount":
			plan.Budget.Amount = value
		case "budgetCurrency":
			plan.Budget.Currency = value
		case "prNumber":
			plan.PRSelector.Number = value
		case "prRepository":
			plan.PRSelector.Repository = value
		case "prUrl":
			plan.PRSelector.URL = value
		case "prNode":
			plan.PRSelector.Node = value
		case "prWorkflow":
			plan.PRSelector.Workflow = value
		case "prConnection":
			plan.PRSelector.Connection = value
		case "resourceQuery":
			plan.Query = value
		case "inspectArea":
			plan.Area = value
		case "skillAction":
			plan.Skill.Action = value
		case "skillId":
			plan.Skill.ID = value
		case "skillTitle":
			plan.Skill.Title = value
		case "skillBody":
			plan.Skill.Body = value
		case "instructionWorkflow":
			plan.Instructions.Graph = value
		case "instructionNode":
			plan.Instructions.Node = value
		case "localInstructions":
			plan.Instructions.After = value
		case "guideWorkflow":
			plan.Guidance.Graph = value
		case "guideNode":
			plan.Guidance.Node = value
		case "guideReplica":
			plan.Guidance.Replica = value
		case "guidance":
			plan.Guidance.Text = value
		case "document":
			plan.Document = value
		case "intakeConsent":
			if value != "review" {
				return Plan{}, ErrInvalidPlan
			}
			plan.IntakeConsent = true
		case "destination":
			plan.Destination = value
		case "controlWorkflow":
			plan.Control.Workflow = value
		case "controlNode":
			plan.Control.Node = value
		case "controlAction":
			plan.Control.Action = value
		case "kind":
			plan.Kind = value
		case "task":
			plan.Task = value
		case "taskAuthorizationMode":
			plan.TaskAuthorizationMode = taskauth.Mode(value)
		case "approvalId":
			plan.Approval.Approval = value
		case "approvalDecision":
			plan.Approval.Decision = value
		case "approvalAnswer":
			plan.Approval.Answer = value
		case "approvalComment":
			plan.Approval.Comment = value
		case "question":
			plan.Question = value
		case "newTitle":
			plan.Correction.Title = value
		case "newRequest":
			plan.Correction.Request = value
		case "newOutcome":
			plan.Correction.Outcome = value
		case "taskTitle":
			plan.TaskDraft.Title = value
		case "workflow":
			plan.TaskDraft.Workflow = value
		case "workflowVersion":
			plan.TaskDraft.WorkflowVersion = value
		case "request":
			plan.TaskDraft.Request = value
		case "outcome":
			plan.TaskDraft.Outcome = value
		case "title":
			plan.Title = cleanTitle(value)
		case "agentId":
			plan.Agent.ID = value
		case "agentName":
			plan.Agent.Name = value
		case "instructions":
			plan.Agent.Instructions = value
		case "workflowId":
			plan.Workflow.ID = value
		case "workflowName":
			plan.Workflow.Name = value
		case "agent":
			plan.Workflow.Agent = value
		case "agentVersion":
			plan.Workflow.AgentVersion = value
		case "publishAgent":
			plan.Publication.Agent = value
		case "sharedInstructions":
			plan.Publication.Instructions = value
		case "adoptWorkflow":
			plan.Adoption.Workflow = value
		case "adoptNode":
			plan.Adoption.Node = value
		case "adoptVersion":
			plan.Adoption.Version = value
		default:
			return Plan{}, ErrInvalidPlan
		}
	}
	if _, err = decoder.Token(); err != nil || decoder.Decode(new(any)) != io.EOF {
		return Plan{}, ErrInvalidPlan
	}
	if (plan.Kind != CancelExecution && seen["executionScope"]) || (plan.Kind != InspectExecution && plan.Kind != CancelExecution && seen["executionId"]) {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != CreateAgent && (seen["agentId"] || seen["agentName"] || seen["instructions"]) {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != PublishAgent && (seen["publishAgent"] || seen["sharedInstructions"]) {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != AdoptAgentVersion && (seen["adoptWorkflow"] || seen["adoptNode"] || seen["adoptVersion"]) {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != CreateWorkflow && (seen["workflowId"] || seen["workflowName"] || seen["agent"] || seen["agentVersion"]) {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != AnalyzePR && (seen["prNumber"] || seen["prWorkflow"] || seen["prConnection"] || seen["prRepository"] || seen["prUrl"] || seen["prNode"]) {
		return Plan{}, ErrInvalidPlan
	}
	if (seen["inspectArea"] && plan.Kind != InspectConsole && plan.Kind != FindResource) || (seen["resourceQuery"] && plan.Kind != FindResource) {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != SaveSkill && (seen["skillAction"] || seen["skillId"] || seen["skillTitle"] || seen["skillBody"]) {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != EditInstructions && (seen["instructionWorkflow"] || seen["instructionNode"] || seen["localInstructions"]) {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != GuideNode && (seen["guideWorkflow"] || seen["guideNode"] || seen["guideReplica"] || seen["guidance"]) {
		return Plan{}, ErrInvalidPlan
	}
	if (seen["newTitle"] || seen["newRequest"] || seen["newOutcome"]) && plan.Kind != CorrectTask {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != CreateTask && (seen["repositories"] || seen["taskTitle"] || seen["workflow"] || seen["workflowVersion"] || seen["request"] || seen["outcome"]) {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != DecideApproval && (seen["approvalId"] || seen["approvalDecision"] || seen["approvalAnswer"] || seen["approvalComment"]) {
		return Plan{}, ErrInvalidPlan
	}
	if seen["taskAuthorizationMode"] && plan.Kind != AuthorizeTask {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != PlanInstallation && (seen["document"] || seen["intakeConsent"]) {
		return Plan{}, ErrInvalidPlan
	}
	if seen["destination"] && plan.Kind != OpenManual {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != ControlWorkflow && (seen["controlWorkflow"] || seen["controlNode"] || seen["controlAction"]) {
		return Plan{}, ErrInvalidPlan
	}
	if seen["environmentWorkflow"] && plan.Kind != PrepareWorkflowEnvironment {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != ControlTrigger && (seen["triggerTarget"] || seen["triggerAction"]) {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != ScaleWorkflowNode && (seen["scaleWorkflow"] || seen["scaleNode"] || seen["scaleSize"] || seen["scaleMode"]) {
		return Plan{}, ErrInvalidPlan
	}
	if plan.Kind != RaiseBudget && (seen["budgetScope"] || seen["budgetTarget"] || seen["budgetAmount"] || seen["budgetCurrency"]) {
		return Plan{}, ErrInvalidPlan
	}
	switch plan.Kind {
	case PrepareWorkflowEnvironment:
		if !shortLabel(plan.EnvironmentWorkflow) || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case ControlTrigger:
		if !plan.Trigger.valid() || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case ScaleWorkflowNode:
		if !plan.Scale.valid() || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case RaiseBudget:
		if !plan.Budget.Valid() || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case AuthorizeTask:
		if !shortLabel(plan.Task) || !taskauth.Valid(plan.TaskAuthorizationMode) || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case DecideApproval:
		if !plan.Approval.valid(plan.Task) || seen["question"] || (seen["approvalId"] && seen["task"]) {
			return Plan{}, ErrInvalidPlan
		}
	case AdoptAgentVersion:
		if !plan.Adoption.valid() || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case PublishAgent:
		if !plan.Publication.valid() || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case ControlWorkflow:
		if !plan.Control.valid() || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case AnalyzePR:
		if !plan.PRSelector.Valid() || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case CancelExecution:
		if !validReferenceID(plan.ExecutionID) || (plan.ExecutionScope != "child" && plan.ExecutionScope != "tree") {
			return Plan{}, ErrInvalidPlan
		}
	case InspectExecution:
		if !validReferenceID(plan.ExecutionID) {
			return Plan{}, ErrInvalidPlan
		}
	case FindResource:
		if !SearchableArea(plan.Area) || !safeText(plan.Query, 200) || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case InspectConsole:
		if InspectionDestination(plan.Area) == "" || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case SaveSkill:
		if !plan.Skill.Valid() || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case EditInstructions:
		if !seen["localInstructions"] || !plan.Instructions.ValidInput() || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case GuideNode:
		if !plan.Guidance.Valid() || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case PlanInstallation:
		if plan.Document == "" || !safeText(plan.Document, MaxRequestBytes) || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case OpenManual:
		if !ManualDestination(plan.Destination) || seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case CorrectTask:
		if !shortLabel(plan.Task) || seen["question"] || !validCorrection(plan.Correction, true) || (seen["newTitle"] && plan.Correction.Title == "") || (seen["newRequest"] && plan.Correction.Request == "") || (seen["newOutcome"] && plan.Correction.Outcome == "") {
			return Plan{}, ErrInvalidPlan
		}
	case WatchTask, InspectTask, StartTask, RetryTask, CancelTask, PrepareTaskEnvironment:
		if plan.Task == "" || !safeText(plan.Task, 200) || strings.ContainsAny(plan.Task, "\r\n\t") || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case CreateTask:
		if seen["task"] || seen["question"] || plan.TaskDraft.validate() != nil {
			return Plan{}, ErrInvalidPlan
		}
	case Clarify:
		if plan.Question == "" || !safeText(plan.Question, 360) || seen["task"] {
			return Plan{}, ErrInvalidPlan
		}
	case Unsupported:
		if seen["task"] || seen["question"] {
			return Plan{}, ErrInvalidPlan
		}
	case CreateAgent:
		if seen["task"] || seen["question"] || plan.Agent.validate() != nil {
			return Plan{}, ErrInvalidPlan
		}
	case CreateWorkflow:
		if seen["task"] || seen["question"] || plan.Workflow.validate() != nil {
			return Plan{}, ErrInvalidPlan
		}
	default:
		return Plan{}, ErrInvalidPlan
	}
	return plan, nil
}
