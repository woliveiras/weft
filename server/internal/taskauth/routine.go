package taskauth

import (
	"encoding/json"

	"github.com/woliveiras/weft/server/internal/domain"
)

// Unrecognized tools require a decision in Assisted, including new tools that
// share a capability with a routine operation.
func routineTool(name, isolation string) bool {
	switch name {
	case "filesystem.read", "filesystem.list", "filesystem.search", "filesystem.stat", "filesystem.mkdir", "filesystem.write", "filesystem.edit", "filesystem.multi_edit", "git.status", "git.diff", "git.log", "git.show", "git.branches", "git.add", "git.commit", "code.outline", "code.map":
		return true
	case "check.run":
		return workspaceShellAllowed(isolation)
	}
	return false
}

// The SCM service checks the head branch, connection revision and candidate again before writing.
func boundDelivery(repos []domain.TaskRepository, args json.RawMessage) bool {
	var dest struct{ Connection string }
	if json.Unmarshal(args, &dest) != nil || dest.Connection == "" {
		return false
	}
	for _, repo := range repos {
		if repo.SCMConnection == dest.Connection && repo.Repository != "" && repo.TargetBranch != "" && repo.ConnectionRevision != "" {
			return true
		}
	}
	return false
}
