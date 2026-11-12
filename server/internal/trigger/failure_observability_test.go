package trigger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestHoldFailuresReportOncePerPersistedTransition(t *testing.T) {
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	tr := &Trigger{ID: "trg_fixture", State: Active}
	raw, _ := json.Marshal(tr)
	if _, err := st.PutSetting(t.Context(), Prefix+tr.ID, raw, 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	r := Runner{Triggers: Manager{Store: st}, Log: slog.New(slog.NewJSONHandler(&logs, nil))}
	for _, h := range []Hold{
		{Key: "label", Publication: "label_failed"},
		{Key: "retry", State: "retry_failed"},
		{Key: "comment", Publication: "comment_failed"},
	} {
		outcome := holdFailureOutcome(h)
		counter := observability.TriggerReadinessFailures.WithLabelValues(outcome)
		var before, after dto.Metric
		if err := counter.Write(&before); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := r.saveHold(t.Context(), tr, h); err != nil {
				t.Fatal(err)
			}
		}
		if err := counter.Write(&after); err != nil {
			t.Fatal(err)
		}
		if after.GetCounter().GetValue()-before.GetCounter().GetValue() != 1 ||
			strings.Count(logs.String(), `"outcome":"`+outcome+`"`) != 1 {
			t.Fatalf("expected one metric and one structured log for %s: %s", outcome, &logs)
		}
	}
	if holdFailureOutcome(Hold{State: "unrecognized", Publication: "unrecognized"}) != "" {
		t.Fatal("unrecognized label could enter the metric")
	}
}
