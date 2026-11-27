package workspace

import (
	"fmt"
	"strings"
	"testing"
)

func TestDecodeBranchPublication(t *testing.T) {
	for _, n := range []int{40, 64} {
		id := strings.Repeat("a", n)
		for _, prev := range []string{"", strings.Repeat("b", n)} {
			req, err := DecodeBranchPublication(strings.NewReader(fmt.Sprintf(`{"commit":%q,"previous":%q}`, id, prev)))
			if err != nil || req != (BranchPublication{id, prev}) {
				t.Fatalf("valid request %+v: %v", req, err)
			}
		}
	}
	id := strings.Repeat("a", 40)
	for _, bad := range []string{"", "null", "[]", "{}", `{"commit":"` + id + `"}`, `{"commit":"` + id + `","previous":null}`, `{"commit":"HEAD","previous":""}`, `{"commit":"` + id + `","previous":"main"}`, `{"commit":"` + id + `","previous":"` + strings.Repeat("b", 64) + `"}`, `{"commit":"` + id + `","previous":""} {}`} {
		if _, err := DecodeBranchPublication(strings.NewReader(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
