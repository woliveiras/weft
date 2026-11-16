package trigger

import (
	"context"
	"errors"
	"strings"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/storage"
)

// LaunchError retains the internal cause for classification, but only exposes
// a bounded diagnostic assembled from trusted messages. Tracker content, build
// output, paths and dependency errors are never copied into public diagnostics.
type LaunchError struct {
	message string
	cause   error
}

func (e *LaunchError) Error() string { return e.message }
func (e *LaunchError) Unwrap() error { return e.cause }

func NewLaunchError(stage string, cause error, blockers ...string) *LaunchError {
	prefix := map[string]string{
		"project":       "Task project could not be loaded.",
		"draft":         "Task draft could not be saved.",
		"authorization": "Task authorization could not be recorded.",
		"review":        "Task requirements could not be reviewed.",
		"preparation":   "Task environment preparation failed.",
		"build":         "Task environment build failed.",
		"approval":      "Task environment approval was rejected or expired.",
		"start":         "Task could not start.",
	}[stage]
	if prefix == "" {
		prefix = "Task launch failed."
	}
	reason := "Review task requirements and service availability."
	if stage == "build" {
		reason = "The container image build command failed."
		if cause != nil && strings.Contains(cause.Error(), "setup:") {
			reason = "An environment setup command failed."
		}
	}
	if stage == "approval" {
		reason = "The decision did not authorize preparation."
	}
	switch {
	case errors.Is(cause, cost.ErrBudgetReached):
		reason = "The admission budget has been reached. Review Models → Costs."
	case errors.Is(cause, storage.ErrConflict):
		reason = "The workflow or task changed. Review the saved version and consent."
	case errors.Is(cause, storage.ErrNotFound):
		reason = "A required workflow, project or task record is unavailable."
	case errors.Is(cause, context.DeadlineExceeded):
		reason = "The operation timed out."
	case errors.Is(cause, context.Canceled):
		reason = "The operation was cancelled."
	}
	for _, b := range blockers {
		switch b {
		case "Environment preparation was interrupted. Inspect retained resources before retrying.", "Task workflows currently support once and event agents only.", "Select an enabled entry agent.", "Select an enabled result agent.",
			"Prepare and review the task environment before starting agents.",
			"Environment preparation is unavailable.",
			"Task authorization is stale. Review and confirm its current scope before starting.",
			"This workflow uses an existing workspace path. Task-owned local project workspaces are not available yet.":
			reason = b
			return &LaunchError{message: prefix + " " + reason, cause: cause}
		}
	}
	return &LaunchError{message: prefix + " " + reason, cause: cause}
}

func launchReason(err error) string {
	var failure *LaunchError
	if errors.As(err, &failure) {
		return failure.Error()
	}
	return NewLaunchError("start", err).Error()
}
