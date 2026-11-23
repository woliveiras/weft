package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/tools"
)

func TestJiraSearchDiagnosticTypesAndBounds(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"", "missing"}, {"null", "null"}, {"{", "invalid_json"},
		{`[]`, "array"}, {`"private"`, "string"}, {`42`, "number"}, {`true`, "boolean"},
	} {
		if got := jiraSearchJSONType(json.RawMessage(tc.raw)); got != tc.want {
			t.Fatalf("shape type = %s, want %s", got, tc.want)
		}
	}
	blocks := make([]any, 100)
	for i := range blocks {
		blocks[i] = map[string]any{"type": "private-block-type", "text": "private body"}
	}
	raw, err := json.Marshal(map[string]any{"private-key": "private-value", "issues": "private issue", "content": blocks, "nextPageToken": "private cursor"})
	if err != nil {
		t.Fatal(err)
	}
	shape := jiraSearchShape(raw)
	if !strings.Contains(shape, "issues=string") || !strings.Contains(shape, "contentCount=10+") || !strings.Contains(shape, "block2=other") || strings.Contains(shape, "block3=") || strings.Contains(shape, "private") || len(shape) > 400 {
		t.Fatalf("unbounded or unsafe shape: %s", shape)
	}
}

func TestJiraSearchFormatDiagnosticsNeverIncludeProviderContent(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"plain text", `{"content":[{"type":"text","text":"private-provider-text"}]}`, "stage=payload;"},
		{"structured shape", `{"structuredContent":{"results":[],"data":{"private-key":"private-provider-text"}},"content":[]}`, "stage=data;"},
		{"wrong issues type", `{"issues":"private-provider-text"}`, "stage=envelope; envelope=object,issues=string"},
		{"wrong content type", `{"content":"private-provider-text"}`, "stage=envelope;"},
		{"multiple blocks", `{"content":[{"type":"text","text":"private-provider-text"},{"type":"image","data":"private-provider-text"}]}`, "contentCount=2,block0=text,block1=image"},
		{"malformed", `{"private-key":"private-provider-text"`, "envelope=invalid_json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeJiraSearch(&tools.Result{Data: json.RawMessage(tc.raw)}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "private") || len(err.Error()) > 850 {
				t.Fatalf("unsafe/missing format diagnostic: %v", err)
			}
		})
	}
	dependency := errors.New("fixture dependency failure")
	if _, err := decodeJiraSearch(&tools.Result{Data: json.RawMessage(`{}`)}, dependency); !errors.Is(err, dependency) {
		t.Fatal("dependency failure replaced by a format diagnostic", err)
	}
	if _, err := decodeJiraSearch(&tools.Result{IsError: true, Content: "private-provider-text"}, nil); err == nil || err.Error() != "tracker operation failed" {
		t.Fatal("tool failure or provider content exposed", err)
	}
}

func TestJiraSearchDataWrapperIsSingleStrictLayer(t *testing.T) {
	const issue = `{"id":"10007","key":"APP-7","self":"https://team.atlassian.net/rest/api/3/issue/10007","fields":{"summary":"Example","description":"Details","labels":["weft"],"project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":"10","statusCategory":{"key":"new"}},"updated":"2026-11-23T12:00:00Z","reporter":{"accountId":"human"},"issuelinks":[]}}`
	for _, envelope := range []string{
		`{"data":{"issues":[` + issue + `],"nextPageToken":"next"}}`,
		`{"structuredContent":{"data":{"issues":[` + issue + `],"nextPageToken":"next"}},"content":[]}`,
		`{"content":[{"type":"text","text":` + strconv.Quote(`{"data":{"issues":[`+issue+`],"nextPageToken":"next"}}`) + `}]}`,
	} {
		reply, err := decodeJiraSearch(&tools.Result{Data: json.RawMessage(envelope)}, nil)
		if err != nil || len(reply.Issues) != 1 || reply.Next != "next" {
			t.Fatalf("wrapped search rejected: %+v %v", reply, err)
		}
		raw := reply.Issues[0]
		if raw.ID != "10007" || raw.Fields.Project.ID != "10001" || raw.Fields.IssueType.ID != "10002" || raw.Fields.Status.ID != "10" || raw.Fields.Updated == "" || raw.Fields.Reporter.AccountID != "human" || raw.Fields.IssueLinks == nil {
			t.Fatal("wrapper lost scope or readiness metadata")
		}
		if err := validateCloudIssue(cloudSource(), raw); err != nil {
			t.Fatal(err)
		}
		for _, clear := range []func(*jiraIssue){
			func(v *jiraIssue) { v.ID = "" }, func(v *jiraIssue) { v.Fields.Project.ID = "" },
			func(v *jiraIssue) { v.Fields.IssueType.ID = "" }, func(v *jiraIssue) { v.Fields.Status.ID = "" },
			func(v *jiraIssue) { v.Fields.Status.StatusCategory.Key = "" },
		} {
			incomplete := raw
			clear(&incomplete)
			encoded, marshalErr := json.Marshal(map[string]any{"data": map[string]any{"issues": []jiraIssue{incomplete}}})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			rows, searchErr := (jira{c: cloudSource()}).Search(t.Context(), func(context.Context, string, any) (*tools.Result, error) { return &tools.Result{Data: encoded}, nil }, "weft")
			if searchErr == nil || rows != nil {
				t.Fatal("incomplete wrapped issue admitted", searchErr)
			}
		}
	}
	for _, payload := range []string{`{"data":{"issues":[]}}`, `{"data":{"issues":[],"nextPageToken":"next"}}`} {
		reply, err := decodeJiraSearch(&tools.Result{Content: payload}, nil)
		if err != nil || reply.Issues == nil || len(reply.Issues) != 0 {
			t.Fatalf("empty wrapped search rejected: %+v %v", reply, err)
		}
	}
	for _, payload := range []string{
		`{"data":null}`, `{"data":[]}`, `{"data":"private-provider-text"}`, `{"data":{}}`,
		`{"data":{"issues":null}}`, `{"data":{"issues":{}}}`, `{"data":{"issues":"private-provider-text"}}`,
		`{"data":{"issues":["private-provider-text"]}}`, `{"data":{"data":{"issues":[]}}}`,
		`{"issues":[],"data":{"issues":[]}}`,
		`{"nextPageToken":"outer-cursor","data":{"issues":[]}}`,
		`{"nextPageToken":"outer-cursor","structuredContent":{"data":{"issues":[]}},"content":[]}`,
		`{"nextPageToken":"outer-cursor","content":[{"type":"text","text":"{\"data\":{\"issues\":[]}}"}]}`,
		`{"structuredContent":{"data":null},"content":[{"type":"text","text":"{\"issues\":[]}"}]}`,
	} {
		if reply, err := decodeJiraSearch(&tools.Result{Content: payload}, nil); err == nil || reply.Issues != nil {
			t.Fatalf("malformed wrapper admitted: %+v %v", reply, err)
		}
	}
	for _, payload := range []string{
		`{"isError":true,"data":{"issues":[]}}`,
		`{"error":"private-provider-text","data":{"issues":[]}}`,
		`{"structuredContent":{"isError":true,"data":{"issues":[]}},"content":[]}`,
		`{"data":{"issues":[],"isError":true}}`,
		`{"data":{"issues":[],"error":{"private-key":"private-provider-text"}}}`,
	} {
		if _, err := decodeJiraSearch(&tools.Result{Content: payload}, nil); err == nil || err.Error() != "tracker operation failed" {
			t.Fatal("explicit failed result accepted or exposed", err)
		}
	}
}
