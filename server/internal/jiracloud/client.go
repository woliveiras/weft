package jiracloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
)

const bodyLimit = 1 << 20

// Client has a credential-free immutable profile snapshot. Each operation
// revalidates that profile and MCP authority; writes also run the core's mapping
// guard immediately before entering the trusted REST transport.
type Client struct {
	profiles    Profiles
	profile     Profile
	writable    bool
	beforeWrite func(context.Context) error
}

type HTTPError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string { return fmt.Sprintf("Jira Cloud returned HTTP %d", e.Status) }
func (e *HTTPError) TrackerRateLimit() (bool, time.Duration, time.Time) {
	return e.Status == http.StatusTooManyRequests, e.RetryAfter, time.Time{}
}

func (c *Client) check(ctx context.Context, writable bool) error {
	p, _, err := c.profiles.checked(ctx, c.profile.Name, c.profile.Revision, writable)
	if err != nil {
		return err
	}
	if p.Input != c.profile.Input {
		return storage.ErrConflict
	}
	return nil
}

func (c *Client) AuthorizeWrite(ctx context.Context) error {
	if !c.writable {
		return ErrWriteDisabled
	}
	if err := c.check(ctx, true); err != nil {
		return err
	}
	if c.beforeWrite != nil {
		if err := c.beforeWrite(ctx); err != nil {
			return err
		}
	}
	return c.check(ctx, true)
}

func (c *Client) do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	if err := c.check(ctx, c.writable); err != nil {
		return nil, err
	}
	return c.request(ctx, method, path, body)
}

func (c *Client) request(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	raw, code, err := c.profiles.API.Call(ctx, c.profile.MCPConnection, "jira", c.profile.MCPRevision, method, "/ex/jira/"+c.profile.CloudID+"/rest/api/3"+path, body)
	if code != 0 && (code < 200 || code >= 300) {
		out := &HTTPError{Status: code}
		var rate interface {
			TrackerRateLimit() (bool, time.Duration, time.Time)
		}
		if errors.As(err, &rate) {
			_, delay, _ := rate.TrackerRateLimit()
			out.RetryAfter = max(0, min(delay, time.Hour))
		}
		return nil, out
	}
	if err != nil || code < 200 || code >= 300 {
		return nil, errors.New("Jira Cloud tracker API request failed")
	}
	if len(raw) > bodyLimit {
		return nil, errors.New("Jira Cloud response exceeds its bound")
	}
	return raw, nil
}

// Issue requests only workflow identity fields. Cloud ADF issue descriptions are
// not decoded using the DC description schema or retained in synchronization.
func (c *Client) Issue(ctx context.Context, key string) (jiradc.Issue, error) {
	if err := jiradc.ValidateIssueKey(key); err != nil {
		return jiradc.Issue{}, err
	}
	raw, err := c.do(ctx, http.MethodGet, "/issue/"+key+"?fields=project,issuetype,status,updated", nil)
	if err != nil {
		return jiradc.Issue{}, err
	}
	var snapshot struct {
		ID, Key string
		Fields  struct {
			Project   struct{ ID string } `json:"project"`
			IssueType struct{ ID string } `json:"issuetype"`
			Status    jiradc.Status       `json:"status"`
			Updated   string              `json:"updated"`
		}
	}
	if json.Unmarshal(raw, &snapshot) != nil || snapshot.Key != key || !numericID.MatchString(snapshot.ID) || snapshot.Fields.Project.ID != c.profile.ProjectID || !numericID.MatchString(snapshot.Fields.IssueType.ID) || !numericID.MatchString(snapshot.Fields.Status.ID) || !jiradc.ValidIssueUpdated(snapshot.Fields.Updated) {
		return jiradc.Issue{}, scopeError()
	}
	out := jiradc.Issue{ID: snapshot.ID, Key: snapshot.Key}
	out.Fields.Project.ID = snapshot.Fields.Project.ID
	out.Fields.IssueType.ID = snapshot.Fields.IssueType.ID
	out.Fields.Status.ID, out.Fields.Status.Name = snapshot.Fields.Status.ID, snapshot.Fields.Status.Name
	out.Fields.Updated = snapshot.Fields.Updated
	return out, nil
}

func (c *Client) DiscoverTransitions(ctx context.Context, key string) (jiradc.TransitionDiscovery, error) {
	before, err := c.Issue(ctx, key)
	if err != nil {
		return jiradc.TransitionDiscovery{}, err
	}
	raw, err := c.do(ctx, http.MethodGet, "/issue/"+before.ID+"/transitions?expand=transitions.fields", nil)
	if err != nil {
		return jiradc.TransitionDiscovery{}, err
	}
	var page struct {
		Transitions []jiradc.Transition `json:"transitions"`
	}
	if json.Unmarshal(raw, &page) != nil || jiradc.ValidateTransitionMetadata(page.Transitions) != nil {
		return jiradc.TransitionDiscovery{}, errors.New("Jira Cloud transition metadata is incomplete or exceeds its bound")
	}
	after, err := c.Issue(ctx, key)
	if err != nil {
		return jiradc.TransitionDiscovery{}, err
	}
	if !sameIssue(before, after) {
		return jiradc.TransitionDiscovery{}, errors.New("Jira Cloud issue changed during transition discovery")
	}
	return jiradc.TransitionDiscovery{Site: c.profile.SiteURL, ProjectID: c.profile.ProjectID, IssueID: before.ID, IssueKey: before.Key,
		IssueTypeID: before.Fields.IssueType.ID, Current: jiradc.Status{ID: before.Fields.Status.ID, Name: before.Fields.Status.Name}, Updated: before.Fields.Updated, Transitions: page.Transitions}, nil
}

func sameIssue(a, b jiradc.Issue) bool {
	return a.ID == b.ID && a.Key == b.Key && a.Fields.Project.ID == b.Fields.Project.ID && a.Fields.IssueType.ID == b.Fields.IssueType.ID && a.Fields.Status.ID == b.Fields.Status.ID && a.Fields.Updated == b.Fields.Updated
}

func (c *Client) SendTransition(ctx context.Context, v jiradc.TransitionIntent) (bool, error) {
	if v.Connection != c.profile.Name || v.ConnectionRevision != c.profile.Revision || !numericID.MatchString(v.IssueID) || !numericID.MatchString(v.IssueTypeID) || !numericID.MatchString(v.TransitionID) || !numericID.MatchString(v.FromStatusID) || !numericID.MatchString(v.TargetStatusID) || v.FromStatusID == v.TargetStatusID {
		return false, ErrInvalid
	}
	if err := c.AuthorizeWrite(ctx); err != nil {
		return false, err
	}
	i, err := c.Issue(ctx, v.IssueKey)
	if err != nil {
		return false, err
	}
	if i.ID != v.IssueID || i.Fields.IssueType.ID != v.IssueTypeID || i.Fields.Status.ID != v.FromStatusID {
		return false, scopeError()
	}
	if err := c.AuthorizeWrite(ctx); err != nil {
		return false, err
	}
	// TrackerAPI.Call does not expose its HTTP dispatch stage. Once entered,
	// even a transport-local rejection is conservatively unknown to the core.
	// It must be reconciled by reads and must never authorize a blind retry.
	_, err = c.request(ctx, http.MethodPost, "/issue/"+v.IssueID+"/transitions", map[string]any{"transition": map[string]string{"id": v.TransitionID}})
	return true, err
}

var _ jiradc.WorkflowClient = (*Client)(nil)
