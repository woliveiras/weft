package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TracerName is the instrumentation scope for all platform spans.
const TracerName = "github.com/woliveiras/weft"

// Tracer returns the platform tracer (a no-op until Setup installs a provider).
func Tracer() trace.Tracer { return otel.Tracer(TracerName) }

// Start starts a span with attributes; callers must End it.
func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, trace.WithAttributes(attrs...))
}

// EndSpan records err (if any) and ends the span.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// TraceID of the span in ctx, or "".
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}

// mapCarrier adapts message metadata to the W3C propagator.
type mapCarrier map[string]any

func (c mapCarrier) Get(k string) string {
	if v, ok := c[k].(string); ok {
		return v
	}
	return ""
}
func (c mapCarrier) Set(k, v string) { c[k] = v }
func (c mapCarrier) Keys() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	return out
}

var propagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

// Inject writes the trace context of ctx into message metadata
// ("traceparent"/"tracestate"), so a trace follows a message across agents,
// processes and brokers.
func Inject(ctx context.Context, meta map[string]any) {
	propagator.Inject(ctx, mapCarrier(meta))
}

// Extract returns ctx carrying the remote span context found in metadata.
func Extract(ctx context.Context, meta map[string]any) context.Context {
	if meta == nil {
		return ctx
	}
	return propagator.Extract(ctx, mapCarrier(meta))
}
