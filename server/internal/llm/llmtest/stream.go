package llmtest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Stream is a fake streaming endpoint: it answers every request with the
// scripted chunks, written and flushed one at a time, so a client sees them
// arrive separately. A Hang chunk blocks the stream until the client goes
// away (cancellation tests); the stream simply ends after the last chunk.
type Stream struct {
	*httptest.Server
	// ContentType of the answer (text/event-stream, application/x-ndjson).
	ContentType string

	mu     sync.Mutex
	chunks []string
	status []int
	bodies []string
	paths  []string
	accept []string
	// Sent is closed once the chunks before a Hang were flushed.
	Sent     chan struct{}
	sentOnce sync.Once
}

// Hang is a chunk that blocks the stream until the request is cancelled.
const Hang = "\x00hang"

// NewStream starts a fake streaming server; it is closed when the test ends.
func NewStream(t *testing.T, contentType string, chunks ...string) *Stream {
	t.Helper()
	s := &Stream{ContentType: contentType, chunks: chunks, Sent: make(chan struct{})}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

// FailFirst answers the first requests with these statuses (empty body)
// before streaming, to test retries before the stream starts.
func (s *Stream) FailFirst(statuses ...int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = statuses
}

// Requests returns the bodies, paths (with query) and Accept headers of the
// requests received.
func (s *Stream) Requests() (bodies, paths, accept []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...), append([]string(nil), s.paths...), append([]string(nil), s.accept...)
}

func (s *Stream) handle(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	n := len(s.bodies)
	s.bodies = append(s.bodies, string(raw))
	s.paths = append(s.paths, r.URL.RequestURI())
	s.accept = append(s.accept, r.Header.Get("Accept"))
	var fail int
	if n < len(s.status) {
		fail = s.status[n]
	}
	chunks := s.chunks
	s.mu.Unlock()
	if fail != 0 {
		w.WriteHeader(fail)
		return
	}
	w.Header().Set("Content-Type", s.ContentType)
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	for _, c := range chunks {
		if c == Hang {
			fl.Flush()
			s.sentOnce.Do(func() { close(s.Sent) })
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, c)
		fl.Flush()
	}
}
