package chat

import (
	"encoding/json"
	"time"

	"github.com/woliveiras/weft/server/internal/taskauth"
)

const AuthorizeTask = "task.authorize"

type TaskAuthorization struct {
	ID              string                 `json:"id"`
	Title           string                 `json:"title"`
	TaskRevision    int64                  `json:"taskRevision"`
	WorkflowID      string                 `json:"workflowId"`
	WorkflowVersion int                    `json:"workflowVersion"`
	ExecutionMode   string                 `json:"executionMode,omitempty"`
	Current         taskauth.StartConsent  `json:"current"`
	Mode            taskauth.Mode          `json:"mode"`
	Agents          []taskauth.AgentScope  `json:"agents"`
	Repositories    []AuthorizedRepository `json:"repositories,omitempty"`
}

type AuthorizedRepository struct {
	Alias         string `json:"alias,omitempty"`
	ProjectID     string `json:"projectId"`
	BaseCommit    string `json:"baseCommit"`
	SCMConnection string `json:"scmConnection,omitempty"`
	Repository    string `json:"repository,omitempty"`
	TargetBranch  string `json:"targetBranch,omitempty"`
}

func NewTaskAuthorizationReview(owner, session string, auth TaskAuthorization, now time.Time) (*OperationReview, error) {
	if auth.ID == "" || auth.TaskRevision < 1 || auth.WorkflowID == "" || auth.WorkflowVersion < 1 ||
		auth.Current.Scope == "" || !taskauth.Valid(auth.Current.Mode) || !taskauth.Valid(auth.Mode) {
		return nil, ErrInvalidPlan
	}
	body, err := json.Marshal(auth)
	if err != nil || !safeText(string(body), MaxRequestBytes) {
		return nil, ErrInvalidPlan
	}
	return newReview(owner, session, now, OperationReview{TaskAuthorization: &auth})
}
