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
