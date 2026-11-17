package observability

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config for Setup.
type Config struct {
	ServiceName    string
	ServiceVersion string
	Instance       string
	// OTLPEndpoint is the OTLP/HTTP base URL (http://otel-collector:4318).
	// Empty disables trace and log export (metrics are always scraped).
	OTLPEndpoint string
	SampleRatio  float64
	LogLevel     string
	LogFormat    string
}

// Setup installs the global tracer provider and returns the process logger
// and a shutdown function that flushes exporters.
//
// Logs go to stdout always (12-factor; any collector can ship them) and,
// when an OTLP endpoint is configured, also over OTLP so they land in Loki
// correlated with traces by trace id.
func Setup(ctx context.Context, c Config) (*slog.Logger, func(context.Context) error, error) {
	var level slog.Level
	_ = level.UnmarshalText([]byte(strings.ToUpper(orDefault(c.LogLevel, "info"))))
	var stdout slog.Handler
	opts := &slog.HandlerOptions{Level: level}
	if c.LogFormat == "console" {
		stdout = newConsoleHandler(os.Stdout, level)
	} else if c.LogFormat == "text" {
		stdout = slog.NewTextHandler(os.Stdout, opts)
	} else {
		stdout = slog.NewJSONHandler(os.Stdout, opts)
	}
	var shutdowns []func(context.Context) error
	shutdown := func(ctx context.Context) error {
		var errs []error
		for _, s := range shutdowns {
			errs = append(errs, s(ctx))
		}
		return errors.Join(errs...)
	}

	if c.OTLPEndpoint == "" {
		return slog.New(stdout).With("service", c.ServiceName, "instance", c.Instance), shutdown, nil
	}

	// Schemaless attributes merge with the SDK default resource whatever
	// semconv version the SDK was built with.
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(c.ServiceName),
		semconv.ServiceVersion(orDefault(c.ServiceVersion, "dev")),
		semconv.ServiceInstanceID(c.Instance),
	))
	if err != nil {
		return nil, nil, err
	}
	endpoint := strings.TrimRight(c.OTLPEndpoint, "/")
	insecure := strings.HasPrefix(endpoint, "http://")
	host := strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")

	topts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(host)}
	if insecure {
		topts = append(topts, otlptracehttp.WithInsecure())
	}
	texp, err := otlptracehttp.New(ctx, topts...)
	if err != nil {
		return nil, nil, err
	}
	ratio := c.SampleRatio
	if ratio <= 0 || ratio > 1 {
		ratio = 1
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(texp, sdktrace.WithBatchTimeout(2*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	)
	otel.SetTracerProvider(tp)
	shutdowns = append(shutdowns, tp.Shutdown)

	lopts := []otlploghttp.Option{otlploghttp.WithEndpoint(host)}
	if insecure {
		lopts = append(lopts, otlploghttp.WithInsecure())
	}
	lexp, err := otlploghttp.New(ctx, lopts...)
	if err != nil {
		return nil, nil, err
	}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(lexp)), sdklog.WithResource(res))
	shutdowns = append(shutdowns, lp.Shutdown)
	otelHandler := otelslog.NewHandler(TracerName, otelslog.WithLoggerProvider(lp))

	logger := slog.New(fanout{stdout, otelHandler, level}).With("service", c.ServiceName, "instance", c.Instance)
	// Export failures (collector down) go to stdout only - never back into the
	// OTLP log pipeline that is failing - and at most once a minute.
	errLog := slog.New(stdout)
	var lastErr time.Time
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		if time.Since(lastErr) > time.Minute {
			lastErr = time.Now()
			errLog.Warn("telemetry export failing (suppressed for 1m)", "err", err)
		}
	}))
	return logger, shutdown, nil
}

// fanout sends records to two handlers.
type fanout struct {
	a, b  slog.Handler
	level slog.Level
}

func (f fanout) Enabled(_ context.Context, l slog.Level) bool { return l >= f.level }
func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	err1 := f.a.Handle(ctx, r.Clone())
	err2 := f.b.Handle(ctx, r)
	return errors.Join(err1, err2)
}
func (f fanout) WithAttrs(as []slog.Attr) slog.Handler {
	return fanout{f.a.WithAttrs(as), f.b.WithAttrs(as), f.level}
}
func (f fanout) WithGroup(n string) slog.Handler {
	return fanout{f.a.WithGroup(n), f.b.WithGroup(n), f.level}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
