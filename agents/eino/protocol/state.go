package protocol

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

func statePath(key string) (string, error) {
	if key == "" || len(key) > 512 || key == "." || key == ".." {
		return "", errors.New("invalid state key")
	}
	return "/state/" + url.PathEscape(key), nil
}

func (c *Client) StateGet(ctx context.Context, key string) ([]byte, bool, error) {
	path, err := statePath(key)
	if err != nil {
		return nil, false, err
	}
	raw, err := c.request(ctx, http.MethodGet, path, "", nil)
	var status *HTTPError
	if errors.As(err, &status) && status.Status == http.StatusNotFound {
		return nil, false, nil
	}
	return raw, err == nil, err
}

func (c *Client) StatePut(ctx context.Context, key string, value []byte) error {
	path, err := statePath(key)
	if err != nil {
		return err
	}
	if len(value) > 1<<20 {
		return errors.New("state exceeds 1 MiB")
	}
	_, err = c.request(ctx, http.MethodPut, path, "application/octet-stream", value)
	return err
}

func (c *Client) Ask(ctx context.Context, question string) (string, error) {
	var out struct {
		Answer string `json:"answer"`
	}
	err := c.post(ctx, "/ask", map[string]string{"question": question}, &out)
	return out.Answer, err
}

func (c *Client) Output(ctx context.Context, text string) error {
	return c.post(ctx, "/output", map[string]string{"text": text}, nil)
}
