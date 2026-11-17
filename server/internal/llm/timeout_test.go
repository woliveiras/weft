package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelTimeoutIsExplainedAndNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	c := NewHTTPClient(ProviderConfig{Name: "slow", TimeoutSec: 1, MaxRetries: 3}, srv.URL, nil)
	err := c.Do(context.Background(), http.MethodPost, "/chat", map[string]string{}, nil)
	if !errors.Is(err, ErrModelTimeout) {
		t.Fatalf("want ErrModelTimeout, got %v", err)
	}
	for _, want := range []string{"slow gave no answer within 1s", "smaller model", "raise the provider's timeout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %q: %v", want, err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("a timed-out call must not be retried: %d requests", n)
	}
}
