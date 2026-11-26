package taskconfig

import (
	"context"
	"fmt"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"net/url"
	"regexp"
	"strings"
)

// IssueOrigin is trusted trigger metadata, separate from the editable request.
// Instance identifies the API/site, including Jira Cloud's scoped cloud ID.
// It never carries credentials and does not grant tracker or SCM access.
type IssueOrigin struct {
	IssueID            string `json:"issueId,omitempty"`
	IssueTypeID        string `json:"issueTypeId,omitempty"`
	Kind               string `json:"kind"`
	ConnectionID       string `json:"connectionId"`
	ConnectionRevision int64  `json:"connectionRevision"`
	Instance           string `json:"instance"`
	ProjectID          string `json:"projectId,omitempty"`
	Key                string `json:"key"`
	URL                string `json:"url,omitempty"`
}

var nativeIssueNumber = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func (o IssueOrigin) Validate() error {
	invalid := fmt.Errorf("%w: invalid trusted issue origin", ErrInvalid)
	if !sourceConnection.MatchString(o.ConnectionID) || o.ConnectionRevision < 1 {
		return invalid
	}
	switch o.Kind {
	case "jira-dc":
		if (o.IssueID != "" || o.IssueTypeID != "") && (!nativeIssueNumber.MatchString(o.IssueID) || !nativeIssueNumber.MatchString(o.IssueTypeID)) {
			return invalid
		}
		if jiradc.ValidateScope(o.Instance, o.ProjectID) != nil || strings.HasSuffix(o.Instance, "/") || !jiraIssue.MatchString(o.Key) {
			return invalid
		}
		if o.URL != "" && o.URL != o.Instance+"/browse/"+o.Key {
			return invalid
		}
	case "github":
		match := githubIssue.FindStringSubmatch(o.Key)
		if match == nil || match[2] == "." || match[2] == ".." || o.Instance != "https://api.github.com" {
			return invalid
		}
		repo, num, _ := strings.Cut(o.Key, "#")
		if o.URL != "" && o.URL != "https://github.com/"+repo+"/issues/"+num {
			return invalid
		}
	case "jira":
		cloud, ok := strings.CutPrefix(o.Instance, "https://api.atlassian.com/ex/jira/")
		if !ok {
			return invalid
		}
		if !jiraIssue.MatchString(o.Key) || !cloudUUID.MatchString(cloud) {
			return invalid
		}
		if o.URL != "" {
			u, err := url.Parse(o.URL)
			if err != nil || u.Scheme != "https" || !strings.HasSuffix(u.Hostname(), ".atlassian.net") || u.Port() != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Path != "/browse/"+o.Key {
				return invalid
			}
		}
	default:
		return invalid
	}
	return nil
}

// RequiresPullRequest keeps delivery separate from a successful agent turn.
func (d *Draft) RequiresPullRequest() bool {
	return d.TriggerGitHubIssue != "" || d.TriggerIssueOrigin != nil
}

// SaveTriggeredIssue preserves the reviewed tracker identity without adding an
// agent-side MCP read or coupling it to the independently selected SCM target.
func (m Manager) SaveTriggeredIssue(ctx context.Context, in Input, actor string, origin IssueOrigin) (*Draft, error) {
	if err := origin.Validate(); err != nil {
		return nil, err
	}
	legacy := ""
	if origin.Kind == "github" {
		legacy = origin.Key
	}
	return m.saveDraft(ctx, "", in, 0, actor, legacy, &origin, "")
}
