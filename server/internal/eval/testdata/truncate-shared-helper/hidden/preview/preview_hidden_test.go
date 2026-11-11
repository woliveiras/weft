package preview

import (
	"testing"
	"unicode/utf8"
)

func TestHiddenTitleKeepsWholeCharacters(t *testing.T) {
	for in, want := range map[string]string{
		"プロジェクト会議の議事録と次のアクション項目":              "プロジェクト会議の議事録と次のアクション",
		"Café crème brûlée recipe for twelve": "Café crème brûlée re",
		"🎉🎉 Party planning checklist":         "🎉🎉 Party planning ch",
		"Ünïcödé":                             "Ünïcödé",
		"Meeting notes for the quarterly":     "Meeting notes for th",
	} {
		got := Title(in)
		if got != want || !utf8.ValidString(got) {
			t.Errorf("Title(%q) = %q, want %q", in, got, want)
		}
	}
}
