package instructions

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestGuidanceLoadTelemetryUsesBoundedLabels(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	cases := []struct{ stage, outcome string }{
		{"catalogue", "loaded"},
		{"body", "loaded"},
		{"body", "omitted"},
	}
	sampleCount := func(stage, outcome string) uint64 {
		t.Helper()
		var result dto.Metric
		metric := observability.GuidanceLoadSeconds.WithLabelValues(stage, outcome).(prometheus.Metric)
		if err := metric.Write(&result); err != nil {
			t.Fatal(err)
		}
		return result.GetHistogram().GetSampleCount()
	}
	before := make([]uint64, len(cases))
	for i, c := range cases {
		before[i] = sampleCount(c.stage, c.outcome)
	}

	root := t.TempDir()
	writeGuidance(t, root, ".agents/skills/sample/SKILL.md", "---\nname: sample\ndescription: Sample skill\n---\nbody")
	g := &domain.Graph{Spec: domain.GraphSpec{Instructions: domain.InstructionRefs{RepositoryGuidance: &domain.RepositoryGuidancePolicy{}}}}
	m := manager(t)
	ctx, parent := observability.Start(t.Context(), "turn")
	defer parent.End()
	snapshot, session, err := m.Prepare(ctx, g, &domain.Node{ID: "worker"}, root, root, "task", nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ExecutionID = "guidance-metrics"
	for _, selection := range []string{"repository:sample", "untrusted-selection"} {
		if _, err := session.Load(ctx, selection, ""); err != nil {
			t.Fatal(err)
		}
	}

	spans := recorder.Ended()
	if len(spans) != len(cases) {
		t.Fatalf("expected %d guidance spans, got %d", len(cases), len(spans))
	}
	for i, c := range cases {
		span := spans[i]
		if span.Name() != "guidance.load."+c.stage || span.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Fatalf("unexpected span: %s with parent %s", span.Name(), span.Parent().SpanID())
		}
		attributes := map[string]string{}
		for _, attr := range span.Attributes() {
			attributes[string(attr.Key)] = attr.Value.AsString()
		}
		if len(attributes) != 2 || attributes["weft.guidance.stage"] != c.stage || attributes["weft.guidance.outcome"] != c.outcome {
			t.Fatal(attributes)
		}
		if got := sampleCount(c.stage, c.outcome) - before[i]; got != 1 {
			t.Fatalf("%s/%s metric sample delta = %d", c.stage, c.outcome, got)
		}
	}
}
