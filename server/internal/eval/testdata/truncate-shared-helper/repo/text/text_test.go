package text

import "testing"

func TestTruncate(t *testing.T) {
	if got := Truncate("hello world", 5); got != "hello" {
		t.Fatal(got)
	}
	if got := Truncate("hi", 5); got != "hi" {
		t.Fatal(got)
	}
}
