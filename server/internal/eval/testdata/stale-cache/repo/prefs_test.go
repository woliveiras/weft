package prefs

import "testing"

func TestMemStore(t *testing.T) {
	s := NewMemStore()
	s.Set("k", "v")
	if v, ok := s.Get("k"); !ok || v != "v" {
		t.Fatal(v, ok)
	}
	s.Delete("k")
	if _, ok := s.Get("k"); ok {
		t.Fatal("deleted key still present")
	}
}

func TestCache(t *testing.T) {
	s := NewMemStore()
	c := NewCache(s)
	c.Set("k", "v")
	if v, ok := s.Get("k"); !ok || v != "v" {
		t.Fatal("Set must write through", v, ok)
	}
	if v, ok := c.Get("k"); !ok || v != "v" {
		t.Fatal(v, ok)
	}
}

func TestSetTheme(t *testing.T) {
	svc := NewService(NewMemStore())
	if got := svc.Theme("ada"); got != DefaultTheme {
		t.Fatal(got)
	}
	if err := svc.SetTheme("ada", "dark"); err != nil {
		t.Fatal(err)
	}
	if got := svc.Theme("ada"); got != "dark" {
		t.Fatal(got)
	}
	if err := svc.SetTheme("ada", "neon"); err == nil {
		t.Fatal("unknown theme must fail")
	}
}
