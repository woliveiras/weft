package chat

import (
	"strings"
	"testing"
	"time"
)

func TestWorkflowInstructionChangeBindsScopeAndExplicitReplacement(t *testing.T) {
	const common = `"kind":"workflow.instructions","instructionWorkflow":"editorial","instructionNode":"reader"`
	if _, err := decodePlan("{" + common + "}"); err == nil {
		t.Fatal("missing replacement accepted")
	}
	p, err := decodePlan("{" + common + `,"localInstructions":""}`)
	if err != nil {
		t.Fatal(err)
	}
	change := p.Instructions
	change.Version, change.SourceDigest, change.Before = 2, strings.Repeat("a", 64), "Check notes."
	r, err := NewInstructionReview("owner", "session", change, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r.Instructions.Node = "another"
	if err := r.ValidateConfirmation("owner", "session", r.Digest, time.Now()); err == nil {
		t.Fatal("node changed after review")
	}
	change.Graph = "task-run-locked"
	if change.ValidInput() {
		t.Fatal("task-owned workflow accepted")
	}
	change.Graph = "editorial"
	change.Before = "apiKey: synthetic-value"
	if _, err := NewInstructionReview("owner", "session", change, time.Now()); err == nil {
		t.Fatal("sensitive current instructions persisted")
	}
}
