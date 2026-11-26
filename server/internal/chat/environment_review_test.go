package chat

import (
	"strings"
	"testing"
	"time"
)

func TestEnvironmentReviewBindsDefinitionAndRejectsUnsafeContent(t *testing.T) {
	p := EnvironmentPreparation{Task: "tsk_demo", Revision: 1, Workflow: "example", Version: 2, WorkflowDigest: "revision", DefinitionHash: "envhash", Definition: `{"image":"fixture:1"}`}
	r, err := NewEnvironmentReview("owner", "session", p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r.Environment.Definition = `{"image":"fixture:2"}`
	digest, err := r.digest("owner", "session")
	if err != nil || digest == r.Digest {
		t.Fatal("unbound definition", err)
	}
	for _, s := range []string{"", strings.Repeat("x", 3001), `{"password":"fixture"}`} {
		p.Definition = s
		if _, err = NewEnvironmentReview("owner", "session", p, time.Now()); err == nil {
			t.Fatal("accepted unsafe definition")
		}
	}
	if _, err = decodePlan(`{"kind":"task.prepare_environment","task":"sample"}`); err != nil {
		t.Fatal(err)
	}
	if _, err = decodePlan(`{"kind":"task.prepare_environment","task":"sample","controlAction":"start"}`); err == nil {
		t.Fatal("accepted mixed intent")
	}
}
