package chat

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/workspace"
)

var ErrCheckpointsUnavailable = errors.New("the checkpoint list could not be read")
var checkpointArguments = []operationArgument{
	requiredArgument("target", "Exact task ID or workflow graph ID supplied by the user or previously verified."),
	optionalArgument("repository", "Exact checkout alias requested by the user. Omit when unspecified; if the tool returns choices, ask the user."),
	optionalArgument("title", "Short neutral conversation title."),
}

func CheckpointQuerySchema() llm.ToolSchema {
	parameters, _ := json.Marshal(operationObjectSchema(checkpointArguments))
	return llm.ToolSchema{Name: "list_checkpoints", Description: "List the latest recorded workspace checkpoints for one task attempt or workflow. Returns IDs, agents and recording context. Read-only. If a task has no execution or an alias is needed, explain the returned status. Never infer a restore.", Parameters: parameters}
}

type CheckpointQuery struct {
	Target     string
	Repository string
	Title      string
}

func ParseCheckpointQuery(raw json.RawMessage) (CheckpointQuery, error) {
	fields, err := decodeOperationArguments(raw, checkpointArguments)
	if err != nil {
		return CheckpointQuery{}, err
	}
	var q CheckpointQuery
	if err := json.Unmarshal(raw, &q); err != nil {
		return CheckpointQuery{}, ErrInvalidPlan
	}
	if !validReferenceID(q.Target) || strings.HasPrefix(q.Target, "tsk_") && !taskconfig.ValidID(q.Target) || fields["repository"] != nil && !domain.ValidRepositoryAlias(q.Repository) {
		return CheckpointQuery{}, ErrInvalidPlan
	}
	q.Title = cleanTitle(q.Title)
	return q, nil
}

type CheckpointRow struct {
	ID        string    `json:"id"`
	Agent     string    `json:"nodeId"`
	Replica   int       `json:"replica"`
	Execution string    `json:"executionId,omitempty"`
	Reason    string    `json:"reason"`
	Tool      string    `json:"tool,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func RecentCheckpointRows(items []workspace.Checkpoint) ([]CheckpointRow, error) {
	rows := make([]CheckpointRow, 0, min(InspectionLimit, len(items)))
	for _, c := range items[:min(InspectionLimit, len(items))] {
		if _, _, _, err := workspace.ParseCheckpointID(c.ID); err != nil || !shortLabel(c.ID) || !shortLabel(c.NodeID) || !shortLabel(c.Reason) || c.Tool != "" && !shortLabel(c.Tool) || c.ExecutionID != "" && !shortLabel(c.ExecutionID) {
			return nil, ErrCheckpointsUnavailable
		}
		rows = append(rows, CheckpointRow{ID: c.ID, Agent: c.NodeID, Replica: c.Replica, Execution: c.ExecutionID, Reason: c.Reason, Tool: c.Tool, CreatedAt: c.CreatedAt})
	}
	return rows, nil
}
