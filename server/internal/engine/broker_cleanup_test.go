package engine

import (
	"testing"
	"time"
)

func TestBrokerCleanupBackoff(t *testing.T) {
	now := time.Unix(0, 0)
	var retry brokerCleanupRetry
	for _, want := range []time.Duration{2, 4, 8, 16, 32, 60, 60} {
		retry = retry.afterFailure(now)
		if retry.delay != want*time.Second || !retry.next.Equal(now.Add(want*time.Second)) {
			t.Fatalf("retry %+v, want %ds", retry, want)
		}
		now = retry.next
	}
}
