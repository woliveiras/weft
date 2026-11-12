package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/tools"
)

const (
	trackerGet      = "tracker.issue"
	trackerComments = "tracker.comments"
	trackerRequeue  = "tracker.requeue"
	trackerLabels   = "tracker.labels"
	trackerComment  = "tracker.comment"
	trackerIdentity = "tracker.comment_identity"
)

var trackerKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}-[1-9][0-9]{0,9}$`)
var githubNumber = regexp.MustCompile(`^[1-9][0-9]{0,8}$`)

type TrackerCall func(ctx context.Context, connection, kind string, revision int64, method, path string, body any) (json.RawMessage, int, error)

type trackerScopeKey struct{}

type trackerArgs struct {
	UndoOwner string    `json:"undoOwner,omitempty"`
	Key       string    `json:"key"`
	Remove    []string  `json:"remove,omitempty"`
	Add       string    `json:"add,omitempty"`
	Owner     string    `json:"owner,omitempty"`
	Body      string    `json:"body,omitempty"`
	Since     time.Time `json:"since,omitempty"`
}

type trackerHTTPFailure struct {
	Status             int           `json:"status"`
	RateLimited        bool          `json:"rateLimited,omitempty"`
	RetryDelay         time.Duration `json:"retryDelay,omitempty"`
	RetryDeadline      *time.Time    `json:"retryDeadline,omitempty"`
	CredentialRejected bool          `json:"credentialRejected,omitempty"`
}

func (e trackerHTTPFailure) Error() string {
	return fmt.Sprintf("tracker returned HTTP %d", e.Status)
}

func (e trackerHTTPFailure) definitive() bool {
	if e.RateLimited || e.Status == http.StatusUnauthorized {
		return e.Status == http.StatusUnauthorized && e.CredentialRejected
	}
	return e.Status >= 400 && e.Status < 500 && e.Status != http.StatusRequestTimeout && e.Status != http.StatusTooManyRequests
}

func (e trackerHTTPFailure) guidance() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "The tracker rejected the credential. Check the connection and review trigger consent again."
	case http.StatusForbidden:
		return "The tracker refused the comment. Verify that the connection credential has permission to write issue comments."
	case http.StatusNotFound:
		return "The tracker could not find the issue. Check the trigger scope and the credential's issue access."
	case http.StatusUnprocessableEntity:
		return "The tracker rejected the comment. Review issue restrictions and the trigger connection."
	default:
		return "The tracker rejected the comment. Review the trigger connection and resolve the hold."
	}
}

// The transport exposes sanitized rate-limit metadata without passing
// response headers or bodies across the gateway.
func trackerFailure(status int, err error) trackerHTTPFailure {
	failure := trackerHTTPFailure{Status: status}
	var rate interface {
		TrackerRateLimit() (bool, time.Duration, time.Time)
	}
	if errors.As(err, &rate) {
		limited, delay, deadline := rate.TrackerRateLimit()
		failure.RateLimited, failure.RetryDelay = limited, delay
		if !deadline.IsZero() {
			failure.RetryDeadline = &deadline
		}
	}
	return failure
}

func trackerIdentityPath(source SourceConfig) string {
	if source.Kind == "jira" {
		return "/ex/jira/" + source.CloudID + "/rest/api/3/myself"
	}
	return "/user"
}

type trackerTool struct {
	name string
	call TrackerCall
}

func TrackerTools(call TrackerCall) []tools.Tool {
	return []tools.Tool{
		trackerTool{trackerGet, call}, trackerTool{trackerComments, call}, trackerTool{trackerRequeue, call},
		trackerTool{trackerLabels, call}, trackerTool{trackerComment, call}, trackerTool{trackerIdentity, call},
	}
}

func (t trackerTool) Name() string            { return t.name }
func (t trackerTool) Capability() string      { return t.name }
func (t trackerTool) Description() string     { return "Reviewed issue trigger tracker operation" }
func (t trackerTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t trackerTool) DescribeArgs(_ *tools.Principal, args json.RawMessage) json.RawMessage {
	var a trackerArgs
	if json.Unmarshal(args, &a) != nil {
		return json.RawMessage(`{}`)
	}
	a.Body = ""
	data, _ := json.Marshal(a)
	return data
}

func trackerPath(source SourceConfig, key string) (string, error) {
	switch source.Kind {
	case "github":
		repo, number, ok := strings.Cut(key, "#")
		if ok && strings.EqualFold(repo, source.Repository) && githubNumber.MatchString(number) {
			return "/repos/" + source.Repository + "/issues/" + number, nil
		}
	case "jira":
		if trackerKey.MatchString(key) {
			return "/ex/jira/" + source.CloudID + "/rest/api/3/issue/" + key, nil
		}
	}
	return "", tools.ErrDenied
}

func (t trackerTool) Invoke(ctx context.Context, inv *tools.Invocation) (*tools.Result, error) {
	scope, _ := ctx.Value(trackerScopeKey{}).(*Trigger)
	if scope == nil || inv.Principal.GraphID != "trigger-"+scope.ID || inv.Principal.ExecutionID != scope.ID ||
		!slices.Contains(scope.Consent.Operations, t.name) || t.call == nil {
		return nil, tools.ErrDenied
	}
	var a trackerArgs
	if json.Unmarshal(inv.Args, &a) != nil {
		return nil, tools.ErrDenied
	}
	path, err := trackerPath(scope.Source, a.Key)
	if err != nil {
		return nil, err
	}
	var status int
	var requestErr error
	request := func(method, path string, body any) (json.RawMessage, int, error) {
		raw, code, err := t.call(ctx, scope.Source.ConnectionID, scope.Source.Kind, scope.Consent.TrackerRevision, method, path, body)
		status, requestErr = code, err
		return raw, code, err
	}
	var out any
	switch t.name {
	case trackerGet:
		out, err = readIssue(request, scope.Source, path, a.Key)
	case trackerComments:
		out, err = readComments(request, scope.Source.Kind, path, a.Since)
	case trackerRequeue:
		out, err = readRequeue(request, scope.Source, path, a.Key, a.Since)
	case trackerLabels:
		if !validLabelChange(scope, a) {
			return nil, tools.ErrDenied
		}
		err = changeLabels(request, scope.Source.Kind, path, a)
	case trackerIdentity:
		actor, lookupErr := readJSON[trackerActor](request, http.MethodGet, trackerIdentityPath(scope.Source), nil)
		err = lookupErr
		out = actor.identity(scope.Source.Kind)
	case trackerComment:
		if len(a.Body) == 0 || len(a.Body) > 4096 {
			return nil, tools.ErrDenied
		}
		commentPath := path + "/comments"
		var body any = map[string]string{"body": a.Body}
		if scope.Source.Kind == "jira" {
			commentPath = path + "/comment"
			body = map[string]any{"body": jiraComment(a.Body)}
		}
		var raw json.RawMessage
		raw, _, err = request(http.MethodPost, commentPath, body)
		if err == nil {
			out, err = decodeCommentReceipt(scope.Source.Kind, raw)
		}
	}
	if err != nil && status >= 400 && status <= 599 {
		failure := trackerFailure(status, requestErr)
		if status == http.StatusUnauthorized {
			// Confirm on the same scoped connection. Never repeat a write
			// or return the identity lookup result as the original response.
			_, probeStatus, probeErr := request(http.MethodGet, trackerIdentityPath(scope.Source), nil)
			failure.CredentialRejected = probeStatus == http.StatusUnauthorized
			if probe := trackerFailure(probeStatus, probeErr); probe.RateLimited {
				failure.RateLimited, failure.RetryDelay, failure.RetryDeadline = true, probe.RetryDelay, probe.RetryDeadline
			}
		}
		data, _ := json.Marshal(failure)
		return &tools.Result{IsError: true, Content: failure.Error(), Data: data}, nil
	}
	if errors.Is(err, errTrackerHistoryLimit) {
		return &tools.Result{IsError: true, Content: errTrackerHistoryLimit.Error(), Data: trackerHistoryLimitResult}, nil
	}
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return &tools.Result{Data: raw}, nil
}

func jiraComment(text string) any {
	lines := strings.Split(text, "\n")
	content := make([]any, 0, len(lines))
	for _, line := range lines {
		paragraph := map[string]any{"type": "paragraph"}
		if line != "" {
			paragraph["content"] = []any{map[string]string{"type": "text", "text": line}}
		}
		content = append(content, paragraph)
	}
	return map[string]any{"type": "doc", "version": 1, "content": content}
}

type apiRequest func(method, path string, body any) (json.RawMessage, int, error)

func readJSON[T any](req apiRequest, method, path string, body any) (T, error) {
	var out T
	raw, _, err := req(method, path, body)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, errors.New("invalid tracker API result")
	}
	return out, nil
}

func validLabelChange(t *Trigger, a trackerArgs) bool {
	allowed := func(label string) bool {
		return label == t.Label || label == LabelTodo || label == LabelInProgress || label == LabelReview || label == LabelFailed
	}
	if !allowed(a.Add) || len(a.Remove) > 3 || (a.Owner != "" && (a.Add != LabelInProgress || a.Owner != t.Owner)) {
		return false
	}
	if a.UndoOwner != "" && (a.UndoOwner != t.Owner || a.Owner != "" || a.Add != LabelFailed || !slices.Contains(a.Remove, LabelInProgress)) {
		return false
	}
	for _, label := range a.Remove {
		if !allowed(label) || label == a.Add {
			return false
		}
	}
	return true
}

func changeLabels(req apiRequest, kind, path string, a trackerArgs) error {
	if kind == "jira" {
		labels := []map[string]string{{"add": a.Add}}
		for _, old := range a.Remove {
			labels = append(labels, map[string]string{"remove": old})
		}
		body := map[string]any{"update": map[string]any{"labels": labels}}
		if a.Owner != "" {
			body["fields"] = map[string]any{"assignee": map[string]string{"accountId": a.Owner}}
		} else if a.UndoOwner != "" {
			current, err := readJSON[jiraIssue](req, http.MethodGet, path+"?fields=assignee", nil)
			if err != nil {
				return err
			}
			if current.Fields.Assignee.AccountID == a.UndoOwner {
				body["fields"] = map[string]any{"assignee": nil}
			}
		}
		_, _, err := req(http.MethodPut, path, body)
		return err
	}
	if a.UndoOwner != "" {
		type assignee struct{ Login string }
		current, err := readJSON[struct {
			Assignees *[]assignee `json:"assignees"`
		}](req, http.MethodDelete, path+"/assignees", map[string]any{"assignees": []string{a.UndoOwner}})
		if err != nil {
			return err
		}
		if current.Assignees == nil || slices.ContainsFunc(*current.Assignees, func(ae assignee) bool {
			return strings.EqualFold(ae.Login, a.UndoOwner)
		}) {
			return errors.New("tracker did not confirm assignment rollback")
		}
	}
	if _, _, err := req(http.MethodPost, path+"/labels", map[string]any{"labels": []string{a.Add}}); err != nil {
		return err
	}
	for _, old := range a.Remove {
		if _, status, err := req(http.MethodDelete, path+"/labels/"+url.PathEscape(old), nil); err != nil {
			if status != http.StatusNotFound {
				return err
			}
			// A retry may find the label already absent. Confirm that the
			// issue is still accessible and that the requested removal holds.
			labels, getErr := readJSON[[]struct{ Name string }](req, http.MethodGet, path+"/labels?per_page=100", nil)
			if getErr != nil || len(labels) >= 100 || slices.ContainsFunc(labels, func(l struct{ Name string }) bool { return strings.EqualFold(l.Name, old) }) {
				return err
			}
		}
	}
	if a.Owner != "" {
		_, _, err := req(http.MethodPost, path+"/assignees", map[string]any{"assignees": []string{a.Owner}})
		return err
	}
	return nil
}

type trackerActor struct {
	ID          int64  `json:"id"`
	Login       string `json:"login"`
	Type        string `json:"type"`
	AccountID   string `json:"accountId"`
	AccountType string `json:"accountType"`
	Active      bool   `json:"active"`
}

func (a trackerActor) identity(kind string) string {
	if kind == "jira" {
		return a.AccountID
	}
	if a.ID > 0 {
		return strconv.FormatInt(a.ID, 10)
	}
	return ""
}

func (a trackerActor) human(kind string) string {
	if kind == "jira" && a.AccountType == "atlassian" && a.Active {
		return a.AccountID
	}
	if kind == "github" && a.Type == "User" && !strings.HasSuffix(strings.ToLower(a.Login), "[bot]") {
		return a.Login
	}
	return ""
}

type issueComment struct {
	AuthorID string    `json:"authorId,omitempty"`
	ID       string    `json:"id"`
	Actor    string    `json:"actor,omitempty"`
	Body     string    `json:"body"`
	At       time.Time `json:"at"`
}

type githubComment struct {
	ID      int64        `json:"id"`
	User    trackerActor `json:"user"`
	Body    string       `json:"body"`
	Created time.Time    `json:"created_at"`
}

type jiraCommentRecord struct {
	ID      string          `json:"id"`
	Author  trackerActor    `json:"author"`
	Body    json.RawMessage `json:"body"`
	Created string          `json:"created"`
}

type commentReceipt struct {
	ID       string    `json:"id"`
	AuthorID string    `json:"authorId"`
	At       time.Time `json:"at"`
}

func decodeCommentReceipt(kind string, raw json.RawMessage) (commentReceipt, error) {
	if len(raw) == 0 {
		return commentReceipt{}, nil
	}
	if kind == "jira" {
		var row jiraCommentRecord
		if err := json.Unmarshal(raw, &row); err != nil {
			return commentReceipt{}, errors.New("invalid tracker comment receipt")
		}
		return commentReceipt{ID: row.ID, AuthorID: row.Author.identity(kind), At: jiraTime(row.Created)}, nil
	}
	var row githubComment
	if err := json.Unmarshal(raw, &row); err != nil {
		return commentReceipt{}, errors.New("invalid tracker comment receipt")
	}
	id := ""
	if row.ID > 0 {
		id = strconv.FormatInt(row.ID, 10)
	}
	return commentReceipt{ID: id, AuthorID: row.User.identity(kind), At: row.Created}, nil
}

func jiraTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999-0700"} {
		if at, err := time.Parse(layout, s); err == nil {
			return at
		}
	}
	return time.Time{}
}

const trackerPage = 100

var errTrackerHistoryLimit = errors.New("tracker history exceeds the bounded scan")
var trackerHistoryLimitResult = json.RawMessage(`{"code":"tracker_history_limit"}`)

func readComments(req apiRequest, kind, path string, since time.Time) ([]issueComment, error) {
	var out []issueComment
	offset := 0
	for page := range 10 {
		if kind == "github" {
			query := fmt.Sprintf("/comments?per_page=%d&page=%d", trackerPage, page+1)
			if !since.IsZero() {
				query += "&since=" + url.QueryEscape(since.UTC().Format(time.RFC3339Nano))
			}
			rows, err := readJSON[[]githubComment](req, http.MethodGet, path+query, nil)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				if row.ID > 0 && row.Created.After(since) {
					out = append(out, issueComment{ID: strconv.FormatInt(row.ID, 10), AuthorID: row.User.identity(kind), Actor: row.User.human(kind), Body: row.Body, At: row.Created})
				}
			}
			if len(rows) < trackerPage {
				return out, nil
			}
			continue
		}
		query := fmt.Sprintf("/comment?orderBy=-created&startAt=%d&maxResults=%d", offset, trackerPage)
		result, err := readJSON[struct {
			Comments []jiraCommentRecord `json:"comments"`
			Total    int                 `json:"total"`
		}](req, http.MethodGet, path+query, nil)
		if err != nil {
			return nil, err
		}
		for _, row := range result.Comments {
			at := jiraTime(row.Created)
			if !at.IsZero() && !at.After(since) {
				return out, nil
			}
			if row.ID != "" && !at.IsZero() {
				out = append(out, issueComment{ID: row.ID, AuthorID: row.Author.identity(kind), Actor: row.Author.human(kind), Body: describe(row.Body), At: at})
			}
		}
		offset += len(result.Comments)
		if offset >= result.Total {
			return out, nil
		}
		if len(result.Comments) == 0 {
			return nil, errors.New("tracker comment page did not cover its reported history")
		}
	}
	return nil, errTrackerHistoryLimit
}

func readIssue(req apiRequest, source SourceConfig, path, key string) (Issue, error) {
	if source.Kind == "github" {
		raw, err := readJSON[githubIssue](req, http.MethodGet, path, nil)
		i := raw.issue(source.Repository)
		if err == nil && (i.Key != key || (len(raw.PullRequest) > 0 && string(raw.PullRequest) != "null")) {
			err = errors.New("tracker returned a different issue")
		}
		return i, err
	}
	raw, err := readJSON[jiraIssue](req, http.MethodGet, path+"?fields="+strings.Join(jiraFields, ","), nil)
	if err == nil && raw.Key != key {
		err = errors.New("tracker returned a different issue")
	}
	return raw.issue(), err
}

type labelEvent struct {
	ID    string
	Actor string
	At    time.Time
	Added bool
}

func readRequeue(req apiRequest, source SourceConfig, path, key string, since time.Time) (string, error) {
	if source.Kind == "github" {
		event, err := githubLabelEvent(req, path, since)
		if err != nil || !event.Added || event.Actor == "" {
			return "", err
		}
		if !githubLogin.MatchString(event.Actor) {
			return "", nil
		}
		permission, err := readJSON[struct {
			Role string `json:"role_name"`
		}](req, http.MethodGet,
			"/repos/"+source.Repository+"/collaborators/"+url.PathEscape(event.Actor)+"/permission", nil)
		if err != nil {
			return "", err
		}
		if permission.Role == "maintain" || permission.Role == "admin" {
			return event.ID, nil
		}
		return "", nil
	}
	event, err := jiraLabelEvent(req, path, since)
	if err != nil || !event.Added || event.Actor == "" {
		return "", err
	}
	issue, err := readJSON[jiraIssue](req, http.MethodGet, path+"?fields=project", nil)
	if err != nil {
		return "", err
	}
	project, err := strconv.ParseInt(issue.Fields.Project.ID, 10, 64)
	if err != nil || project <= 0 || issue.Key != key {
		return "", errors.New("invalid tracker project")
	}
	body := map[string]any{"accountId": event.Actor, "projectPermissions": []any{
		map[string]any{"permissions": []string{"ADMINISTER_PROJECTS"}, "projects": []int64{project}},
	}}
	perms, err := readJSON[struct {
		ProjectPermissions []struct {
			Permission string  `json:"permission"`
			Projects   []int64 `json:"projects"`
		} `json:"projectPermissions"`
	}](req, http.MethodPost, "/ex/jira/"+source.CloudID+"/rest/api/3/permissions/check", body)
	if err != nil {
		return "", err
	}
	for _, permission := range perms.ProjectPermissions {
		if permission.Permission == "ADMINISTER_PROJECTS" && slices.Contains(permission.Projects, project) {
			return event.ID, nil
		}
	}
	return "", nil
}

var githubLogin = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)

func githubLabelEvent(req apiRequest, path string, since time.Time) (labelEvent, error) {
	var latest labelEvent
	for page := range 10 {
		rows, err := readJSON[[]struct {
			ID    int64        `json:"id"`
			Event string       `json:"event"`
			Actor trackerActor `json:"actor"`
			Label struct {
				Name string `json:"name"`
			} `json:"label"`
			Created time.Time `json:"created_at"`
		}](req, http.MethodGet, fmt.Sprintf("%s/events?per_page=%d&page=%d", path, trackerPage, page+1), nil)
		if err != nil {
			return labelEvent{}, err
		}
		for _, row := range rows {
			if row.ID > 0 && row.Created.After(since) && !row.Created.Before(latest.At) &&
				strings.EqualFold(row.Label.Name, LabelTodo) && (row.Event == "labeled" || row.Event == "unlabeled") {
				latest = labelEvent{strconv.FormatInt(row.ID, 10), row.Actor.human("github"), row.Created, row.Event == "labeled"}
			}
		}
		if len(rows) < trackerPage {
			return latest, nil
		}
	}
	return labelEvent{}, errTrackerHistoryLimit
}

func jiraLabelEvent(req apiRequest, path string, since time.Time) (labelEvent, error) {
	type history struct {
		Total  int `json:"total"`
		Values []struct {
			ID      string       `json:"id"`
			Author  trackerActor `json:"author"`
			Created string       `json:"created"`
			Items   []struct {
				Field string `json:"field"`
				From  string `json:"fromString"`
				To    string `json:"toString"`
			} `json:"items"`
		} `json:"values"`
	}
	result, err := readJSON[history](req, http.MethodGet, path+"/changelog?maxResults=1", nil)
	if err != nil {
		return labelEvent{}, err
	}
	containsTodo := func(value string) bool {
		labels := strings.FieldsFunc(value, func(r rune) bool { return r == ' ' || r == ',' })
		return slices.ContainsFunc(labels, func(label string) bool { return strings.EqualFold(label, LabelTodo) })
	}
	offset := result.Total
	for page := 0; page < 10 && offset > 0; page++ {
		start := max(0, offset-trackerPage)
		rows, err := readJSON[history](req, http.MethodGet, fmt.Sprintf("%s/changelog?startAt=%d&maxResults=%d", path, start, offset-start), nil)
		if err != nil {
			return labelEvent{}, err
		}
		if len(rows.Values) != offset-start {
			return labelEvent{}, errTrackerHistoryLimit
		}
		for _, row := range slices.Backward(rows.Values) {
			at := jiraTime(row.Created)
			if !at.IsZero() && !at.After(since) {
				return labelEvent{}, nil
			}
			for _, item := range row.Items {
				if row.ID != "" && !at.IsZero() && item.Field == "labels" && containsTodo(item.From) != containsTodo(item.To) {
					return labelEvent{row.ID, row.Author.human("jira"), at, containsTodo(item.To)}, nil
				}
			}
		}
		offset = start
	}
	if offset > 0 {
		return labelEvent{}, errTrackerHistoryLimit
	}
	return labelEvent{}, nil
}
