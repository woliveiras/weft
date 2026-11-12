package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestTrackerOperationsRequireTrustedTriggerScope(t *testing.T) {
	dispatched := false
	op := trackerTool{trackerLabels, func(context.Context, string, string, int64, string, string, any) (json.RawMessage, int, error) {
		dispatched = true
		return nil, 200, nil
	}}
	reg := tools.NewRegistry()
	reg.MustRegister(op)
	gw := tools.NewGateway(reg, nil, nil)
	p := &tools.Principal{GraphID: "trigger-fixture", ExecutionID: "fixture", Grants: []domain.ToolGrant{{Name: trackerLabels}}}
	source := SourceConfig{Kind: "github", Repository: "acme/app"}
	tr := &Trigger{ID: "fixture", Source: source, Label: LabelTodo, Consent: Consent{Operations: []string{trackerLabels}}}
	for _, tc := range []struct {
		scope       *Trigger
		key, remove string
	}{
		{nil, "acme/app#7", LabelTodo},
		{tr, "elsewhere/app#7", LabelTodo},
		{tr, "acme/app#7", "unrelated"},
		{tr, "acme/app#../7", LabelTodo},
	} {
		ctx := t.Context()
		if tc.scope != nil {
			ctx = context.WithValue(ctx, trackerScopeKey{}, tc.scope)
		}
		_, err := GatewayInvoker(gw, p)(ctx, trackerLabels, trackerArgs{Key: tc.key, Add: LabelInProgress, Remove: []string{tc.remove}})
		if !errors.Is(err, tools.ErrDenied) || dispatched {
			t.Fatalf("unreviewed tracker operation dispatched: %+v, %v", tc, err)
		}
	}
}

func TestTrackerLabelChangesPreserveUnrelatedConcurrentLabels(t *testing.T) {
	labels := []string{LabelTodo, "bug"}
	var calls []string
	req := func(method, path string, body any) (json.RawMessage, int, error) {
		calls = append(calls, method+" "+path)
		if method == http.MethodPost {
			labels = append(labels, body.(map[string]any)["labels"].([]string)...)
			labels = append(labels, "added-concurrently")
		}
		if method == http.MethodDelete {
			if labels[0] != LabelTodo {
				t.Fatal(labels)
			}
			labels = labels[1:]
		}
		return nil, 200, nil
	}
	if err := changeLabels(req, "github", "/repos/acme/app/issues/1", trackerArgs{Add: LabelFailed, Remove: []string{LabelTodo}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(labels, []string{"bug", LabelFailed, "added-concurrently"}) || len(calls) != 2 {
		t.Fatalf("concurrent labels lost: %v calls %v", labels, calls)
	}
}

func TestJiraVerifiedManualRequeueUsesHistoryAndProjectPermission(t *testing.T) {
	since := time.Now().UTC()
	eventTime := since.Add(time.Minute)
	calls := 0
	source := SourceConfig{Kind: "jira", CloudID: "11111111-2222-3333-4444-555555555555"}
	path, err := trackerPath(source, "APP-7")
	if err != nil {
		t.Fatal(err)
	}
	var projectPermission = true
	req := func(method, route string, body any) (json.RawMessage, int, error) {
		calls++
		switch {
		case strings.Contains(route, "/changelog"):
			data, _ := json.Marshal(map[string]any{"total": 1, "values": []any{map[string]any{
				"id": "77", "created": eventTime.Format(time.RFC3339Nano),
				"author": map[string]any{"accountId": "human", "accountType": "atlassian", "active": true},
				"items":  []any{map[string]string{"field": "labels", "fromString": "bug", "toString": "bug, weft:todo"}},
			}}})
			return data, 200, nil
		case strings.HasSuffix(route, "?fields=project"):
			return json.RawMessage(`{"key":"APP-7","fields":{"project":{"id":"101"}}}`), 200, nil
		case method == "POST" && strings.HasSuffix(route, "/permissions/check"):
			raw, _ := json.Marshal(body)
			if !strings.Contains(string(raw), `"accountId":"human"`) {
				t.Fatal(string(raw))
			}
			if projectPermission {
				return json.RawMessage(`{"projectPermissions":[{"permission":"ADMINISTER_PROJECTS","projects":[101]}]}`), 200, nil
			}
			return json.RawMessage(`{"projectPermissions":[]}`), 200, nil
		default:
			t.Fatalf("unexpected operation %s %s", method, route)
			return nil, 0, nil
		}
	}
	if id, err := readRequeue(req, source, path, "APP-7", since); err != nil || id != "77" || calls != 4 {
		t.Fatalf("verified Jira event: %q, %v calls %d", id, err, calls)
	}
	projectPermission = false
	if id, err := readRequeue(req, source, path, "APP-7", since); err != nil || id != "" {
		t.Fatalf("unprivileged Jira event accepted: %q, %v", id, err)
	}
}

func TestDefinitiveTrackerCommentFailures(t *testing.T) {
	for _, status := range []int{401, 403, 404, 422, 408, 429, 500, 503} {
		failure := trackerHTTPFailure{Status: status}
		want := status < 500 && status != 401 && status != 408 && status != 429
		if failure.definitive() != want || failure.guidance() == "" {
			t.Fatalf("wrong disposition for %d", status)
		}
	}
}

func TestJiraRollbackClearsOnlyTheReviewedAssignment(t *testing.T) {
	for _, account := range []string{"reviewed", "different"} {
		req := func(method, path string, body any) (json.RawMessage, int, error) {
			if method == http.MethodGet {
				raw, _ := json.Marshal(map[string]any{"fields": map[string]any{"assignee": map[string]string{"accountId": account}}})
				return raw, 200, nil
			}
			m := body.(map[string]any)
			fields, clears := m["fields"].(map[string]any)
			if clears != (account == "reviewed") || (clears && fields["assignee"] != nil) {
				t.Fatal(m)
			}
			return nil, 204, nil
		}
		if err := changeLabels(req, "jira", "/issue/APP-1", trackerArgs{Add: LabelFailed, Remove: []string{LabelInProgress}, UndoOwner: "reviewed"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestJiraCommentScanAdvancesByReturnedRows(t *testing.T) {
	calls := 0
	req := func(_, path string, _ any) (json.RawMessage, int, error) {
		want := "startAt=0"
		if calls > 0 {
			want = "startAt=1"
		}
		if !strings.Contains(path, want) {
			t.Fatalf("expected %s in %s", want, path)
		}
		calls++
		return json.RawMessage(`{"total":2,"comments":[{"id":"1","created":"2026-11-12T10:00:00Z","body":"reply"}]}`), 200, nil
	}
	if replies, err := readComments(req, "jira", "/issue/APP-1", time.Time{}); err != nil || calls != 2 || len(replies) != 2 {
		t.Fatalf("short pages skipped rows: %d %v", calls, err)
	}
}

func TestTracker401RequiresIdentityConfirmation(t *testing.T) {
	for _, probe := range []int{200, 401, 503} {
		requests := 0
		op := trackerTool{trackerLabels, func(_ context.Context, _, _ string, _ int64, method, path string, _ any) (json.RawMessage, int, error) {
			requests++
			if requests == 1 && method == "POST" && strings.HasSuffix(path, "/labels") {
				return nil, 401, errors.New("fixture unauthorized")
			}
			if requests != 2 || method != "GET" || path != "/user" {
				t.Fatalf("unexpected request %s %s", method, path)
			}
			return nil, probe, nil
		}}
		tr := &Trigger{ID: "fixture", Source: SourceConfig{Kind: "github", Repository: "acme/app"}, Label: LabelTodo,
			Consent: Consent{Operations: []string{trackerLabels}}}
		ctx := context.WithValue(t.Context(), trackerScopeKey{}, tr)
		res, err := op.Invoke(ctx, &tools.Invocation{Principal: Principal(tr), Args: json.RawMessage(`{"key":"acme/app#7","add":"weft:needs-attention"}`)})
		var failure trackerHTTPFailure
		if err != nil || requests != 2 || !errors.As(trackerResultFailure(res, err), &failure) || failure.definitive() != (probe == 401) {
			t.Fatalf("probe %d: result %+v, %v, requests %d", probe, res, err, requests)
		}
	}
}

func TestOnlyAnExplicitTracker404MeansIssueMissing(t *testing.T) {
	for _, err := range []error{nil, errors.New("HTTP 404"), trackerHTTPFailure{Status: 403}, trackerHTTPFailure{Status: 503}} {
		if trackerIssueMissing(err) {
			t.Fatalf("incorrectly classified %v", err)
		}
	}
	if !trackerIssueMissing(errors.Join(errors.New("fixture context"), trackerHTTPFailure{Status: 404})) {
		t.Fatal("wrapped tracker 404 was lost")
	}
}

func TestMissingHoldDispositionRequiresConsecutiveVerifiedPolls(t *testing.T) {
	now := time.Now().UTC()
	tr := &Trigger{Source: SourceConfig{Kind: "github"}, PollSequence: 5, IntervalMinutes: 15}
	for _, tc := range []struct {
		name    string
		hold    Hold
		failure trackerHTTPFailure
		release bool
		reason  string
		err     error
	}{
		{"missing repository", Hold{MissingPoll: 5}, trackerHTTPFailure{Status: 404, AccessStatus: 404}, false, "", errHeldRepositoryAccess},
		{"refused repository", Hold{MissingPoll: 5}, trackerHTTPFailure{Status: 404, AccessStatus: 403}, false, "", errHeldRepositoryAccess},
		{"failed probe", Hold{MissingPoll: 5}, trackerHTTPFailure{Status: 404, AccessStatus: 503}, false, "", errHeldIssueAccessUnverified},
		{"miss after gap", Hold{MissingPoll: 4, MissingSince: now.Add(-time.Hour)}, trackerHTTPFailure{Status: 404, AccessStatus: 200}, false, issueMissingAwaitingPoll, nil},
		{"second miss at boundary", Hold{MissingPoll: 5, MissingSince: now.Add(-15 * time.Minute)}, trackerHTTPFailure{Status: 404, AccessStatus: 200}, true, "one polling interval apart", nil},
		{"early second miss", Hold{MissingPoll: 5, MissingSince: now.Add(-time.Minute)}, trackerHTTPFailure{Status: 404, AccessStatus: 200}, false, issueMissingAwaitingPoll, nil},
		{"legacy observation without timestamp", Hold{MissingPoll: 5}, trackerHTTPFailure{Status: 404, AccessStatus: 200}, false, issueMissingAwaitingPoll, nil},
		{"deleted", Hold{}, trackerHTTPFailure{Status: 410, AccessStatus: 200}, true, "HTTP 410", nil},
		{"disabled Issues", Hold{}, trackerHTTPFailure{Status: 410, AccessStatus: 200, IssuesDisabled: true}, false, "", errHeldIssuesDisabled},
		{"deleted with inaccessible repository", Hold{}, trackerHTTPFailure{Status: 410, AccessStatus: 404}, false, "", errHeldRepositoryAccess},
		{"operator deleted", Hold{Reason: "snapshot_round_limit"}, trackerHTTPFailure{Status: 410, AccessStatus: 200}, false, issueMissingForOperator, nil},
		{"comment failure", Hold{Publication: "comment_failed", MissingPoll: 5}, trackerHTTPFailure{Status: 404, AccessStatus: 200}, false, issueMissingForOperator, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release, reason, err := missingHoldDisposition(tr, tc.hold, tc.failure, now)
			if release != tc.release || !strings.Contains(reason, tc.reason) || !errors.Is(err, tc.err) {
				t.Fatalf("got %t, %q, %v", release, reason, err)
			}
		})
	}
}

func TestReadinessRetryCapsTheGatewayDeadline(t *testing.T) {
	now := time.Now().UTC()
	far := now.Add(24 * time.Hour)
	failure := trackerHTTPFailure{Status: 403, RateLimited: true, RetryDelay: 24 * time.Hour, RetryDeadline: &far}
	if got := trackerRetryDeadline(now, failure); !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("retry beyond cap: %v", got)
	}
}

func TestGitHubIssueAccessRequiresAnExplicitIssuesSetting(t *testing.T) {
	for _, tc := range []struct {
		body       string
		wantStatus int
		disabled   bool
	}{
		{`{"has_issues":true}`, 200, false},
		{`{"has_issues":false}`, 200, true},
		{`{}`, 0, false},
		{`{"has_issues":null}`, 0, false},
	} {
		code, disabled, err := githubIssueAccess(func(method, path string, body any) (json.RawMessage, int, error) {
			if method != http.MethodGet || path != "/repos/acme/app" || body != nil {
				t.Fatalf("unexpected probe: %s %s", method, path)
			}
			return json.RawMessage(tc.body), 200, nil
		}, "acme/app")
		if code != tc.wantStatus || disabled != tc.disabled || (err != nil) != (tc.wantStatus == 0) {
			t.Fatalf("probe for %s: %d, %t, %v", tc.body, code, disabled, err)
		}
	}
}

func TestJiraIssueAccessReadsOnlyTheIssueProject(t *testing.T) {
	const cloudID = "11111111-2222-3333-4444-555555555555"
	for _, tc := range []struct {
		key, body  string
		wantStatus int
	}{
		{"APP_1-5", `{"key":"APP_1"}`, 200},
		{"APP_1-5", `{"key":"OTHER"}`, 0},
		{"../APP-5", `{"key":"APP"}`, 0},
	} {
		calls := 0
		code, err := jiraIssueAccess(func(method, path string, body any) (json.RawMessage, int, error) {
			calls++
			if method != http.MethodGet || path != "/ex/jira/"+cloudID+"/rest/api/3/project/APP_1" || body != nil {
				t.Fatalf("probe escaped the held issue project: %s %s", method, path)
			}
			return json.RawMessage(tc.body), 200, nil
		}, cloudID, tc.key)
		if code != tc.wantStatus || (err != nil) != (code == 0) || (strings.HasPrefix(tc.key, "..") && calls != 0) {
			t.Fatalf("project probe for %q: %d, %v, calls %d", tc.key, code, err, calls)
		}
	}
}

func TestIssueAccessRetryUsesRateLimitDeadlineOrTransportBackoff(t *testing.T) {
	now := time.Now().UTC()
	reset := now.Add(45 * time.Minute)
	for _, tc := range []struct {
		failure trackerHTTPFailure
		want    time.Time
	}{
		{trackerHTTPFailure{Status: 404}, now.Add(time.Minute)},
		{trackerHTTPFailure{Status: 404, RateLimited: true, RetryDelay: 2 * time.Minute}, now.Add(2 * time.Minute)},
		{trackerHTTPFailure{Status: 404, RateLimited: true, RetryDeadline: &reset}, reset},
	} {
		if got := issueAccessRetryAt(now, tc.failure); !got.Equal(tc.want) {
			t.Fatalf("got %v, want %v", got, tc.want)
		}
	}
}
