package chat

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type TaskPayloadChange struct {
	Channel     string `json:"channel"`
	MessageType string `json:"messageType"`
	Before      string `json:"before"`
	After       string `json:"after"`
}

func validTaskPayload(text string) bool {
	if !safeText(text, MaxRequestBytes) || !json.Valid([]byte(text)) {
		return false
	}
	// Inspect every decoded string in the original document. An object map
	// would discard earlier occurrences of a repeated field before review.
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return true
		}
		if err != nil {
			return false
		}
		value, ok := token.(string)
		if !ok {
			continue
		}
		if !safeText(value, MaxRequestBytes) {
			return false
		}
		rest := strings.TrimLeft(text[decoder.InputOffset():], " \t\r\n")
		if strings.HasPrefix(rest, ":") && credentialPattern.MatchString(value+"\":value") {
			return false
		}
	}
}

func NewTaskPayloadCorrection(owner, session string, draft *taskconfig.Draft, payload, workflowDigest string, now time.Time) (*OperationReview, error) {
	if draft == nil || draft.Revision < 1 || workflowDigest == "" || draft.PayloadJSON == payload ||
		!shortLabel(draft.ChannelID) || !shortLabel(draft.MessageType) || !validTaskPayload(draft.PayloadJSON) || !validTaskPayload(payload) {
		return nil, ErrInvalidPlan
	}
	if _, err := draft.ReplacedPayloadInput(payload); err != nil {
		return nil, err
	}
	correction := &TaskCorrection{Task: draft.ID, Revision: draft.Revision, Workflow: draft.GraphID, WorkflowVersion: draft.GraphVersion, WorkflowDigest: workflowDigest,
		Payload: &TaskPayloadChange{Channel: draft.ChannelID, MessageType: draft.MessageType, Before: draft.PayloadJSON, After: payload}}
	return newReview(owner, session, now, OperationReview{TaskCorrection: correction})
}
