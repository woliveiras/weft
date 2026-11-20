package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/tools"
)

const trackerSearch = "tracker.search"

var errNativeRequeueProof = errors.New("Jira Data Center cannot verify a label actor's project-administrator permission; resolve the hold in the console before selecting the issue again")
var nativeRequeueProofResult = json.RawMessage(`{"code":"native_requeue_unverified"}`)

func nativeScopeIdentity(source SourceConfig) string {
	u, err := url.Parse(source.SiteURL)
	if err != nil {
		return ""
	} // Source.Open and connection validation reject it.
	u.Host = strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(u.Host), ":443"), ".")
	return triggerFingerprint([]string{u.String(), source.ProjectID})
}

// Native sources use the same trigger-only gateway scope as lifecycle writes.
// No agent grant alone can obtain the private trigger context.
type nativeJira struct{ c SourceConfig }

func (s nativeJira) Tools() []string { return []string{trackerSearch} }
func (s nativeJira) Search(ctx context.Context, call Invoker, label string) ([]Issue, error) {
	return trackerRead[[]Issue](ctx, call, trackerSearch, trackerArgs{})
}
func (s nativeJira) MatchesHeld(ctx context.Context, call Invoker, i Issue, _ string) (bool, error) {
	if s.c.Query == "" {
		return true, nil
	}
	rows, err := trackerRead[[]Issue](ctx, call, trackerSearch, trackerArgs{Key: i.Key})
	return slices.ContainsFunc(rows, func(row Issue) bool { return row.Key == i.Key }), err
}

func nativeIssue(c *jiradc.Client, i jiradc.Issue) (Issue, error) {
	if jiraTime(i.Fields.Created).IsZero() || jiraTime(i.Fields.Updated).IsZero() {
		return Issue{}, errors.New("Jira Data Center issue timestamps are unavailable")
	}
	out := Issue{ID: i.ID, IssueTypeID: i.Fields.IssueType.ID, Source: "jira-dc", Key: i.Key, Title: i.Fields.Summary, Body: i.Fields.Description,
		URL: c.IssueURL(i), Created: i.Fields.Created, Updated: i.Fields.Updated, Labels: i.Fields.Labels,
		Open: i.Fields.Status.StatusCategory.Key == "new", Blocked: containsLabel(i.Fields.Labels, "blocked")}
	if i.Fields.Reporter != nil && i.Fields.Reporter.Active {
		out.Author = i.Fields.Reporter.Name
	}
	if i.Fields.Assignee != nil {
		if i.Fields.Assignee.Key == "" || i.Fields.Assignee.Name == "" {
			return Issue{}, errors.New("Jira Data Center assignee identity is unavailable")
		}
		out.Assignee = i.Fields.Assignee.Name
	}
	for _, link := range i.Fields.IssueLinks {
		if strings.EqualFold(link.Type.Inward, "is blocked by") && link.InwardIssue != nil {
			switch link.InwardIssue.Fields.Status.StatusCategory.Key {
			case "done":
			case "new", "indeterminate":
				out.Blocked = true
			default:
				return Issue{}, errors.New("Jira blocker status category is unavailable")
			}
		}
	}
	return out, nil
}

func containsLabel(labels []string, label string) bool {
	return slices.ContainsFunc(labels, func(value string) bool { return strings.EqualFold(value, label) })
}

func (t trackerTool) invokeNative(ctx context.Context, scope *Trigger, a trackerArgs) (*tools.Result, error) {
	if t.native == nil {
		return nil, tools.ErrDenied
	}
	if t.name == trackerTransition {
		v, err := (Manager{Store: t.native.Store, NativeConnections: t.native}).processLifecycleSync(ctx, scope, a.SyncID)
		raw, marshalErr := json.Marshal(v)
		return &tools.Result{Data: raw, IsError: err != nil, Content: v.Error}, errors.Join(err, marshalErr)
	}
	connection, err := t.native.Get(ctx, scope.Source.ConnectionID)
	if err != nil {
		return nil, err
	}
	if connection.SiteURL != scope.Source.SiteURL || connection.ProjectID != scope.Source.ProjectID || connection.Revision != scope.Consent.TrackerRevision || !connection.ProjectRestricted {
		return nil, tools.ErrDenied
	}
	write := t.name == trackerLabels || t.name == trackerComment
	var c *jiradc.Client
	if write {
		c, err = t.native.OpenForWrite(ctx, connection.Name, connection.Revision)
	} else {
		c, err = t.native.Open(ctx, connection.Name, connection.Revision)
	}
	if err != nil {
		return nil, err
	}
	if t.name != trackerSearch && t.name != trackerIdentity && !trackerKey.MatchString(a.Key) {
		return nil, tools.ErrDenied
	}
	var out any
	switch t.name {
	case trackerSearch:
		out, err = nativeSearch(ctx, c, scope, a.Key)
	case trackerGet:
		var i jiradc.Issue
		i, err = c.Issue(ctx, a.Key)
		if err == nil {
			out, err = nativeIssue(c, i)
		}
	case trackerLabels:
		if !validLabelChange(scope, a) {
			return nil, tools.ErrDenied
		}
		// DC claims preserve assignees. Owner only controls eligibility/replies.
		err = c.ChangeLabels(ctx, a.Key, jiradc.LabelChange{Remove: a.Remove, Add: a.Add})
	case trackerIdentity:
		var actor jiradc.Actor
		actor, err = c.Identity(ctx)
		out = actor.Key
	case trackerComments:
		out, err = nativeComments(ctx, c, a)
	case trackerRequeue:
		// /mypermissions describes the connection user, not the actor who
		// relabelled the issue. Never treat it as authorization of that actor.
		return &tools.Result{IsError: true, Content: errNativeRequeueProof.Error(), Data: nativeRequeueProofResult}, nil
	case trackerComment:
		if len(a.Body) == 0 || len(a.Body) > 4096 {
			return nil, tools.ErrDenied
		}
		var comment jiradc.Comment
		comment, err = c.AddComment(ctx, a.Key, a.Body)
		if err == nil {
			at := jiraTime(comment.Created)
			if at.IsZero() {
				err = errors.New("Jira did not confirm the comment timestamp; reconcile its marker before retrying")
			} else {
				out = commentReceipt{ID: comment.ID, AuthorID: comment.Author.Key, At: at}
			}
		}
	default:
		return nil, tools.ErrDenied
	}
	if err != nil {
		if errors.Is(err, errTrackerHistoryLimit) {
			return &tools.Result{IsError: true, Content: errTrackerHistoryLimit.Error(), Data: trackerHistoryLimitResult}, nil
		}
		var failure *jiradc.HTTPError
		if errors.As(err, &failure) {
			f := trackerFailure(failure.Status, err)
			if f.Status == http.StatusUnauthorized {
				_, probeErr := c.Identity(ctx)
				var probe *jiradc.HTTPError
				if errors.As(probeErr, &probe) {
					f.CredentialRejected = probe.Status == http.StatusUnauthorized
					if probe.Status == 429 {
						f.RateLimited, f.RetryDelay = true, probe.RetryAfter
					}
				}
			}
			if t.name == trackerGet && a.VerifyMissing && (f.Status == 404 || f.Status == 410) {
				probeErr := c.ProjectAccess(ctx)
				if probeErr == nil {
					f.AccessStatus = 200
				} else {
					var probe *jiradc.HTTPError
					if errors.As(probeErr, &probe) {
						f.AccessStatus = probe.Status
						if probe.Status == 429 {
							f.RateLimited, f.RetryDelay = true, probe.RetryAfter
						}
					}
				}
			}
			raw, _ := json.Marshal(f)
			return &tools.Result{IsError: true, Content: f.Error(), Data: raw}, nil
		}
		return nil, err
	}
	raw, err := json.Marshal(out)
	return &tools.Result{Data: raw}, err
}

func nativeSearch(ctx context.Context, c *jiradc.Client, scope *Trigger, key string) ([]Issue, error) {
	// Eligibility uses the provider category key from each issue, never a
	// localized status/category display name in JQL.
	query := `labels = ` + strconv.Quote(scope.Label)
	if key != "" {
		if !trackerKey.MatchString(key) {
			return nil, tools.ErrDenied
		}
		query = "key = " + strconv.Quote(key)
	}
	if scope.Source.Query != "" {
		query += " AND (" + scope.Source.Query + ")"
	}
	out := []Issue{}
	seen := map[string]bool{}
	start := 0
	for page := 0; page < 10; page++ {
		rows, next, err := c.Search(ctx, query, start)
		if err != nil {
			return nil, err
		}
		for _, raw := range rows.Issues {
			if seen[raw.ID] {
				return nil, errors.New("Jira search changed during pagination")
			}
			seen[raw.ID] = true
			i, err := nativeIssue(c, raw)
			if err != nil {
				return nil, err
			}
			if key != "" {
				if i.Key != key {
					return nil, errors.New("Jira returned an unrelated held issue")
				}
			} else if !i.Open || !i.HasLabel(scope.Label) {
				continue
			}
			out = append(out, i)
		}
		if next < 0 {
			return out, nil
		}
		start = next
	}
	return nil, errors.New("Jira issue search exceeds the bounded scan")
}

func nativeComments(ctx context.Context, c *jiradc.Client, a trackerArgs) ([]issueComment, error) {
	rows, err := c.Comments(ctx, a.Key)
	if errors.Is(err, jiradc.ErrHistoryLimit) {
		return nil, errTrackerHistoryLimit
	}
	if err != nil {
		return nil, err
	}
	out := []issueComment{}
	for _, row := range rows {
		at := jiraTime(row.Created)
		if at.IsZero() {
			return nil, errors.New("Jira comment timestamp is unavailable")
		}
		if !a.Since.IsZero() && at.Before(a.Since) {
			continue
		}
		// Active is required for a reply to contribute; stable keys reconcile
		// publication receipts, while logins match the reviewed reporter/owner.
		actor := ""
		if row.Author.Active {
			actor = row.Author.Name
		}
		out = append(out, issueComment{ID: row.ID, AuthorID: row.Author.Key, Actor: actor, Body: row.Body, At: at})
	}
	return out, nil
}
