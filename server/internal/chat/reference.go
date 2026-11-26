package chat

import (
	"fmt"
	"regexp"
)

// A reference carries a confirmed identifier into the next model turn.
// Definitions, tool output, provider errors and approval material stay local.
var contextID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

func observedReference(message Message) string {
	if message.Role != "assistant" {
		return ""
	}
	kind, id, version := "", "", 0
	if message.Task != nil {
		kind, id = "task", message.Task.ID
	} else if r := message.Review; r != nil && (r.State == ReviewCreated || r.State == ReviewApplied) {
		switch {
		case r.AgentCreation != nil:
			kind, id, version = "catalog agent", r.AgentID, r.ResultVersion
		case r.Publication != nil:
			kind, id, version = "catalog agent", r.Publication.ID, r.ResultVersion
		case r.Workflow != nil:
			kind, id, version = "workflow", r.Workflow.Graph.Metadata.ID, r.ResultVersion
		case r.TaskDraft != nil || r.TaskRetry != nil || r.TaskCorrection != nil:
			kind, id = "task", r.ResultID
		}
	}
	if !contextID.MatchString(id) || credentialPattern.MatchString(id) {
		return ""
	}
	if version > 0 {
		return fmt.Sprintf("Weft observed %s %s at version %d.", kind, id, version)
	}
	return fmt.Sprintf("Weft observed %s %s.", kind, id)
}
