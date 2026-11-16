package trigger

import "testing"

func TestPendingRetryRequiresUnusedExplicitGenerationAndFailedAdmission(t *testing.T) {
	for _, tc := range []struct {
		processed string
		retry     FailedRetry
		want      bool
	}{
		{"failed", FailedRetry{}, false},
		{"failed", FailedRetry{Generation: "retry_1"}, true},
		{"failed", FailedRetry{Generation: "retry_1", Consumed: true}, false},
		{"tsk_started", FailedRetry{Generation: "retry_1"}, false},
		{"", FailedRetry{Generation: "retry_1"}, false},
	} {
		tr := &Trigger{Processed: map[string]string{"issue": tc.processed}, FailedRetries: map[string]FailedRetry{"issue": tc.retry}}
		if pendingFailedRetry(tr, "issue") != tc.want {
			t.Fatal(tc)
		}
	}
}

func TestPendingRetryIsBoundToTheOriginalUndeliveredTask(t *testing.T) {
	tr := &Trigger{Processed: map[string]string{"issue": "tsk_old"}, FailedRetries: map[string]FailedRetry{"issue": {Generation: "retry_1", TaskID: "tsk_old"}}}
	if !pendingFailedRetry(tr, "issue") {
		t.Fatal("explicit recovery unavailable")
	}
	tr.Processed["issue"] = "tsk_other"
	if pendingFailedRetry(tr, "issue") {
		t.Fatal("retry applied to a different admission")
	}
	tr.Processed["issue"] = "tsk_old"
	r := tr.FailedRetries["issue"]
	r.Consumed = true
	tr.FailedRetries["issue"] = r
	if pendingFailedRetry(tr, "issue") {
		t.Fatal("consumed retry reused")
	}
}
