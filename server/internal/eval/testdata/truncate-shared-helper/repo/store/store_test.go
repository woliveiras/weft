package store

import "testing"

func TestSave(t *testing.T) {
	b := NewBackend()
	title := "A very long title that does not fit in a storage key"
	if err := Save(b, title, "body"); err != nil {
		t.Fatal(err)
	}
	if v, ok := b.Get(Key(title)); !ok || v != "body" {
		t.Fatal(v, ok)
	}
	if err := b.Put("this key is far longer than thirty-two bytes", "x"); err == nil {
		t.Fatal("long keys must be rejected")
	}
}
