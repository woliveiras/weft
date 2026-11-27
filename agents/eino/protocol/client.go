package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/otel/propagation"
)

const maxResponse = 16 << 20

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var toolName = regexp.MustCompile(`^[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*$`)

type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("controller returned HTTP %d", e.Status) }

var ErrToolFailed = errors.New("gateway tool failed")
var ErrYield = errors.New("turn yielded for review")
var callbackHTTP = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

type Client struct {
	base, token string
	yielded     atomic.Bool
	closed      atomic.Bool
}

func bind(in *TurnInput, origins map[string]bool) (*Client, error) {
	if !identifier.MatchString(in.ExecutionID) || in.Callback.Token == "" || strings.ContainsAny(in.Callback.Token, "\r\n\t ") {
		return nil, errors.New("invalid execution capability")
	}
	u, err := url.Parse(in.Callback.URL)
	if err != nil || !origins[u.Scheme+"://"+u.Host] || in.Callback.URL != u.Scheme+"://"+u.Host+"/internal/v1/executions/"+in.ExecutionID {
		return nil, errors.New("invalid callback address")
	}
	return &Client{base: in.Callback.URL, token: in.Callback.Token}, nil
}

func (c *Client) post(ctx context.Context, path string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return errors.New("invalid callback input")
	}
	raw, err := c.request(ctx, http.MethodPost, path, "application/json", body)
	if err != nil {
		return err
	}
	if output != nil && (bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, output) != nil) {
		return errors.New("invalid callback response")
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, path, contentType string, body []byte) ([]byte, error) {
	if c.yielded.Load() {
		return nil, ErrYield
	}
	if c.closed.Load() {
		return nil, errors.New("turn has ended")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid callback request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(req.Header))
	resp, err := callbackHTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("controller unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, &HTTPError{Status: resp.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse {
		return nil, errors.New("invalid callback response")
	}
	return raw, nil
}

func (c *Client) Tool(ctx context.Context, name string, args any) (ToolResult, error) {
	if !toolName.MatchString(name) {
		return ToolResult{}, errors.New("invalid tool name")
	}
	var out ToolResult
	if err := c.post(ctx, "/tools/"+name, args, &out); err != nil {
		return ToolResult{}, err
	}
	if out.Yield {
		c.yielded.Store(true)
		return ToolResult{}, ErrYield
	}
	if out.IsError {
		return ToolResult{}, ErrToolFailed
	}
	return out, nil
}

func (c *Client) Chat(ctx context.Context, input ChatRequest) (ChatResponse, error) {
	var out ChatResponse
	err := c.post(ctx, "/llm/chat", input, &out)
	return out, err
}

// ListTools returns the current execution's authorized schemas.
func (c *Client) ListTools(ctx context.Context) ([]ToolSchema, error) {
	raw, err := c.request(ctx, http.MethodGet, "/tools", "", nil)
	if err != nil {
		return nil, err
	}
	var out []ToolSchema
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return nil, errors.New("invalid tool catalog")
	}
	return out, nil
}

// Emit queues an output at the owning controller. Publication follows turn success.
func (c *Client) Emit(ctx context.Context, msg Emit) error {
	var ack struct{ Queued bool }
	if err := c.post(ctx, "/emit", msg, &ack); err != nil {
		return err
	}
	if !ack.Queued {
		return errors.New("controller did not queue output")
	}
	return nil
}
