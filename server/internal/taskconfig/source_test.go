package taskconfig

import (
	"encoding/json"
	"testing"
)

func TestIssueReadCall(t *testing.T) {
	for _, s := range []IssueSource{{Kind: "github", ConnectionID: "personal", Reference: "octocat/Hello-World#42"}, {Kind: "github", ConnectionID: "personal", Reference: "https://github.com/octocat/Hello-World/issues/42"}, {Kind: "jira", ConnectionID: "work", Reference: "APP-42", CloudID: "11111111-2222-3333-4444-555555555555"}, {Kind: "jira", ConnectionID: "work", Reference: "https://example.atlassian.net/browse/APP-42", CloudID: "11111111-2222-3333-4444-555555555555"}} {
		tool, args, err := s.ReadCall()
		if err != nil || tool == "" || !json.Valid([]byte(args)) {
			t.Fatal(tool, args, err)
		}
	}
	for _, ref := range []string{"https://user:pass@github.com/o/r/issues/1", "https://github.com/o/r/issues/1?token=x", "https://github.com/o/r/issues/1#secret", "https://evil.test/o/r/issues/1", "o/r#0", "o/r#9999999999", "o/r#1\n", "https://github.com/o/r/issues/%31", "o/..#1"} {
		if _, _, err := (IssueSource{Kind: "github", ConnectionID: "personal", Reference: ref}).ReadCall(); err == nil {
			t.Fatal(ref)
		}
	}
	for _, s := range []IssueSource{{Kind: "jira", ConnectionID: "work", Reference: "APP-1"}, {Kind: "github", ConnectionID: "*", Reference: "o/r#1"}, {Kind: "custom", ConnectionID: "work", Reference: "x"}} {
		if _, _, err := s.ReadCall(); err == nil {
			t.Fatal(s)
		}
	}
}
