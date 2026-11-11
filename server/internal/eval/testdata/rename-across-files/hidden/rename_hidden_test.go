package users

import "testing"

func TestUserNameHidden(t *testing.T) {
	if got := UserName(User{"Grace", "Hopper"}); got != "Grace Hopper" {
		t.Fatal(got)
	}
	if got := Report([]User{{"A", "B"}, {"C", "D"}}); got != "A B\nC D" {
		t.Fatal(got)
	}
}
