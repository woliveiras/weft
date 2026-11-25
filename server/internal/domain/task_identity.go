package domain

import (
	"crypto/sha256"
	"encoding/base32"
	"strings"
)

// TaskRunHashedGraphPrefix distinguishes bounded execution IDs from the
// reversible legacy mapping. Valid task suffixes contain no hyphens.
const TaskRunHashedGraphPrefix = TaskRunGraphPrefix + "h-"

// TaskExecutionID preserves existing short IDs while fitting long task IDs into
// the graph's 63-byte DNS label. The full digest prevents prefix collisions;
// ownership remains recorded in graph labels and the task's execution binding.
func TaskExecutionID(id string) string {
	legacy := TaskRunGraphPrefix + strings.TrimPrefix(id, "tsk_")
	if len(legacy) <= 63 {
		return legacy
	}
	sum := sha256.Sum256([]byte(id))
	return TaskRunHashedGraphPrefix + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))
}
