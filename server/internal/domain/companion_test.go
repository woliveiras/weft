package domain

import "testing"

func TestCompanionValidation(t *testing.T) {
	good := Companion{Name: "cache", Image: "redis:7", Command: []string{"redis-server"}}
	if err := ValidateCompanions([]Companion{good}); err != nil {
		t.Fatal(err)
	}
	for _, items := range [][]Companion{
		{{Name: "../escape", Image: "alpine"}},
		{{Name: "cache", Image: "--privileged"}},
		{{Name: "cache", Image: "${IMAGE}"}},
		{{Name: "cache", Image: "alpine", Command: []string{"${COMMAND}"}}},
		{good, good},
	} {
		if err := ValidateCompanions(items); err == nil {
			t.Fatal(items)
		}
	}
}
