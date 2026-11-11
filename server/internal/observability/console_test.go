package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
)

type failedConsoleWriter struct{}

func (failedConsoleWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }
func TestConsoleFormatAndScopedAttributes(t *testing.T) {
	var out bytes.Buffer
	base := slog.New(newConsoleHandler(&out, slog.LevelInfo)).With("instance", "fixture")
	scoped := base.WithGroup("request").With("id", "one").WithGroup("nested")
	scoped.Info("Ready\nweftd", "url", "http://localhost:8787", "unsafe", "\x1b[31m")
	base.Info("Stopped", "elapsed", "1ms")
	value := out.String()
	for _, want := range []string{"INFO", `Ready\nweftd`, "request.id = one", "request.nested.url = http://localhost:8787", `unsafe = \x1b[31m`, "elapsed = 1ms"} {
		if !strings.Contains(value, want) {
			t.Fatalf("missing %q in %q", want, value)
		}
	}
	if strings.Contains(value, "\x1b") || strings.Contains(value, "request.elapsed") {
		t.Fatal(value)
	}
	before := out.Len()
	base.Debug("hidden")
	if before != out.Len() {
		t.Fatal("debug leaked")
	}
}
func TestConsoleWriteFailure(t *testing.T) {
	h := newConsoleHandler(failedConsoleWriter{}, slog.LevelInfo)
	if err := h.Handle(t.Context(), slog.Record{}); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
}
func TestSetupSelectsConsoleWithoutChangingJSON(t *testing.T) {
	for _, format := range []string{"console", "json"} {
		t.Run(format, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "console")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			original := os.Stdout
			os.Stdout = file
			defer func() { os.Stdout = original }()
			logger, shutdown, err := Setup(context.Background(), Config{LogFormat: format, ServiceName: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			logger.Info("Weft ready", "url", "http://localhost:8787")
			if err := shutdown(t.Context()); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			if format == "json" && !strings.Contains(string(data), `"msg":"Weft ready"`) {
				t.Fatal(string(data))
			}
			if format == "console" && (strings.HasPrefix(string(data), "{") || !strings.Contains(string(data), "INFO   Weft ready")) {
				t.Fatal(string(data))
			}
		})
	}
}
