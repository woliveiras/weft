package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/jiracloud"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/tools"
)

func cloudSource() SourceConfig {
	return SourceConfig{Kind: "jira", ConnectionID: "jira-mcp", CloudID: "11111111-1111-1111-1111-111111111111", SiteURL: "https://api.atlassian.com/ex/jira/11111111-1111-1111-1111-111111111111", ProjectID: "10001", ProfileID: "team", ProfileRevision: 2}
}
func TestCloudProfilePinsRequireExactEditionScopeWithoutChangingLegacy(t *testing.T) {
	good := cloudSource()
	if _, err := good.Open(); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*SourceConfig){func(v *SourceConfig) { v.Kind = "jira-dc" }, func(v *SourceConfig) { v.Kind = "github" }, func(v *SourceConfig) { v.ProfileRevision = 0 }, func(v *SourceConfig) { v.ProfileID = "" }, func(v *SourceConfig) { v.SiteURL = "https://team.atlassian.net" }, func(v *SourceConfig) { v.ProjectID = "TEAM" }, func(v *SourceConfig) { v.CloudID = "other" }} {
		v := good
		edit(&v)
		if _, err := v.Open(); err == nil {
			t.Fatal(v)
		}
	}
	legacy := SourceConfig{Kind: "jira", ConnectionID: "jira-mcp", CloudID: good.CloudID}
	if _, err := legacy.Open(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(raw), "profile") {
		t.Fatal(string(raw), err)
	}
	pin := LifecycleMappings{PRDelivered: MappingPin{Name: "review", Revision: 1}}
	if pin.validate(good) != nil || pin.validate(legacy) == nil {
		t.Fatal("mapping authority was inferred")
	}
	if workflowConnection(good) != "team" || workflowRevision(good, 7) != 2 {
		t.Fatal("MCP revision mixed with workflow authority")
	}
	dc := SourceConfig{Kind: "jira-dc", ConnectionID: "native"}
	if workflowConnection(dc) != "native" || workflowRevision(dc, 7) != 7 {
		t.Fatal("DC authority changed")
	}
}
func TestCloudIssueIdentityValidationOnlyAppliesToOptIn(t *testing.T) {
	var raw jiraIssue
	if err := json.Unmarshal([]byte(`{"id":"10007","key":"APP-7","fields":{"project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":"10","statusCategory":{"key":"new"}}}}`), &raw); err != nil {
		t.Fatal(err)
	}
	if err := validateCloudIssue(cloudSource(), raw); err != nil {
		t.Fatal(err)
	}
	got := raw.issue()
	if got.ID != "10007" || got.IssueTypeID != "10002" || got.StatusID != "10" {
		t.Fatal(got)
	}
	for _, edit := range []func(*jiraIssue){func(v *jiraIssue) { v.ID = "APP-7" }, func(v *jiraIssue) { v.Fields.IssueType.ID = "" }, func(v *jiraIssue) { v.Fields.Project.ID = "10099" }, func(v *jiraIssue) { v.Fields.Status.ID = "Review" }, func(v *jiraIssue) { v.Fields.Status.StatusCategory.Key = "unknown" }} {
		v := raw
		edit(&v)
		if validateCloudIssue(cloudSource(), v) == nil {
			t.Fatal(v)
		}
		if validateCloudIssue(SourceConfig{Kind: "jira"}, v) != nil {
			t.Fatal("legacy shape rejected")
		}
	}
}

func TestCloudProfileSearchUsesProjectAndBoundedContinuation(t *testing.T) {
	for _, mode := range []string{"success", "duplicate", "foreign", "dependency", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			source := cloudSource()
			if mode == "legacy" {
				source.ProfileID = ""
				source.ProfileRevision = 0
			}
			calls := 0
			rows, err := (jira{c: source}).Search(t.Context(), func(_ context.Context, _ string, args any) (*tools.Result, error) {
				calls++
				a := args.(map[string]any)
				fields, ok := a["fields"].([]string)
				if a["view"] != "full" || !ok || !slices.Equal(fields, jiraFields) || a["maxResults"] != trackerSearchLimit {
					t.Error("search lost the full view, required fields or bound")
				}
				query := a["jql"].(string)
				if mode == "legacy" {
					if !strings.Contains(query, `statusCategory = "To Do"`) {
						t.Error("legacy JQL changed", query)
					}
				} else if !strings.Contains(query, "project = 10001") || strings.Contains(query, "To Do") {
					t.Error("unscoped JQL", query)
				}
				if mode == "dependency" {
					return nil, errors.New("fixture search unavailable")
				}
				id := fmt.Sprint(10006 + calls)
				if mode == "duplicate" {
					id = "10007"
				}
				project := "10001"
				if mode == "foreign" {
					project = "10099"
				}
				next := ""
				if calls == 1 && mode != "legacy" {
					next = "next-page"
				} else if calls > 1 && a["nextPageToken"] != "next-page" {
					t.Error("continuation omitted")
				}
				raw := fmt.Sprintf(`{"issues":[{"id":%q,"key":"APP-7","fields":{"project":{"id":%q},"issuetype":{"id":"10002"},"status":{"id":"10","statusCategory":{"key":"new"}}}}],"nextPageToken":%q}`, id, project, next)
				return &tools.Result{Content: raw}, nil
			}, "weft")
			if mode == "success" || mode == "legacy" {
				want := 2
				if mode == "legacy" {
					want = 1
				}
				if err != nil || len(rows) != want || calls != want {
					t.Fatal(rows, err, calls)
				}
			} else if err == nil {
				t.Fatal("invalid search accepted", mode, rows)
			}
		})
	}
}

func TestLifecycleIntentPinsProfileRatherThanMCPAndRejectsMissingBackend(t *testing.T) {
	source := cloudSource()
	v := LifecycleSync{ID: strings.Repeat("a", 64), Source: source, ConnectionRevision: 2, Mapping: MappingPin{Name: "review", Revision: 1}, IssueID: "10007", IssueTypeID: "10002", IssueKey: "APP-7"}
	p := jiradc.TransitionIntent{OperationID: "lifecycle:" + v.ID, Connection: source.ProfileID, ConnectionRevision: 2, MappingName: "review", MappingRevision: 1, IssueID: v.IssueID, IssueTypeID: v.IssueTypeID, IssueKey: v.IssueKey}
	if !lifecycleIntentMatches(v, p) {
		t.Fatal("valid profile authority rejected")
	}
	p.Connection = source.ConnectionID
	if lifecycleIntentMatches(v, p) {
		t.Fatal("MCP identity replaced profile")
	}
	for _, m := range []Manager{{}, {CloudProfiles: &jiracloud.Profiles{}}, {NativeConnections: &jiradc.Connections{}}} {
		if _, _, err := m.workflowServices(source); err == nil {
			t.Fatal("missing Cloud backend accepted")
		}
		if _, err := m.cloudProfile(t.Context(), source, 1); err == nil {
			t.Fatal("missing profile authority accepted")
		}
	}
}
