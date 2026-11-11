package store

import (
	"strings"
	"testing"
)

func TestHiddenSaveNonASCIITitles(t *testing.T) {
	b := NewBackend()
	for _, title := range []string{
		"プロジェクト会議の議事録と次のアクション項目",
		"Café crème brûlée recipe for twelve",
		"🎉🎉 Party planning checklist for the office",
		"A plain ASCII title that is longer than the key limit",
	} {
		if err := Save(b, title, "body of "+title); err != nil {
			t.Errorf("Save(%q): %v", title, err)
			continue
		}
		k := Key(title)
		if len(k) > MaxKeyBytes || len(k) < MaxKeyBytes-3 || !strings.HasPrefix(strings.ToLower(title), k) {
			t.Errorf("Key(%q) = %q (%d bytes)", title, k, len(k))
		}
		if v, ok := b.Get(k); !ok || v != "body of "+title {
			t.Errorf("Get(%q) = %q %v", k, v, ok)
		}
	}
}
