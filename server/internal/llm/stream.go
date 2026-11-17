package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// StreamingProvider is implemented by providers that can stream an answer
// (NAT-09). ChatStream behaves exactly like Chat - same request, same final
// ChatResponse with the whole text, the tool calls assembled from their
// deltas, the stop reason and the usage - and additionally calls onText
// with each piece of answer text as it arrives. Hidden thinking is never
// passed to onText. A stream that fails or ends early returns an error and
// no response: text already passed to onText is not an answer.
//
// onText is called from the goroutine running ChatStream, in order; it must
// not block for long, since the stream waits for it. It may be nil.
type StreamingProvider interface {
	Provider
	ChatStream(ctx context.Context, req *ChatRequest, onText func(text string)) (*ChatResponse, error)
}

// ErrStreamIncomplete: the stream ended before the backend said the answer
// was complete (a dropped connection or a crashed server).
var ErrStreamIncomplete = errors.New("model stream ended before the answer was complete")

// ErrBadStream: a stream chunk could not be decoded.
var ErrBadStream = errors.New("malformed model stream")

// BadStream wraps a chunk decoding failure of provider's stream.
func BadStream(provider string, err error) error {
	return fmt.Errorf("llm %s: %w: %v", provider, ErrBadStream, err)
}

// Incomplete is ErrStreamIncomplete for provider.
func Incomplete(provider string) error {
	return fmt.Errorf("llm %s: %w", provider, ErrStreamIncomplete)
}

// maxStreamLine bounds one SSE or NDJSON line (a whole chunk of tool-call
// arguments can arrive in one), matching the non-streamed body limit.
const maxStreamLine = 64 << 20

// DoStream sends method path with a JSON body and, once the backend answers
// 2xx, passes the response body to read; whole is set when the backend
// ignored the stream request and answered with one JSON document (some
// OpenAI-compatible servers do), which read must then decode as a whole
// answer. Failures before the answer starts
// (rate limits, 5xx, transport errors) are retried like Do; a failure while
// reading is not, because part of the answer was already delivered. Read
// errors from a cancelled context are the context's error, and a client
// timeout is ErrModelTimeout.
func (c *HTTPClient) DoStream(ctx context.Context, method, path string, body any, accept string, read func(body io.Reader, whole bool) error) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("llm %s: encode request: %w", c.Provider, err)
	}
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			if err := c.wait(ctx, attempt, lastErr); err != nil {
				return err
			}
		}
		resp, err := c.send(ctx, method, path, payload, accept)
		if err == nil {
			err = read(resp.Body, IsWholeAnswer(resp.Header.Get("Content-Type")))
			_ = resp.Body.Close()
			if err != nil {
				return c.streamError(ctx, err)
			}
			return nil
		}
		lastErr = err
		if !IsRetryable(lastErr) || ctx.Err() != nil {
			return lastErr
		}
	}
	return lastErr
}

// streamError maps an error returned while reading a stream. Errors the
// reader built itself (an error the backend sent, an incomplete stream) are
// kept; transport failures are mapped like any other read failure.
func (c *HTTPClient) streamError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var ae *APIError
	if errors.Is(err, ErrStreamIncomplete) || errors.Is(err, ErrBadStream) || errors.As(err, &ae) {
		return err
	}
	return c.readError(ctx, err)
}

// IsWholeAnswer reports whether a response content type is one JSON
// document rather than a stream (SSE or newline-delimited JSON).
func IsWholeAnswer(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(ct, "application/json")
}

// DecodeWhole decodes a whole answer read from a stream request.
func DecodeWhole(provider string, body io.Reader, out any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxStreamLine))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return BadStream(provider, err)
	}
	return nil
}

// StreamError builds the error for a failure the backend reported inside a
// stream. It is an *APIError (status 0 unless the backend gave one) so the
// gateway accounts for it like any failure after dispatch.
func (c *HTTPClient) StreamError(status int, message string) error {
	message = strings.TrimSpace(message)
	if len(message) > maxErrBody {
		message = message[:maxErrBody] + "…"
	}
	return &APIError{Provider: c.Provider, Status: status, Body: "stream failed: " + message}
}

// ReadSSE reads a server-sent event stream and calls fn with each event's
// data (multi-line data joined with "\n"). Comments and events without data
// are skipped. fn returning io.EOF stops reading without error.
func ReadSSE(r io.Reader, fn func(data []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxStreamLine)
	var data []byte
	has := false
	flush := func() error {
		if !has {
			return nil
		}
		d := data
		data, has = nil, false
		return fn(d)
	}
	for sc.Scan() {
		line := sc.Bytes()
		switch {
		case len(line) == 0:
			if err := flush(); err != nil {
				return stopAtEOF(err)
			}
		case line[0] == ':':
			// comment / keep-alive
		default:
			field, value, _ := bytes.Cut(line, []byte(":"))
			value = bytes.TrimPrefix(value, []byte(" "))
			if string(field) == "data" {
				if has {
					data = append(data, '\n')
				}
				data = append(data, value...)
				has = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return stopAtEOF(flush())
}

// ReadLines calls fn with each non-blank line of a newline-delimited JSON
// stream. fn returning io.EOF stops reading without error.
func ReadLines(r io.Reader, fn func(line []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxStreamLine)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			return stopAtEOF(err)
		}
	}
	return sc.Err()
}

func stopAtEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
