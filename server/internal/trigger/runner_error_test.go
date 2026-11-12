package trigger

import (
	"errors"
	"testing"
)

func TestTriggerRunErrorIncludesEveryAffectedHold(t *testing.T) {
	if got := triggerRunError(nil, []Hold{{Key: "APP-1"}}); got != "" {
		t.Fatalf("unaffected hold produced an error: %q", got)
	}
	holds := []Hold{
		{Key: "APP-1", AccessError: "Check project access."},
		{Key: "APP-2"},
		{Key: "OPS-3", AccessError: "Check tracker availability."},
	}
	want := "Discovery unavailable. APP-1: Check project access. OPS-3: Check tracker availability."
	if got := triggerRunError(errors.New("Discovery unavailable."), holds); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
