package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudProfileInputRejectsExtraUnknownAndOversizedBodies(t *testing.T) {
	for _, body := range []string{`{`, `{} {}`, `{"token":"should-not-be-a-profile-secret"}`, strings.Repeat(" ", 4097)} {
		if _, err := decodeCloudProfile(httptest.NewRequest("POST", "/", strings.NewReader(body))); err == nil {
			t.Fatal("accepted invalid profile body")
		}
	}
	in, err := decodeCloudProfile(httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"cloud","mcpConnection":"jira","mcpRevision":1,"cloudId":"11111111-2222-3333-4444-555555555555","siteUrl":"https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555","projectId":"10001","enabled":true,"projectRestricted":true}`)))
	if err != nil || in.Name != "cloud" || !in.ProjectRestricted {
		t.Fatal(in, err)
	}
}
