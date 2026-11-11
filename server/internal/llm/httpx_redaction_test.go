package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRedactCredentialHeaders(t *testing.T) {
	for _, tc := range []struct{ name, value, body, want string }{
		{"Authorization", "Bearer artificial-key", "rejected Bearer artificial-key / artificial-key", "rejected [redacted] / [redacted]"},
		{"X-Api-Key", "test-key", "rejected test-key", "rejected [redacted]"},
		{"Api-Key", "abc", "rejected abc", "rejected [redacted]"},
		{"Authorization", "", "empty credential", "empty credential"},
		{"Content-Type", "application/json", "application/json failure", "application/json failure"},
	} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			h := http.Header{}
			h.Set(tc.name, tc.value)
			if got := redactCredentialHeaders(tc.body, h); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHTTPProviderErrorRedactionPreservesRetryContract(t *testing.T) {
	const key = "artificial-boundary-key"
	for _, status := range []int{401, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+key {
					t.Error("credential missing on provider wire")
				}
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(status)
				// The credential crosses the old truncation boundary.
				_, _ = w.Write([]byte(strings.Repeat("x", maxErrBody-5) + key))
			}))
			defer srv.Close()
			c := NewHTTPClient(ProviderConfig{Name: "fixture", MaxRetries: -1}, srv.URL, map[string]string{"Authorization": "Bearer " + key})
			err := c.Do(context.Background(), "POST", "/chat", map[string]string{"prompt": "test"}, nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Status != status || apiErr.RetryAfter != 2*time.Second || apiErr.Retryable != (status != 401) {
				t.Fatalf("lost provider error contract: %v", err)
			}
			if strings.Contains(apiErr.Body, key[:5]) || !strings.Contains(apiErr.Body, "[reda") {
				t.Fatal("credential not redacted before truncation")
			}
		})
	}
}
