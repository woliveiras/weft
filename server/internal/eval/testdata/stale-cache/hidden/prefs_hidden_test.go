package prefs

import "testing"

// countingStore counts the reads that reach the backing store.
type countingStore struct {
	*MemStore
	gets int
}

func (s *countingStore) Get(key string) (string, bool) {
	s.gets++
	return s.MemStore.Get(key)
}

func TestHiddenThemeChangesAreVisible(t *testing.T) {
	store := &countingStore{MemStore: NewMemStore()}
	svc := NewService(store)
	steps := []struct {
		set, reset bool
		theme      string
		want       string
	}{
		{want: DefaultTheme},
		{set: true, theme: "dark", want: "dark"},
		{set: true, theme: "contrast", want: "contrast"},
		{reset: true, want: DefaultTheme},
		{set: true, theme: "dark", want: "dark"},
	}
	for i, st := range steps {
		if st.set {
			if err := svc.SetTheme("ada", st.theme); err != nil {
				t.Fatal(err)
			}
		}
		if st.reset {
			svc.ResetTheme("ada")
		}
		if got := svc.Theme("ada"); got != st.want {
			t.Fatalf("step %d: Theme = %q, want %q", i, got, st.want)
		}
	}
	if v, ok := store.Get("theme/ada"); !ok || v != "dark" {
		t.Fatalf("store holds %q %v, want dark", v, ok)
	}
	if got := svc.Theme("bob"); got != DefaultTheme {
		t.Fatalf("another user: %q", got)
	}
}

func TestHiddenThemeReadsStayCached(t *testing.T) {
	store := &countingStore{MemStore: NewMemStore()}
	svc := NewService(store)
	if err := svc.SetTheme("ada", "dark"); err != nil {
		t.Fatal(err)
	}
	store.gets = 0
	for i := 0; i < 5; i++ {
		if got := svc.Theme("ada"); got != "dark" {
			t.Fatal(got)
		}
	}
	if store.gets > 1 {
		t.Fatalf("%d reads reached the store; the cache must serve repeated reads", store.gets)
	}
}
