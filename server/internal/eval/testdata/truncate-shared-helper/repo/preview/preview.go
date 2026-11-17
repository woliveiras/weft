// Package preview renders note titles for the notes list.
package preview

import (
	"strings"

	"example.com/notes/text"
)

// MaxTitle is how many characters of a title the notes list shows.
const MaxTitle = 20

// Title returns the part of a note title the notes list shows.
func Title(title string) string {
	return text.Truncate(strings.TrimSpace(title), MaxTitle)
}
