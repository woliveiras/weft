package jiradc

import (
	"context"
	"errors"
	"net/http"
	"regexp"
)

var label = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,49}$`)

// LabelChange uses relative updates so unrelated labels survive the claim.
// Assignment is deliberately untouched: REST 2 offers no reviewed assignee CAS.
// Callers restrict the allowed Weft labels.
type LabelChange struct {
	Remove []string
	Add    string
}

func (change LabelChange) validate() error {
	if len(change.Remove) > 5 || (change.Add == "" && len(change.Remove) == 0) {
		return errors.New("invalid Jira label change")
	}
	for _, value := range change.Remove {
		if !label.MatchString(value) {
			return errors.New("invalid Jira label")
		}
	}
	if change.Add != "" && !label.MatchString(change.Add) {
		return errors.New("invalid Jira label")
	}
	return nil
}

// ChangeLabels performs one numeric-issue PUT after project preflight. The
// reviewed account must be remotely restricted to this project. A failed or
// interrupted response must be reconciled by reading the current issue before
// any retry; this method never retries a mutation.
func (c *Client) ChangeLabels(ctx context.Context, key string, change LabelChange) error {
	if err := c.authorizeWrite(ctx); err != nil {
		return err
	}
	if err := change.validate(); err != nil {
		return err
	}
	for _, value := range append(append([]string{change.Add}, change.Remove...), key) {
		if containsSecret(value, c.token, 0) {
			return errors.New("invalid Jira label change")
		}
	}
	i, err := c.Issue(ctx, key)
	if err != nil {
		return err
	}
	updates := []map[string]string{}
	for _, value := range change.Remove {
		updates = append(updates, map[string]string{"remove": value})
	}
	if change.Add != "" {
		updates = append(updates, map[string]string{"add": change.Add})
	}
	body := map[string]any{}
	if len(updates) > 0 {
		body["update"] = map[string]any{"labels": updates}
	}
	if len(body) == 0 {
		return nil
	}
	return c.do(ctx, http.MethodPut, "/issue/"+i.ID, body, nil)
}

// ProjectAccess verifies access to the configured numeric project, not the
// mutable project prefix of an issue key. It returns no project payload.
func (c *Client) ProjectAccess(ctx context.Context) error {
	var project struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodGet, "/project/"+c.project, nil, &project); err != nil {
		return err
	}
	if project.ID != c.project {
		return errors.New("Jira project access is unverified")
	}
	return nil
}
