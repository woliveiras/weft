package trigger

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/jiradc"
)

func TestNativeJiraNormalizationUsesCategoryAndPreservesIdentities(t *testing.T) {
	c, err := jiradc.New("https://jira.example/context", "10001", "fixture-only")
	if err != nil {
		t.Fatal(err)
	}
	var i jiradc.Issue
	if err := json.Unmarshal([]byte(`{"key":"APP-7","fields":{"summary":"Fix parser","description":"Input data","labels":["blocked"],"created":"2026-11-20T10:00:00.000+0000","updated":"2026-11-20T10:01:00.000+0000","status":{"name":"Review","statusCategory":{"key":"new"}},"reporter":{"name":"author","key":"author-key","active":true},"assignee":{"name":"operator","key":"operator-key"}}}`), &i); err != nil {
		t.Fatal(err)
	}
	out, err := nativeIssue(c, i)
	if err != nil || !out.Open || !out.Blocked || out.Author != "author" || out.Assignee != "operator" || out.URL != "https://jira.example/context/browse/APP-7" {
		t.Fatal(out, err)
	}
	i.Fields.Status.Name, i.Fields.Status.StatusCategory.Key = "To Do", "done"
	out, err = nativeIssue(c, i)
	if err != nil || out.Open {
		t.Fatal("display name changed eligibility", out, err)
	}
	i.Fields.Reporter.Active = false
	out, err = nativeIssue(c, i)
	if err != nil || out.Author != "" {
		t.Fatal("inactive author contributed", out, err)
	}
	i.Fields.Assignee.Name = ""
	if _, err := nativeIssue(c, i); err == nil {
		t.Fatal("unknown assignee treated as unassigned")
	}
	i.Fields.Assignee = nil
	i.Fields.Updated = "invalid"
	if _, err := nativeIssue(c, i); err == nil {
		t.Fatal("unknown timestamp accepted")
	}
}

func TestNativeSourceAndDedupScope(t *testing.T) {
	source := SourceConfig{Kind: "jira-dc", ConnectionID: "jira", SiteURL: "https://jira.example/context", ProjectID: "10001"}
	if _, err := source.Open(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*SourceConfig){
		func(c *SourceConfig) { c.SiteURL = "http://jira.example" },
		func(c *SourceConfig) { c.SiteURL += "?secret=bad" },
		func(c *SourceConfig) { c.SiteURL += "/" },
		func(c *SourceConfig) { c.ProjectID = "APP" },
		func(c *SourceConfig) { c.CloudID = "cloud" },
		func(c *SourceConfig) { c.Repository = "acme/app" },
	} {
		bad := source
		mutate(&bad)
		if _, err := bad.Open(); err == nil {
			t.Fatal("invalid scope accepted", bad)
		}
	}
	tr := &Trigger{Source: source, Workflow: "factory"}
	i := Issue{Source: "jira-dc", Key: "APP-7"}
	first := DedupKey(tr, i)
	tr.Source.SiteURL = "https://JIRA.EXAMPLE.:443/context"
	if DedupKey(tr, i) != first {
		t.Fatal("host spelling/default port reset admission")
	}
	tr.Source.SiteURL = source.SiteURL
	tr.Source.ConnectionID, tr.Source.Query = "other-credential", "labels = bug"
	if DedupKey(tr, i) != first {
		t.Fatal("credential/query reset admission")
	}
	tr.Source.ProjectID = "10002"
	if DedupKey(tr, i) == first {
		t.Fatal("different projects collided")
	}
	tr.Source.ProjectID, tr.Source.SiteURL = "10001", "https://other.example/context"
	if DedupKey(tr, i) == first || strings.Contains(first, "https://") {
		t.Fatal("different sites collided or leaked raw identity")
	}
}
