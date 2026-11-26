package chat

import (
	"strings"
	"unicode"

	"github.com/woliveiras/weft/server/internal/taskconfig"
)

const TaskLinkView = "link"

// MatchesTaskLinkTarget binds a link to the user's ID or the preceding
// verified task. An explicit ID always takes precedence over prior context.
func MatchesTaskLinkTarget(text, id, previous string) bool {
	if !taskconfig.ValidID(id) {
		return false
	}
	found := false
	for _, word := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) && r != '_' && r != '-'
	}) {
		if !strings.HasPrefix(strings.ToLower(word), "tsk_") {
			continue
		}
		if !strings.EqualFold(word, id) {
			return false
		}
		found = true
	}
	return found || id == previous
}

// PriorTaskLinkTarget checks only the reply immediately before this request.
// Use a session projected for the caller's current role.
func (s Session) PriorTaskLinkTarget(requestID string) string {
	for i, message := range s.Messages {
		if message.ID != requestID || message.Role != "user" || i == 0 {
			continue
		}
		last := s.Messages[i-1]
		if last.Role != "assistant" {
			return ""
		}
		var id string
		if last.Task != nil {
			id = last.Task.ID
		}
		if last.Selection != nil && last.Selection.Area == "tasks" {
			if id != "" && id != last.Selection.ID {
				return ""
			}
			id = last.Selection.ID
		}
		if taskconfig.ValidID(id) {
			return id
		}
		return ""
	}
	return ""
}
