package api_test

import (
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestUnavailableChatCallIncrementsOnlyBoundedTelemetry(t *testing.T) {
	count := func() uint64 {
		t.Helper()
		families, err := prometheus.DefaultGatherer.Gather()
		if err != nil {
			t.Fatal(err)
		}
		for _, family := range families {
			if family.GetName() != "weft_chat_operation_duration_seconds" {
				continue
			}
			for _, metric := range family.Metric {
				labels := map[string]string{}
				for _, label := range metric.Label {
					labels[label.GetName()] = label.GetValue()
				}
				if len(labels) != 2 {
					t.Fatalf("unexpected label names on Chat operation")
				}
				if labels["operation"] == "chat_interpretation" && labels["outcome"] == "model_unavailable" {
					return metric.Histogram.GetSampleCount()
				}
			}
		}
		return 0
	}
	srv := setup(t)
	before := count()
	const body = `{"requestId":"10192572086b4b50a83981d63b1a811b","text":"Private project question"}`
	if status := call(t, srv, "POST", "/api/v1/chat/sessions", viewerTok, "application/json", body, nil); status != http.StatusOK {
		t.Fatal(status)
	}
	if after := count(); after != before+1 {
		t.Fatalf("sample count %d before, %d after", before, after)
	}
}
