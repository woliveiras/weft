package llm_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/llm/llmtest"
)

func TestReadSSE(t *testing.T) {
	in := ": keep-alive\n\nevent: x\ndata: one\n\ndata: two\ndata: lines\n\nid: 3\n\ndata:three"
	var got []string
	if err := llm.ReadSSE(strings.NewReader(in), func(d []byte) error { got = append(got, string(d)); return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "one|two\nlines|three" {
		t.Fatalf("events = %q", got)
	}
	got = nil
	err := llm.ReadSSE(strings.NewReader("data: a\n\ndata: b\n\n"), func(d []byte) error { got = append(got, string(d)); return io.EOF })
	if err != nil || len(got) != 1 {
		t.Fatalf("io.EOF must stop without error: %v %q", err, got)
	}
	boom := errors.New("boom")
	if err := llm.ReadSSE(strings.NewReader("data: a\n\n"), func([]byte) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadLines(t *testing.T) {
	var got []string
	if err := llm.ReadLines(strings.NewReader("{\"a\":1}\n\n  \n{\"b\":2}"), func(l []byte) error { got = append(got, string(l)); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1] != `{"b":2}` {
		t.Fatalf("lines = %q", got)
	}
}

func newStreamClient(s *llmtest.Stream, retries int) *llm.HTTPClient {
	c := llm.NewHTTPClient(llm.ProviderConfig{Name: "p", MaxRetries: retries}, s.URL, nil)
	c.BaseDelay = time.Millisecond
	return c
}

func TestDoStreamRetriesOnlyBeforeTheAnswerStarts(t *testing.T) {
	s := llmtest.NewStream(t, "text/event-stream", "data: a\n\n", "data: b\n\n")
	s.FailFirst(503, 429)
	var got []string
	err := newStreamClient(s, 3).DoStream(context.Background(), http.MethodPost, "/chat", map[string]any{"x": 1}, "text/event-stream", func(r io.Reader, _ bool) error {
		return llm.ReadSSE(r, func(d []byte) error { got = append(got, string(d)); return nil })
	})
	if err != nil || strings.Join(got, "") != "ab" {
		t.Fatalf("err %v got %q", err, got)
	}
	bodies, _, accept := s.Requests()
	if len(bodies) != 3 || bodies[2] != `{"x":1}` || accept[2] != "text/event-stream" {
		t.Fatalf("requests %q accept %q", bodies, accept)
	}

	// A failure while reading is final: part of the answer was delivered.
	s2 := llmtest.NewStream(t, "text/event-stream", "data: a\n\n")
	calls := 0
	failed := llm.Incomplete("p")
	err = newStreamClient(s2, 3).DoStream(context.Background(), http.MethodPost, "/chat", struct{}{}, "text/event-stream", func(r io.Reader, _ bool) error {
		calls++
		return failed
	})
	if !errors.Is(err, llm.ErrStreamIncomplete) || calls != 1 {
		t.Fatalf("err %v after %d reads", err, calls)
	}
	if b, _, _ := s2.Requests(); len(b) != 1 {
		t.Fatalf("a stream that started must not be retried: %d requests", len(b))
	}
}

func TestDoStreamStopsPromptlyOnCancel(t *testing.T) {
	s := llmtest.NewStream(t, "text/event-stream", "data: a\n\n", llmtest.Hang)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-s.Sent; cancel() }()
	start := time.Now()
	err := newStreamClient(s, 0).DoStream(ctx, http.MethodPost, "/chat", struct{}{}, "text/event-stream", func(r io.Reader, _ bool) error {
		return llm.ReadSSE(r, func([]byte) error { return nil })
	})
	if !errors.Is(err, context.Canceled) || time.Since(start) > 5*time.Second {
		t.Fatalf("err %v after %s", err, time.Since(start))
	}
}

func TestStreamErrorIsAnAPIErrorAfterDispatch(t *testing.T) {
	c := llm.NewHTTPClient(llm.ProviderConfig{Name: "p"}, "http://x", nil)
	err := c.StreamError(0, " overloaded ")
	var ae *llm.APIError
	if !errors.As(err, &ae) || ae.Retryable || err.Error() != "llm p: stream failed: overloaded" {
		t.Fatalf("err %#v %q", err, err)
	}
	if llm.IsRetryable(err) {
		t.Fatal("a failure inside a stream must not be retried")
	}
}

// streamer is a provider that streams scripted pieces of its answer.
type streamer struct {
	pieces []string
	resp   *llm.ChatResponse
	err    error
	chats  int
}

func (s *streamer) Name() string { return "s" }
func (s *streamer) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	s.chats++
	return s.resp, s.err
}
func (s *streamer) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (s *streamer) ChatStream(_ context.Context, _ *llm.ChatRequest, onText func(string)) (*llm.ChatResponse, error) {
	for _, p := range s.pieces {
		onText(p)
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

// ChatStream is Chat with live text: the same answer and the same observed
// call (accounting), whether the provider streams or not.
func TestRegistryChatStreamAccountsLikeChat(t *testing.T) {
	answer := &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: "hello world"}, StopReason: "stop",
		Usage: llm.Usage{InputTokens: 12, OutputTokens: 5}}
	req := func() *llm.ChatRequest {
		return &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: strings.Repeat("x", 400)}}, Metadata: map[string]string{"execution": "x1"}}
	}
	run := func(p llm.Provider, stream bool) (*llm.ChatResponse, error, []llm.Call, []string) {
		var calls []llm.Call
		var live []string
		r, _ := llm.NewRegistry(nil, "")
		r.Add(llm.ProviderConfig{Name: "p", Type: "ollama", DefaultModel: "m"}, p)
		r.SetObserver(func(_ context.Context, c llm.Call) { calls = append(calls, c) })
		if !stream {
			resp, err := r.Chat(context.Background(), "p", req())
			return resp, err, calls, nil
		}
		resp, err := r.ChatStream(context.Background(), "p", req(), func(s string) { live = append(live, s) })
		return resp, err, calls, live
	}

	whole, err, wholeCalls, _ := run(&streamer{resp: answer}, false)
	if err != nil {
		t.Fatal(err)
	}
	streamed, err, streamCalls, live := run(&streamer{pieces: []string{"hello", " world"}, resp: answer}, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(live, "") != "hello world" || streamed != whole || len(streamCalls) != 1 || len(wholeCalls) != 1 ||
		streamCalls[0].Usage != wholeCalls[0].Usage || streamCalls[0].Estimated || streamCalls[0].Model != "m" {
		t.Fatalf("live %q resp %+v calls %+v vs %+v", live, streamed, streamCalls, wholeCalls)
	}

	// A provider that cannot stream answers whole: no live text, same call.
	plain, err, plainCalls, live := run(scripted{resp: answer}, true)
	if err != nil || plain != answer || len(live) != 0 || len(plainCalls) != 1 || plainCalls[0].Usage != answer.Usage {
		t.Fatalf("fallback resp %+v err %v live %q calls %+v", plain, err, live, plainCalls)
	}

	// A stream that fails midway is a failure: no answer, and the call is
	// charged with the request estimate like any failure after dispatch.
	broken, err, brokenCalls, live := run(&streamer{pieces: []string{"hel"}, err: llm.Incomplete("p")}, true)
	if broken != nil || !errors.Is(err, llm.ErrStreamIncomplete) || strings.Join(live, "") != "hel" ||
		len(brokenCalls) != 1 || !brokenCalls[0].Estimated || brokenCalls[0].Err == nil ||
		brokenCalls[0].Usage != (llm.Usage{InputTokens: 102, OutputTokens: llm.DefaultEstimatedOutput}) {
		t.Fatalf("broken resp %+v err %v live %q calls %+v", broken, err, live, brokenCalls)
	}
}
