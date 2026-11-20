package scm

import (
	"strings"
	"testing"
)

func TestReviewSnapshotValidation(t *testing.T) {
	valid := ReviewSnapshot{Number: 1, URL: "https://scm.example/pr/1", State: "open", Head: "weft/task", Base: "main", HeadSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40)}
	if err := valid.validate(1); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ReviewSnapshot){
		func(s *ReviewSnapshot) { s.Number = 2 }, func(s *ReviewSnapshot) { s.HeadSHA = "short" }, func(s *ReviewSnapshot) { s.BaseSHA = "../path" }, func(s *ReviewSnapshot) { s.State = "unknown" }, func(s *ReviewSnapshot) { s.URL = "javascript:alert(1)" }, func(s *ReviewSnapshot) { s.Head = "--flag" },
	} {
		s := valid
		change(&s)
		if s.validate(1) == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
}

func TestSnapshotRedactionBeforeJSONEncoding(t *testing.T) {
	token := `fixture-"secret`
	s := redactSnapshot(ReviewSnapshot{Title: token, Body: token, URL: "https://scm.example/" + token}, token)
	if strings.Contains(s.Title, token) || strings.Contains(s.Body, token) || strings.Contains(s.URL, token) {
		t.Fatal("credential reflected in snapshot")
	}
}

func TestNestedDiffTruncation(t *testing.T) {
	for _, flag := range []any{true, "true", "unknown", nil} {
		if !truncatedDiff(map[string]any{"diffs": []any{map[string]any{"hunks": []any{map[string]any{"truncated": flag}}}}}) {
			t.Fatalf("accepted truncation %v", flag)
		}
	}
	for _, flag := range []any{false, "false"} {
		if truncatedDiff(map[string]any{"truncated": flag}) {
			t.Fatalf("rejected complete flag %v", flag)
		}
	}
}
