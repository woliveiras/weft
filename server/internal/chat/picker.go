package chat

import (
	"strings"

	"github.com/woliveiras/weft/server/internal/taskconfig"
)

// PickerArea bounds the read-only inventory available before a conversation exists.
func PickerArea(area string) bool {
	switch area {
	case "tasks", "agents", "workflows", "projects", "skills":
		return true
	}
	return false
}

// The first line binds an exact reference. Optional operator notes stay in the
// saved turn and never become a replacement selector or a second command.
func PickerCommand(text string) (Plan, bool) {
	line, _, _ := strings.Cut(text, "\n")
	args := strings.Fields(line)
	if len(args) == 0 {
		return Plan{}, false
	}
	switch args[0] {
	case "/follow":
		if len(args) == 2 && taskconfig.ValidID(args[1]) {
			return Plan{Kind: WatchTask, Area: "tasks", Task: args[1], Title: "Follow task"}, true
		}
	case "/lookup":
		if len(args) == 3 && PickerArea(args[1]) && validReferenceID(args[2]) {
			return Plan{Kind: FindResource, Area: args[1], Query: args[2], Title: "Find " + args[1]}, true
		}
	default:
		return Plan{}, false
	}
	return Plan{Kind: Clarify, Title: "Choose a resource"}, true
}
