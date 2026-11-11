package workspacejournal

import "time"

const (
	unresolvedKernelPath = "unresolved_kernel_path"
	gapWindowDuration    = 10 * time.Second
)

type gapWindow struct {
	Count uint64    `json:"count"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// unresolvedGaps is owned by the capture loop. Startup and queue overflow
// bypass it and are persisted immediately.
type unresolvedGaps struct {
	window   gapWindow
	deadline time.Time
}

func (g *unresolvedGaps) observe(now time.Time, appendRecord func(Record) error) error {
	if err := g.flushExpired(now, appendRecord); err != nil {
		return err
	}
	wall := now.UTC()
	if g.window.Count == 0 {
		g.deadline = now.Add(gapWindowDuration)
		g.window = gapWindow{Count: 1, Start: wall, End: wall}
		return nil
	}
	g.window.Count++
	// Deadlines use the monotonic clock. Persist the observed wall-clock
	// range even if the system clock stepped during the window.
	if wall.Before(g.window.Start) {
		g.window.Start = wall
	}
	if wall.After(g.window.End) {
		g.window.End = wall
	}
	return nil
}

func (g *unresolvedGaps) flushExpired(now time.Time, appendRecord func(Record) error) error {
	if now.Before(g.deadline) {
		return nil
	}
	return g.flush(appendRecord)
}

func (g *unresolvedGaps) flush(appendRecord func(Record) error) error {
	if g.window.Count == 0 {
		return nil
	}
	window := g.window
	if err := appendRecord(Record{Gap: unresolvedKernelPath, Time: window.End, GapWindow: &window}); err != nil {
		return err
	}
	*g = unresolvedGaps{}
	return nil
}
