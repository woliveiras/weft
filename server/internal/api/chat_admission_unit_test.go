package api

import "testing"

func TestChatAdmissionReleasesPerOwnerAndGlobalCapacity(t *testing.T) {
	var a chatAdmission
	if a.acquire("") || !a.acquire("one") || !a.acquire("one") || a.acquire("one") {
		t.Fatal("owner capacity")
	}
	if !a.acquire("two") || !a.acquire("three") || a.acquire("four") {
		t.Fatal("global capacity")
	}
	a.release("two")
	if !a.acquire("four") {
		t.Fatal("capacity lost")
	}
	for _, owner := range []string{"one", "one", "three", "four"} {
		a.release(owner)
	}
	if len(a.active) != 0 || a.total != 0 {
		t.Fatal("idle owners retained")
	}
}
