package chat

import "testing"

func TestPullRequestLocatorSeparatesRepositoryAndSite(t *testing.T) {
	for _, tc := range []struct{ url, kind, repository, site string }{
		{"https://github.com/acme/app/pull/418/files", "github", "acme/app", ""},
		{"https://bitbucket.org/team/docs/pull-requests/418", "bitbucket-cloud", "team/docs", ""},
		{"https://scm.example/stash/projects/TEAM/repos/app/pull-requests/418/overview", "bitbucket-dc", "TEAM/app", "stash"},
	} {
		got, ok := ReadPRURL(tc.url)
		if !ok || got.Kind != tc.kind || got.Repository != tc.repository || got.SitePath != tc.site || got.Number != "418" {
			t.Errorf("locator for %s: %+v, %t", tc.url, got, ok)
		}
	}
	for _, input := range []PRSelector{
		{URL: "http://github.com/acme/app/pull/418"},
		{URL: "https://github.com/acme/app/pull/418", Number: "419"},
		{URL: "https://github.com/acme/app/pull/418", Repository: "acme/mismatch"},
		{URL: "https://github.com/acme%2Fapp/pull/418"},
		{URL: "https://github.com/acme/app/pull/418?access_token=synthetic"},
		{Number: "001", Repository: "acme/app"},
		{Number: "418", Repository: "acme/.."},
	} {
		if input.Valid() {
			t.Errorf("accepted ambiguous locator: %+v", input)
		}
	}
	if title := SafePRTitle("Update docs\nwith examples"); title != "Update docs with examples" {
		t.Fatalf("unexpected safe title: %q", title)
	}
	if title := SafePRTitle("password=fixture-unsafe"); title != "" {
		t.Fatal("credential-shaped title entered display")
	}
}
