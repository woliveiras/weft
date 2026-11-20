package scm

import (
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"strings"
	"testing"
)

func TestAppendClosingIssue(t *testing.T) {
	if got := appendClosingIssue("", "ACME/App", "acme/app#42"); got != "Closes #42" {
		t.Fatal(got)
	}
	if got := appendClosingIssue(" Summary  ", "acme/app", "acme/tracker#42"); got != "Summary\n\nCloses acme/tracker#42" {
		t.Fatal(got)
	}
}

func TestTaskIssueReferenceUsesActualProviderPair(t *testing.T) {
	gh := taskconfig.IssueOrigin{Kind: "github", ConnectionID: "issues", ConnectionRevision: 2, Instance: "https://api.github.com", Key: "acme/app#7", URL: "https://github.com/acme/app/issues/7"}
	jira := taskconfig.IssueOrigin{Kind: "jira", ConnectionID: "issues", ConnectionRevision: 2, Instance: "https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555", Key: "APP-7", URL: "https://team.atlassian.net/browse/APP-7"}
	for _, kind := range []string{"github", "bitbucket-cloud", "bitbucket-dc"} {
		for _, origin := range []taskconfig.IssueOrigin{gh, jira} {
			got, err := appendTaskIssue("Verified.", Connection{Kind: kind, Repository: "acme/app"}, &taskconfig.Draft{TriggerIssueOrigin: &origin})
			closes := kind == "github" && origin.Kind == "github"
			if err != nil || strings.Contains(got, "Closes") != closes {
				t.Fatalf("%s/%s: %q %v", kind, origin.Kind, got, err)
			}
			if !closes && !strings.Contains(got, "Source issue: "+origin.URL) {
				t.Fatal(got)
			}
		}
	}
	bad := jira
	bad.Instance = "https://wrong.example"
	if _, err := appendTaskIssue("Verified.", Connection{Kind: "github"}, &taskconfig.Draft{TriggerIssueOrigin: &bad}); err == nil {
		t.Fatal("invalid stored origin accepted")
	}
}
