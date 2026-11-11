package taskconfig

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// IssueSource is a reference, never fetched when saving or reviewing a draft.
// Arguments are generated from bounded identifiers rather than arbitrary tool input.
type IssueSource struct {
	Kind         string `json:"kind"`
	ConnectionID string `json:"connectionId"`
	Reference    string `json:"reference"`
	CloudID      string `json:"cloudId,omitempty"`
}

var sourceConnection = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var githubIssue = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9-]{0,38})/([A-Za-z0-9_.-]{1,100})#([1-9][0-9]{0,8})$`)
var jiraIssue = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}-[1-9][0-9]{0,9}$`)
var cloudUUID = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

func (s IssueSource) ReadCall() (string, string, error) {
	invalid := func() (string, string, error) {
		return "", "", fmt.Errorf("%w: select a valid issue reference, connection and Jira cloud ID when required", ErrInvalid)
	}
	if !sourceConnection.MatchString(s.ConnectionID) || len(s.Reference) > 512 {
		return invalid()
	}
	ref := s.Reference
	if strings.Contains(ref, "://") {
		u, err := url.Parse(ref)
		if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" || u.Hostname() == "" || u.RawPath != "" {
			return invalid()
		}
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		switch s.Kind {
		case "github":
			if u.Host != "github.com" || len(parts) != 4 || parts[2] != "issues" {
				return invalid()
			}
			ref = parts[0] + "/" + parts[1] + "#" + parts[3]
		case "jira":
			if len(parts) != 2 || parts[0] != "browse" {
				return invalid()
			}
			ref = parts[1]
		default:
			return invalid()
		}
	}
	var tool string
	var args map[string]any
	switch s.Kind {
	case "github":
		match := githubIssue.FindStringSubmatch(ref)
		if match == nil || s.CloudID != "" || match[2] == "." || match[2] == ".." {
			return invalid()
		}
		number, err := strconv.Atoi(match[3])
		if err != nil {
			return invalid()
		}
		tool = "issue_read"
		args = map[string]any{"method": "get", "owner": match[1], "repo": match[2], "issue_number": number}
	case "jira":
		if !jiraIssue.MatchString(ref) || !cloudUUID.MatchString(s.CloudID) {
			return invalid()
		}
		tool = "getJiraIssue"
		args = map[string]any{"cloudId": s.CloudID, "issueIdOrKey": ref}
	default:
		return invalid()
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "", "", err
	}
	return "mcp." + s.ConnectionID + "." + tool, string(data), nil
}
