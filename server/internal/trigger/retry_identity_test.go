package trigger

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestRetryMatchesTrustedIssueOrigin(t *testing.T) {
	const cloud = "11111111-2222-3333-4444-555555555555"
	for _, kind := range []string{"github", "jira", "jira-dc"} {
		for _, scenario := range []string{"same", "rotated-credential", "connection", "instance", "key", "kind", "invalid-key", "project", "mixed-legacy"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				source := SourceConfig{Kind: kind, ConnectionID: "issues"}
				origin := taskconfig.IssueOrigin{Kind: kind, ConnectionID: "issues", ConnectionRevision: 3, Key: "APP-7"}
				switch kind {
				case "github":
					source.Repository = "acme/app"
					origin.Key = "acme/app#7"
					origin.Instance = "https://api.github.com"
				case "jira":
					source.CloudID = cloud
					source.ProjectID = "10001"
					origin.Instance = "https://api.atlassian.com/ex/jira/" + cloud
				case "jira-dc":
					source.SiteURL = "https://jira.example/context"
					source.ProjectID = "10001"
					origin.Instance = source.SiteURL
					origin.ProjectID = source.ProjectID
				}
				draft := &taskconfig.Draft{TriggerIssueOrigin: &origin}
				issue := origin.Key
				want := scenario == "same" || scenario == "rotated-credential" || (scenario == "project" && kind != "jira-dc")
				switch scenario {
				case "rotated-credential":
					origin.ConnectionRevision = 1
				case "connection":
					origin.ConnectionID = "other"
				case "instance":
					origin.Instance = "https://other.example/context"
				case "key":
					origin.Key = "APP-8"
				case "kind":
					origin.Kind = "jira-other"
				case "invalid-key":
					origin.Key = "../APP-7"
					issue = origin.Key
				case "project":
					if kind == "jira-dc" {
						origin.ProjectID = "10002"
					}
				case "mixed-legacy":
					draft.TriggerGitHubIssue = "acme/app#9"
				}
				tr := &Trigger{Source: source, Consent: Consent{TrackerRevision: 9}}
				if got := retryMatchesOrigin(tr, draft, issue); got != want {
					t.Fatalf("identity match=%v want %v", got, want)
				}
			})
		}
	}
}

func TestRetryHistoricalGitHubOriginCompatibility(t *testing.T) {
	tr := &Trigger{Source: SourceConfig{Kind: "github", ConnectionID: "gh", Repository: "acme/app"}}
	for _, key := range []string{"acme/app#7", "other/app#7", "acme/app#garbage", "acme/app#-1"} {
		d := &taskconfig.Draft{TriggerGitHubIssue: key}
		if got := retryMatchesOrigin(tr, d, key); got != (key == "acme/app#7") {
			t.Fatalf("legacy %s accepted=%v", key, got)
		}
	}
	tr.Source.Kind = "jira"
	if retryMatchesOrigin(tr, &taskconfig.Draft{TriggerGitHubIssue: "acme/app#7"}, "acme/app#7") {
		t.Fatal("legacy issue changed provider")
	}
}
