package scm

import (
	"strings"
	"testing"
)

func TestReportedChecksNeverInferRequiredRulesOrUnknownSuccess(t *testing.T) {
	for _, tc := range []struct{ state, want string }{{"SUCCESSFUL", "passed"}, {"success", "passed"}, {"FAILED", "failed"}, {"INPROGRESS", "pending"}, {"neutral", "unknown"}, {"skipped", "unknown"}, {"NEW_VENDOR_STATE", "unknown"}, {"", "unknown"}} {
		out, err := checkEntry("build", tc.state)
		if err != nil || out.State != tc.want {
			t.Fatalf("%s: %+v %v", tc.state, out, err)
		}
	}
	if _, err := checkEntry("", "success"); err == nil {
		t.Fatal("accepted missing context")
	}
	if _, err := fileEntry("", "", "modified"); err == nil {
		t.Fatal("accepted missing path")
	}
	if _, err := fileEntry("a\x00b", "", "modified"); err == nil {
		t.Fatal("accepted invalid path")
	}
	if _, err := fileEntry(strings.Repeat("x", 4097), "", "modified"); err == nil {
		t.Fatal("accepted excessive path")
	}
	f, err := fileEntry("new.go", "old.go", "renamed")
	if err != nil || f.PreviousPath != "old.go" {
		t.Fatal(f, err)
	}
}

func TestEvidencePaginationPreservesComparisonAndUnfilteredChecks(t *testing.T) {
	for _, tc := range []struct {
		path      string
		topic, ok bool
	}{
		{"/diffstat/a..b?topic=true&page=2&pagelen=100", true, true},
		{"/checks?page=2", false, true},
		{"/diffstat/a..b?topic=false&page=2", true, false},
		{"/diffstat/a..b?page=2", true, false},
		{"/diffstat/a..b?topic=true&path=only.go", true, false},
		{"/checks?q=state=SUCCESSFUL", false, false},
		{"/checks?pagelen=1000", false, false},
		{"/checks?page=1&page=2", false, false},
	} {
		if err := reviewPageQuery(tc.path, tc.topic); (err == nil) != tc.ok {
			t.Fatal(tc, err)
		}
	}
}
