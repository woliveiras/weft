// Package dur parses short durations such as "30s", "10m" and "2h".
package dur

import (
	"fmt"
	"strconv"
	"time"
)

// Parse reads a number followed by a unit: s (seconds), m (minutes) or h (hours).
func Parse(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("dur: %q is too short", s)
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return 0, fmt.Errorf("dur: %q: %w", s, err)
	}
	switch s[len(s)-1] {
	case 's', 'm':
		return time.Duration(n) * time.Second, nil
	case 'h':
		return time.Duration(n) * time.Hour, nil
	}
	return 0, fmt.Errorf("dur: unknown unit in %q", s)
}
