package chat

import (
	"strings"
	"testing"
	"time"
)

func TestPullRequestAnalysisBindsTheScope(t *testing.T) {
	for _, input := range []string{`{"kind":"pr.analyze","prNumber":"418"}`, `{"kind":"pr.analyze","prNumber":"418","prWorkflow":"review","prConnection":"origin"}`} {
		if _, err := decodePlan(input); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{`{"kind":"pr.analyze","prNumber":"-1"}`, `{"kind":"pr.analyze","prNumber":"0418"}`, `{"kind":"pr.analyze","prNumber":"3","task":"a"}`, `{"kind":"unsupported","prWorkflow":"review"}`} {
		if _, err := decodePlan(input); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	p := PRAnalysis{Number: 418, Head: strings.Repeat("a", 40), Base: strings.Repeat("b", 40), HeadRef: "feature", BaseRef: "main", Connection: "repo", ConnectionRevision: "v1", Provider: "github", Repository: "team/repo", Graph: "review", Node: "reader", Channel: "input", Run: "run", Version: 1, ModelName: "fixture", ModelProvider: "local"}
	r, err := NewPRReview("owner", "session", p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r.PR.Head = strings.Repeat("c", 40)
	if r.validateDigest("owner", "session", r.Digest) == nil {
		t.Fatal("modified source passed confirmation")
	}
	if SafeReviewContent("password=hidden") || SafeReviewContent(strings.Repeat("a", 13000)) {
		t.Fatal("unexpected source content")
	}
}
