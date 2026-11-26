package chat

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type DraftRepositorySelector struct {
	Project string `json:"project"`
	Alias   string `json:"alias"`
	Commit  string `json:"commit,omitempty"`
}

type TaskDraft struct {
	Repositories    []DraftRepositorySelector
	Title           string
	Workflow        string
	WorkflowVersion string
	Request         string
	Outcome         string
}

func (d TaskDraft) validate() error {
	if !shortLabel(d.Title) || !shortLabel(d.Workflow) || strings.TrimSpace(d.Request) == "" || !safeText(d.Request, 3000) {
		return ErrInvalidPlan
	}
	if len(d.Repositories) > 8 {
		return ErrInvalidPlan
	}
	aliases := map[string]bool{}
	for _, repo := range d.Repositories {
		if !shortLabel(repo.Project) || !domain.ValidRepositoryAlias(repo.Alias) || aliases[repo.Alias] || (repo.Commit != "" && !domain.ValidFullCommit(repo.Commit)) {
			return ErrInvalidPlan
		}
		aliases[repo.Alias] = true
	}
	switch d.Outcome {
	case "understand", "implement", "investigate", "review", "freeform":
	default:
		return ErrInvalidPlan
	}
	if d.WorkflowVersion != "" {
		version, err := strconv.Atoi(d.WorkflowVersion)
		if err != nil || version < 1 || strconv.Itoa(version) != d.WorkflowVersion {
			return ErrInvalidPlan
		}
	}
	return nil
}

// TaskCreation records the fully bound input. The control plane derives its
// task ID from the operation and never dispatches it from this review.
type TaskCreation struct {
	Repositories     []StartRepository `json:"repositories,omitempty"`
	Input            taskconfig.Input  `json:"input"`
	WorkflowRevision string            `json:"workflowRevision"`
}

func (d TaskDraft) Input() taskconfig.Input {
	return taskconfig.Input{Title: d.Title, Request: d.Request, Outcome: d.Outcome}
}

func (d TaskDraft) MatchWorkflowVersion(version int) bool {
	return d.WorkflowVersion == "" || d.WorkflowVersion == strconv.Itoa(version)
}

func NewTaskReview(owner, session string, input taskconfig.Input, workflowRevision string, now time.Time, repositories ...StartRepository) (*OperationReview, error) {
	if err := taskconfig.Validate(input); err != nil || workflowRevision == "" {
		return nil, fmt.Errorf("%w: invalid draft bindings", ErrInvalidPlan)
	}
	return newReview(owner, session, now, OperationReview{TaskDraft: &TaskCreation{Input: input, WorkflowRevision: workflowRevision, Repositories: repositories}})
}
