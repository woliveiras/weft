package release

import "testing"

func TestLatest(t *testing.T) {
	if got := Latest([]string{"v1.2.0", "v1.3.1", "v1.3.0"}); got != "v1.3.1" {
		t.Fatal(got)
	}
	if got := Latest(nil); got != "" {
		t.Fatal(got)
	}
}

func TestCompare(t *testing.T) {
	if Compare("v1.2.3", "v1.2.3") != 0 || Compare("v1.2.3", "v1.2.4") != -1 || Compare("v2.0.0", "v1.9.9") != 1 {
		t.Fatal("basic comparisons")
	}
}
