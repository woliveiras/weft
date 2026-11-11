package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// APIError is a non-2xx answer from a provider. Body is truncated so a
// verbose HTML error page never floods logs or the event store.
type APIError struct {
	Provider  string
	Status    int
	Body      string
	Retryable bool
	// RetryAfter is the server-requested delay, when it sent one.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("llm %s: HTTP %d: %s", e.Provider, e.Status, e.Body)
}

// IsRetryable reports whether err is worth retrying (rate limit, 5xx,
// network failure). Context cancellation never is.
// ErrModelTimeout: the provider did not answer within its timeout. Not
// retried: a model that needed the whole timeout once will likely need it
// again, and each retry would hold the turn for another full timeout.
var ErrModelTimeout = errors.New("model call timed out")

func IsRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrModelTimeout) {
		return false
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Retryable
	}
	return true // transport-level error
}

const maxErrBody = 2048

// APIKey resolves the provider key from APIKeyValue, APIKeyEnv or APIKeyFile. An empty
// key with neither configured is not an error: local backends (Ollama, vLLM,
// TGI) need none. A configured but missing source is an error, because a
// silently keyless request to a hosted API fails later with a worse message.
func (c ProviderConfig) APIKey() (string, error) {
	if c.APIKeyValue != "" {
		return c.APIKeyValue, nil
	}
	if c.APIKeyEnv != "" {
		v := strings.TrimSpace(os.Getenv(c.APIKeyEnv))
		if v == "" {
			return "", fmt.Errorf("llm %s: environment variable %s is empty", c.Name, c.APIKeyEnv)
		}
		return v, nil
	}
	if c.APIKeyFile != "" {
		b, err := os.ReadFile(c.APIKeyFile)
		if err != nil {
			return "", fmt.Errorf("llm %s: read api key file: %w", c.Name, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return "", nil
}

// Timeout returns the configured per-request timeout (default 300s: local
// models on CPU are slow, and a turn timeout bounds the whole thing anyway).
func (c ProviderConfig) Timeout() time.Duration {
	if c.TimeoutSec > 0 {
		return time.Duration(c.TimeoutSec) * time.Second
	}
	return 300 * time.Second
}

// Retries returns the configured retry count (default 3; negative disables).
func (c ProviderConfig) Retries() int {
	if c.MaxRetries < 0 {
		return 0
	}
	if c.MaxRetries == 0 {
		return 3
	}
	return c.MaxRetries
}

// HTTPClient is a small JSON-over-HTTP client with retries shared by every
// HTTP-based provider.
type HTTPClient struct {
	Provider string
	BaseURL  string
	Headers  map[string]string
	Client   *http.Client
	Retries  int
	// BaseDelay is the first backoff step (default 500ms); tests shrink it.
	BaseDelay time.Duration
}

// NewHTTPClient builds a client from provider config plus static headers.
func NewHTTPClient(cfg ProviderConfig, baseURL string, headers map[string]string) *HTTPClient {
	h := map[string]string{}
	for k, v := range headers {
		h[k] = v
	}
	for k, v := range cfg.Headers {
		h[k] = v
	}
	return &HTTPClient{
		Provider:  cfg.Name,
		BaseURL:   strings.TrimRight(baseURL, "/"),
		Headers:   h,
		Client:    &http.Client{Timeout: cfg.Timeout()},
		Retries:   cfg.Retries(),
		BaseDelay: 500 * time.Millisecond,
	}
}

// Do sends method path with body (JSON-encoded unless nil) and decodes a
// 2xx JSON response into out (if non-nil). Retries on 429, 5xx and
// transport errors with exponential backoff and full jitter, honouring
// Retry-After.
func (c *HTTPClient) Do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("llm %s: encode request: %w", c.Provider, err)
		}
	}
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			delay := c.backoff(attempt)
			var ae *APIError
			if errors.As(lastErr, &ae) && ae.RetryAfter > 0 {
				delay = ae.RetryAfter
			}
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
		lastErr = c.once(ctx, method, path, payload, out)
		if lastErr == nil || !IsRetryable(lastErr) || ctx.Err() != nil {
			return lastErr
		}
	}
	return lastErr
}

func (c *HTTPClient) backoff(attempt int) time.Duration {
	base := c.BaseDelay
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	max := base << min(attempt, 6)
	if max > 30*time.Second {
		max = 30 * time.Second
	}
	return time.Duration(rand.Int64N(int64(max)) + 1)
}

func (c *HTTPClient) once(ctx context.Context, method, path string, payload []byte, out any) error {
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range c.Headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return fmt.Errorf("%w: %s gave no answer within %s (the provider's timeout); the model may still be loading or the machine running it may be short of memory: use a smaller model, free memory, or raise the provider's timeout",
				ErrModelTimeout, c.Provider, c.Client.Timeout)
		}
		return fmt.Errorf("llm %s: %w", c.Provider, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("llm %s: read response: %w", c.Provider, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Redact before truncation, which could otherwise expose a key prefix.
		b := redactCredentialHeaders(string(data), req.Header)
		if len(b) > maxErrBody {
			b = b[:maxErrBody] + "…"
		}
		ae := &APIError{
			Provider:  c.Provider,
			Status:    resp.StatusCode,
			Body:      strings.TrimSpace(b),
			Retryable: resp.StatusCode == 429 || resp.StatusCode == 408 || resp.StatusCode >= 500,
		}
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if s, err := strconv.Atoi(ra); err == nil && s >= 0 && s <= 120 {
				ae.RetryAfter = time.Duration(s) * time.Second
			}
		}
		return ae
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("llm %s: decode response: %w", c.Provider, err)
	}
	return nil
}

// LastUserText returns the content of the last user message (or "").
func LastUserText(msgs []ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}

// NormalizeArgs turns tool-call arguments that arrived as a JSON string
// (OpenAI encodes them that way) or as nothing into a JSON object document.
func NormalizeArgs(raw json.RawMessage) json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`)
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			s = strings.TrimSpace(s)
			if s == "" {
				return json.RawMessage(`{}`)
			}
			if json.Valid([]byte(s)) {
				return json.RawMessage(s)
			}
			b, _ := json.Marshal(map[string]string{"_raw": s})
			return b
		}
	}
	if !json.Valid(raw) {
		b, _ := json.Marshal(map[string]string{"_raw": string(raw)})
		return b
	}
	return raw
}

// EmptySchema is used when a tool declares no parameters; vendors reject a
// missing schema.
var EmptySchema = json.RawMessage(`{"type":"object","properties":{}}`)

// SchemaOrEmpty returns s, or EmptySchema when s is blank.
func SchemaOrEmpty(s json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(s)) == 0 || string(s) == "null" {
		return EmptySchema
	}
	return s
}

// redactCredentialHeaders sanitizes provider error bodies at the HTTP boundary,
// before they can reach runtime callbacks, events, or callers of Models.
func redactCredentialHeaders(body string, headers http.Header) string {
	for _, name := range []string{"Authorization", "X-Api-Key", "Api-Key"} {
		for _, value := range headers.Values(name) {
			if value == "" {
				continue
			}
			body = strings.ReplaceAll(body, value, "[redacted]")
			if name == "Authorization" {
				if _, credential, ok := strings.Cut(value, " "); ok && credential != "" {
					body = strings.ReplaceAll(body, credential, "[redacted]")
				}
			}
		}
	}
	return body
}
