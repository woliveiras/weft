// Package text has string helpers shared by the notes service.
package text

// Truncate shortens s to at most n bytes.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
