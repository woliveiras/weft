package chat

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

var workflowDraftID = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

type WorkflowDraft struct {
	ID           string
	Name         string
	Agent        string
	AgentVersion string
}

func (d WorkflowDraft) validate() error {
	if !workflowDraftID.MatchString(d.ID) || d.ID == "new" || strings.HasPrefix(d.ID, domain.TaskRunGraphPrefix) ||
		!shortLabel(d.Name) || !shortLabel(d.Agent) {
		return ErrInvalidPlan
	}
	if d.AgentVersion != "" && d.AgentVersion != "latest" {
		version, err := strconv.Atoi(d.AgentVersion)
		if err != nil || version < 1 || strconv.Itoa(version) != d.AgentVersion {
			return ErrInvalidPlan
		}
	}
	return nil
}

func shortLabel(value string) bool {
	return strings.TrimSpace(value) != "" && safeText(value, 200) && !strings.ContainsAny(value, "\r\n\t")
}

// Graph uses an ingress that admits task requests once the operator separately
// configures and starts a task. It does not create or start any execution.
func (d WorkflowDraft) Graph(binding domain.AgentBinding) domain.Graph {
	const node, channel, message = "worker", "requests", "work.request"
	return domain.Graph{
		APIVersion: domain.APIVersion,
		Kind:       "Graph",
		Metadata:   domain.GraphMetadata{ID: d.ID, Name: d.Name},
		Spec: domain.GraphSpec{
			Nodes:    []domain.Node{{ID: node, Agent: &binding, Loop: domain.LoopSpec{Mode: domain.LoopEvent}}},
			Channels: []domain.Channel{{ID: channel, Type: domain.ChannelQueue, To: []string{node}, Types: []string{message}}},
			Task:     &domain.TaskDefaults{NodeID: node, ResultNodeID: node, ChannelID: channel, MessageType: message},
		},
	}
}

// WorkflowCreation stores the graph exactly as reviewed. The agent preview
// excludes its prompts, parameters, provider settings and runtime configuration.
type WorkflowCreation struct {
	Graph     domain.Graph `json:"graph"`
	AgentName string       `json:"agentName"`
	Runtime   string       `json:"runtime"`
	ReadOnly  bool         `json:"readOnly"`
	ToolNames []string     `json:"toolNames,omitempty"`
}

func NewWorkflowReview(owner, session string, creation WorkflowCreation, now time.Time) (*OperationReview, error) {
	return newReview(owner, session, now, OperationReview{Workflow: &creation})
}
