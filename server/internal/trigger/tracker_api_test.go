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
		failure := trackerCommentFailure{Status: status}
		want := status < 500 && status != 408 && status != 429
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
