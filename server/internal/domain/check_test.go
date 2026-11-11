package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidateChecks(t *testing.T) {
	ok := []Check{{Name: "test", Command: "go test ./..."}, {Name: "web-lint", Command: "npm run lint", Cwd: "web", TimeoutSec: 1800}}
	if err := ValidateChecks(ok); err != nil {
		t.Fatalf("valid checks rejected: %v", err)
	}
	if err := ValidateChecks(nil); err != nil {
		t.Fatalf("no checks is valid: %v", err)
	}
	many := make([]Check, MaxChecks+1)
	for i := range many {
		many[i] = Check{Name: "c" + strings.Repeat("x", i), Command: "true"}
	}
	for name, tc := range map[string]struct {
		checks []Check
		want   string
	}{
		"bad name":      {[]Check{{Name: "Test", Command: "x"}}, "checks[0].name"},
		"empty name":    {[]Check{{Command: "x"}}, "checks[0].name"},
		"duplicate":     {[]Check{{Name: "a", Command: "x"}, {Name: "a", Command: "y"}}, "duplicate"},
		"empty command": {[]Check{{Name: "a", Command: "  "}}, "checks[0].command"},
		"multi-line":    {[]Check{{Name: "a", Command: "x\ny"}}, "checks[0].command"},
		"long command":  {[]Check{{Name: "a", Command: strings.Repeat("x", 1025)}}, "checks[0].command"},
		"bad cwd":       {[]Check{{Name: "a", Command: "x", Cwd: "a\x00"}}, "checks[0].cwd"},
		"timeout high":  {[]Check{{Name: "a", Command: "x", TimeoutSec: MaxCheckTimeout + 1}}, "timeoutSec"},
		"timeout neg":   {[]Check{{Name: "a", Command: "x", TimeoutSec: -1}}, "timeoutSec"},
		"too many":      {many, "at most"},
	} {
		err := ValidateChecks(tc.checks)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want error containing %q", name, err, tc.want)
		}
	}
}

func TestMergeChecksWorkflowOverridesProject(t *testing.T) {
	project := []Check{{Name: "test", Command: "make test"}, {Name: "lint", Command: "make lint"}}
	workflow := []Check{{Name: "build", Command: "go build ./..."}, {Name: "test", Command: "go test ./..."}}
	got := MergeChecks(project, workflow)
	want := []Check{{Name: "test", Command: "go test ./..."}, {Name: "lint", Command: "make lint"}, {Name: "build", Command: "go build ./..."}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge = %+v, want %+v", got, want)
	}
	if got := MergeChecks(nil, workflow); !reflect.DeepEqual(got, workflow) {
		t.Fatalf("without project checks the workflow's are kept: %+v", got)
	}
	if got := MergeChecks(project, nil); !reflect.DeepEqual(got, project) {
		t.Fatalf("without workflow checks the project's are kept: %+v", got)
	}
}
