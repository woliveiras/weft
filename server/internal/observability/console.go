package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
)

// consoleHandler is a plain terminal view of the same structured records. It
// avoids ANSI/control sequences, keeps attributes, and shares its lock with clones.
type consoleHandler struct {
	out    io.Writer
	mu     *sync.Mutex
	level  slog.Level
	attrs  []slog.Attr
	groups []string
}

func newConsoleHandler(out io.Writer, level slog.Level) slog.Handler {
	return &consoleHandler{out: out, mu: &sync.Mutex{}, level: level}
}
func (h *consoleHandler) Enabled(_ context.Context, level slog.Level) bool { return level >= h.level }
func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	copy := *h
	copy.attrs = append(append([]slog.Attr{}, h.attrs...), h.scoped(attrs)...)
	return &copy
}
func (h *consoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	copy := *h
	copy.groups = append(append([]string{}, h.groups...), name)
	return &copy
}
func (h *consoleHandler) scoped(attrs []slog.Attr) []slog.Attr {
	for i := len(h.groups) - 1; i >= 0; i-- {
		attrs = []slog.Attr{{Key: h.groups[i], Value: slog.GroupValue(attrs...)}}
	}
	return attrs
}
func consoleText(s string) string { quoted := strconv.Quote(s); return quoted[1 : len(quoted)-1] }
func (h *consoleHandler) Handle(_ context.Context, record slog.Record) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %-5s  %s\n", record.Time.Format("15:04:05"), record.Level, consoleText(record.Message))
	var attribute func(string, slog.Attr)
	attribute = func(prefix string, a slog.Attr) {
		a.Value = a.Value.Resolve()
		if a.Equal(slog.Attr{}) {
			return
		}
		if a.Value.Kind() == slog.KindGroup {
			if a.Key != "" {
				prefix += a.Key + "."
			}
			for _, child := range a.Value.Group() {
				attribute(prefix, child)
			}
			return
		}
		fmt.Fprintf(&b, "         %s = %s\n", consoleText(prefix+a.Key), consoleText(a.Value.String()))
	}
	for _, a := range h.attrs {
		attribute("", a)
	}
	var attrs []slog.Attr
	record.Attrs(func(a slog.Attr) bool { attrs = append(attrs, a); return true })
	for _, a := range h.scoped(attrs) {
		attribute("", a)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, b.String())
	return err
}
