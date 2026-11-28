package api

import "testing"

func TestCheckoutDestinationMustBeExplicitAndUnambiguous(t *testing.T) {
	for _, tc := range []struct {
		input, connection, branch string
		invalid                   bool
	}{
		{"replace docs", "", "", false},
		{"Replace docs using connection=docs-v2 branch=release/v2", "docs-v2", "release/v2", false},
		{"connection=a branch=", "", "", true},
		{"branch=main", "", "", true},
		{"connection=a branch=..", "", "", true},
		{"connection=a branch=main connection=b", "", "", true},
		{"connection=a branch=main branch=other", "", "", true},
	} {
		connection, branch, prompt := checkoutDestinationChoice(tc.input)
		if connection != tc.connection || branch != tc.branch || (prompt != "") != tc.invalid {
			t.Errorf("%q: connection %q branch %q prompt %q", tc.input, connection, branch, prompt)
		}
	}
}
