package chat

import "time"

const PrepareWorkflowEnvironment = "workflow.prepare_environment"

type WorkflowEnvironment struct {
	Workflow       string `json:"workflow"`
	Version        int    `json:"workflowVersion"`
	WorkflowDigest string `json:"workflowDigest"`
	DefinitionHash string `json:"definitionHash"`
	Definition     string `json:"definition"`
}

func NewWorkflowEnvironmentReview(owner, session string, build WorkflowEnvironment, now time.Time) (*OperationReview, error) {
	if !shortLabel(build.Workflow) || build.Version < 1 || build.WorkflowDigest == "" || build.DefinitionHash == "" ||
		build.Definition == "" || !safeText(build.Definition, 3000) {
		return nil, ErrInvalidPlan
	}
	return newReview(owner, session, now, OperationReview{WorkflowEnvironment: &build})
}
