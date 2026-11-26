package chat

import (
	"strconv"
	"time"
)

const AdoptAgentVersion = "workflow.adopt_agent"

type AgentAdoptionDraft struct {
	Workflow string
	Node     string
	Version  string
}

func (d AgentAdoptionDraft) valid() bool {
	if !shortLabel(d.Workflow) || !shortLabel(d.Node) {
		return false
	}
	if d.Version == "latest" {
		return true
	}
	version, err := strconv.Atoi(d.Version)
	return err == nil && version > 0 && strconv.Itoa(version) == d.Version
}

type AgentAdoption struct {
	WorkflowID      string    `json:"workflowId"`
	NodeID          string    `json:"nodeId"`
	WorkflowVersion int       `json:"workflowVersion"`
	WorkflowCreated time.Time `json:"workflowCreated"`
	WorkflowDigest  string    `json:"workflowDigest"`
	AgentID         string    `json:"agentId"`
	CurrentVersion  int       `json:"currentVersion"`
	TargetVersion   int       `json:"targetVersion"`
	AgentDigest     string    `json:"agentDigest"`
	Before          string    `json:"before"`
	After           string    `json:"after"`
}

func NewAgentAdoptionReview(owner, session string, adoption AgentAdoption, now time.Time) (*OperationReview, error) {
	if !shortLabel(adoption.WorkflowID) || !shortLabel(adoption.NodeID) || !shortLabel(adoption.AgentID) ||
		adoption.WorkflowVersion < 1 || adoption.WorkflowCreated.IsZero() || adoption.WorkflowDigest == "" ||
		adoption.CurrentVersion < 1 || adoption.TargetVersion < 1 || adoption.CurrentVersion == adoption.TargetVersion ||
		adoption.AgentDigest == "" || !safeText(adoption.Before, 3000) || !safeText(adoption.After, 3000) {
		return nil, ErrInvalidPlan
	}
	return newReview(owner, session, now, OperationReview{Adoption: &adoption})
}
