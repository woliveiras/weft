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
	if c.yielded.Load() {
		return ErrYield
	}
	if c.closed.Load() {
		return errors.New("turn has ended")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return errors.New("invalid callback input")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid callback request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(req.Header))
	resp, err := callbackHTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("controller unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("controller returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, output) != nil {
		return errors.New("invalid callback response")
	}
	return nil
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
		return ToolResult{}, errors.New("gateway tool failed")
	}
	return out, nil
}

func (c *Client) Chat(ctx context.Context, input ChatRequest) (ChatResponse, error) {
	var out ChatResponse
	err := c.post(ctx, "/llm/chat", input, &out)
	return out, err
}
