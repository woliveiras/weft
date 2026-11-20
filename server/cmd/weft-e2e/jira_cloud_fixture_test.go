//go:build e2e

package main

import (
	"context"
	"net/http"
	"testing"
)

func TestCloudBrowserFixtureRequiresVerifiedScopedReadOnlyTransport(t *testing.T) {
	remote := cloudWorkflowFixture()
	defer remote.Close()
	client := &http.Client{Transport: cloudFixtureTransport{remote.URL, remote.Client().Transport}}
	base := "https://api.atlassian.com/ex/jira/" + cloudFixtureID + "/rest/api/3/issue/APP-7"
	for _, tc := range []struct {
		method, token, url string
		status             int
	}{{"GET", cloudFixtureCredential, base, 200}, {"POST", cloudFixtureCredential, base, 405}, {"GET", "wrong", base, 401}, {"GET", cloudFixtureCredential, "https://api.github.com/user", 200}, {"GET", cloudFixtureCredential, "https://external.example/", 0}} {
		req, err := http.NewRequestWithContext(context.Background(), tc.method, tc.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+tc.token)
		resp, err := client.Do(req)
		if tc.status == 0 {
			if err == nil {
				resp.Body.Close()
				t.Fatal("external destination accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatal(tc.method, resp.StatusCode, tc.status)
		}
	}
}
