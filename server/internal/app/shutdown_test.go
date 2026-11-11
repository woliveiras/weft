package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestShutdownHTTPIdle(t *testing.T) {
	if err := shutdownHTTP(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := shutdownHTTP(t.Context(), &http.Server{}); err != nil {
		t.Fatal(err)
	}
}
func TestShutdownHTTPTimeoutClosesRemainingConnections(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(cancelled) }))
	defer srv.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := http.Get(srv.URL)
		if err == nil {
			res.Body.Close()
		}
	}()
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := shutdownHTTP(ctx, srv.Config); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("request not cancelled after deadline")
	}
	<-done
}
