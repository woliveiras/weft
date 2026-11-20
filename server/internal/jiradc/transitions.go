package jiradc

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
)

// Status names are presentation text. Only scoped IDs can identify a mapping.
type Status struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TransitionField is a bounded metadata subset, not a complete field editor or
// a write schema. In particular it does not authorize arbitrary field updates.
type TransitionField struct {
	Required        *bool    `json:"required"`
	Name            string   `json:"name"`
	HasDefaultValue *bool    `json:"hasDefaultValue,omitempty"`
	Operations      []string `json:"operations,omitempty"`
	Schema          struct {
		Type  string `json:"type"`
		Items string `json:"items,omitempty"`
	} `json:"schema"`
	AllowedValues []json.RawMessage `json:"allowedValues,omitempty"`
}
type Transition struct {
	ID     string                     `json:"id"`
	Name   string                     `json:"name"`
	To     Status                     `json:"to"`
	Fields map[string]TransitionField `json:"fields"`
}

// TransitionDiscovery is issue-specific, read-only evidence. It is not a workflow
// ID, an authorization grant or permission to replay any transition later.
type TransitionDiscovery struct {
	Site        string       `json:"site"`
	ProjectID   string       `json:"projectId"`
	IssueID     string       `json:"issueId"`
	IssueKey    string       `json:"issueKey"`
	IssueTypeID string       `json:"issueTypeId"`
	Current     Status       `json:"current"`
	Updated     string       `json:"updated"`
	Transitions []Transition `json:"transitions"`
}

var fieldName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,127}$`)

// ValidateTransitionMetadata checks the bounded discovery subset shared by
// edition adapters. Nil fields remain unavailable, distinct from empty fields.
func ValidateTransitionMetadata(v []Transition) error { return validateTransitions(v) }

func validateTransitions(v []Transition) error {
	if v == nil || len(v) > 100 {
		return errors.New("Jira transition metadata is missing or exceeds the bound")
	}
	seen := map[string]bool{}
	for _, tr := range v {
		if !numericID.MatchString(tr.ID) || seen[tr.ID] || !numericID.MatchString(tr.To.ID) || tr.Name == "" || len(tr.Name) > 512 || tr.To.Name == "" || len(tr.To.Name) > 512 || len(tr.Fields) > 100 {
			return errors.New("invalid Jira transition identity or field metadata")
		}
		seen[tr.ID] = true
		// A nil fields map means unavailable metadata, not an empty field requirement.
		for key, f := range tr.Fields {
			if !fieldName.MatchString(key) || f.Required == nil || f.Schema.Type == "" || len(f.Name) > 512 || len(f.AllowedValues) > 1000 || len(f.Operations) > 20 {
				return errors.New("incomplete Jira transition field metadata")
			}
		}
	}
	return nil
}
func (c *Client) DiscoverTransitions(ctx context.Context, key string) (TransitionDiscovery, error) {
	before, err := c.Issue(ctx, key)
	if err != nil {
		return TransitionDiscovery{}, err
	}
	var page struct {
		Transitions []Transition `json:"transitions"`
	}
	if err = c.do(ctx, "GET", "/issue/"+before.ID+"/transitions?expand=transitions.fields", nil, &page); err != nil {
		return TransitionDiscovery{}, err
	}
	if err = validateTransitions(page.Transitions); err != nil {
		return TransitionDiscovery{}, err
	}
	after, err := c.Issue(ctx, key)
	if err != nil {
		return TransitionDiscovery{}, err
	}
	if before.ID != after.ID || before.Fields.Project.ID != after.Fields.Project.ID || before.Fields.IssueType.ID != after.Fields.IssueType.ID || before.Fields.Status.ID != after.Fields.Status.ID || before.Fields.Updated != after.Fields.Updated {
		return TransitionDiscovery{}, errors.New("Jira issue changed during transition discovery; reload its configuration")
	}
	return TransitionDiscovery{Site: c.site, ProjectID: c.project, IssueID: before.ID, IssueKey: before.Key, IssueTypeID: before.Fields.IssueType.ID, Current: Status{ID: before.Fields.Status.ID, Name: before.Fields.Status.Name}, Updated: before.Fields.Updated, Transitions: page.Transitions}, nil
}
