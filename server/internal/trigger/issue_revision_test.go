package trigger

import "testing"

func TestGitHubSearchRevisionComparesInstants(t *testing.T) {
	const rest = "2026-11-12T10:00:00Z"
	for _, tc := range []struct {
		search string
		match  bool
	}{
		{"2026-11-12T11:00:00.000+01:00", true},
		{"2026-11-12T09:59:59.999Z", false},
		{"2026-11-12T10:00:01Z", false},
		{"", false},
		{"invalid", false},
	} {
		if got := sameGitHubRevision(rest, tc.search); got != tc.match {
			t.Fatalf("%q: got %v, want %v", tc.search, got, tc.match)
		}
	}
}
