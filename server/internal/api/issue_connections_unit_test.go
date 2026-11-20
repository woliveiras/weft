package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeTrackerBodyMustBeOneKnownObject(t *testing.T) {
	for _, body := range []string{`{"token":"sensitive-fixture",`, `{"unexpected":"sensitive-fixture"}`, `{"name":"jira"} {"token":"sensitive-fixture"}`, `{"name":"jira"} trailing`} {
		req := httptest.NewRequest("POST", "/api/v1/issue-connections", strings.NewReader(body))
		if _, err := decodeIssueConnection(req); err == nil || strings.Contains(err.Error(), "sensitive-fixture") {
			t.Fatal("unsafe/invalid decode", err)
		}
	}
	req := httptest.NewRequest("POST", "/api/v1/issue-connections", strings.NewReader(`{"name":"jira","kind":"jira-dc","siteUrl":"https://jira.example","projectId":"10001","enabled":true}`))
	if in, err := decodeIssueConnection(req); err != nil || in.ProjectID != "10001" {
		t.Fatal(in.Name, err)
	}
}

func TestNativeConnectionRevisionRequired(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "invalid"} {
		req := httptest.NewRequest("PUT", "/", nil)
		req.Header.Set("If-Match", value)
		if _, err := issueConnectionRevision(req); err == nil {
			t.Fatalf("accepted revision %q", value)
		}
	}
	req := httptest.NewRequest("PUT", "/", nil)
	req.Header.Set("If-Match", `"2"`)
	if rev, err := issueConnectionRevision(req); err != nil || rev != 2 {
		t.Fatal(rev, err)
	}
}

func TestTransitionDiscoveryInputRejectsArbitraryPayloads(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"issueKey":"APP-0"}`, `{"issueKey":"../APP-7"}`, `{"issueKey":"https://jira.example/APP-7"}`, `{"issueKey":"APP-7","fields":{}}`, `{"issueKey":"APP-7"} {}`, `{"issueKey":"APP-7","token":"fixture-secret"}`, strings.Repeat(" ", 1024) + `{"issueKey":"APP-7"}`, `{"issueKey":"APP-7"}` + strings.Repeat(" ", 1024) + `{}`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if _, err := decodeTransitionIssue(r); err == nil || strings.Contains(err.Error(), "fixture-secret") {
			t.Fatal("invalid discovery body accepted", err)
		}
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"issueKey":"APP-7"}`))
	if key, err := decodeTransitionIssue(r); err != nil || key != "APP-7" {
		t.Fatal(key, err)
	}
}

func TestWorkflowMappingBodyRejectsUnreviewedFieldsAndMultipleObjects(t *testing.T) {
	for _, body := range []string{`{"name":"review","token":"fixture-secret"}`, `{"name":"review","fields":{"assignee":"human"}}`, `{"name":"review"} {}`, strings.Repeat(" ", 8193)} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if _, err := decodeWorkflowMapping(r); err == nil || strings.Contains(err.Error(), "fixture-secret") {
			t.Fatal("invalid mapping decode", err)
		}
	}
}
