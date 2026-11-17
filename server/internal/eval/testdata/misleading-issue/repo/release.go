// Package release picks releases by their version tags.
package release

import "strings"

// Compare compares two version tags such as "v1.2.3" or "1.4" and returns
// -1 when a is older than b, 0 when they are the same version and +1 when a
// is newer.
func Compare(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(a, "v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(pa) < len(pb):
		return -1
	case len(pa) > len(pb):
		return 1
	}
	return 0
}

// Latest returns the newest of tags, or "" when there are none.
func Latest(tags []string) string {
	latest := ""
	for _, t := range tags {
		if latest == "" || Compare(t, latest) > 0 {
			latest = t
		}
	}
	return latest
}
