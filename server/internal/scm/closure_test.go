package scm

import "testing"

func TestAppendClosingIssue(t *testing.T) {
	if got := appendClosingIssue("", "ACME/App", "acme/app#42"); got != "Closes #42" {
		t.Fatal(got)
	}
	if got := appendClosingIssue(" Summary  ", "acme/app", "acme/tracker#42"); got != "Summary\n\nCloses acme/tracker#42" {
		t.Fatal(got)
	}
}
