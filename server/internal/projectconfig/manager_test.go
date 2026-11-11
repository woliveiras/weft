package projectconfig

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	if err := Validate(Input{Name: "Project", Path: filepath.Join(t.TempDir(), "repo")}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []Input{{Name: "", Path: "/repo"}, {Name: "x", Path: "relative"}, {Name: strings.Repeat("x", 201), Path: "/repo"}, {Name: "x", Path: "/repo\x00"}} {
		if Validate(in) == nil {
			t.Fatal(in)
		}
	}
}

func TestExecutionPreferenceValidation(t *testing.T) {
	for _, mode := range []string{"workflow", "local", "sandbox"} {
		if !ValidExecutionPreference(mode) {
			t.Fatal(mode)
		}
	}
	for _, mode := range []string{"", "host", "LOCAL", "local;run"} {
		if ValidExecutionPreference(mode) {
			t.Fatal(mode)
		}
	}
}
