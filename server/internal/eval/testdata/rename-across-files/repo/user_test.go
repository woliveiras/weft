package users

import "testing"

func TestGreeting(t *testing.T) {
	if got := Greeting(User{"Ada", "Lovelace"}); got != "Hello, Ada Lovelace!" {
		t.Fatal(got)
	}
}
