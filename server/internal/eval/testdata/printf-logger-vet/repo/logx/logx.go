// Package logx is the job runner's small line logger.
package logx

import (
	"fmt"
	"io"
	"sync"
)

// Logger writes prefixed lines to a writer. Lines from concurrent callers
// never interleave.
type Logger struct {
	mu     sync.Mutex
	w      io.Writer
	prefix string
}

// New returns a Logger that writes to w and starts every line with prefix.
func New(w io.Writer, prefix string) Logger {
	return Logger{w: w, prefix: prefix}
}

// Log writes the prefix and args separated by spaces, as one line.
func (l Logger) Log(args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintln(l.w, append([]any{l.prefix}, args...)...)
}
