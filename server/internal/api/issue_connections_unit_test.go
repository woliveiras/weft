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
