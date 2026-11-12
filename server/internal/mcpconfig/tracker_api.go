package mcpconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TrackerAPI is used only by the trigger gateway's typed tracker operations.
// The connection is reviewed independently of the SCM destination.
type TrackerAPI struct {
	Connections *Manager
	Client      *http.Client
}

func trackerOrigin(r View, kind string) string {
	// Only provider-issued static API tokens from known first-party
	// connections may be used on the respective vendor API. OAuth tokens
	// and tokens belonging to custom endpoints keep their original audience.
	if !r.Enabled || r.LocalProcess != nil || r.OAuth != nil || !r.CredentialConfigured ||
		len(r.ArgumentPolicy) > 0 || r.AtlassianPolicy != nil {
		return ""
	}
	switch kind {
	case "github":
		if r.Service == "github" && strings.TrimSuffix(r.Endpoint, "/") == "https://api.githubcopilot.com/mcp" && credentialKind(r.CredentialType) == "bearer" {
			return "https://api.github.com"
		}
	case "jira":
		if (r.Service == "atlassian" || r.Service == "jira") && canonicalMCPResource(r.Endpoint) == atlassianMCPEndpoint &&
			(credentialKind(r.CredentialType) == "basic" || credentialKind(r.CredentialType) == "bearer") {
			return "https://api.atlassian.com"
		}
	}
	return ""
}

func (a *TrackerAPI) Revision(ctx context.Context, connection, kind string) (int64, error) {
	r, err := a.Connections.Get(ctx, connection)
	if err != nil || trackerOrigin(r, kind) == "" {
		return 0, errors.New("a compatible tracker API connection with a static scoped credential is required")
	}
	return r.Revision, nil
}

// Call dispatches a path constructed by trusted, operation-specific gateway
// code. A response body or upstream error never becomes diagnostic text.
func (a *TrackerAPI) Call(ctx context.Context, connection, kind string, revision int64, method, path string, body any) (json.RawMessage, int, error) {
	r, err := a.Connections.load(ctx, connection)
	if err != nil || r.Revision != revision {
		return nil, 0, errors.New("tracker connection changed")
	}
	origin := trackerOrigin(r.View, kind)
	if origin == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, 0, errors.New("tracker API scope is unavailable")
	}
	if err := a.Connections.admitCall(ctx, r); err != nil {
		return nil, 0, errors.New("tracker connection quota reached")
	}
	_, token, err := a.Connections.accessToken(ctx, r)
	if err != nil || token == "" {
		return nil, 0, errors.New("tracker API credential unavailable")
	}
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		payload = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, origin+path, payload)
	if err != nil {
		return nil, 0, errors.New("invalid tracker API request")
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", authorizationHeader(credentialContext(ctx, r.View), token))
	client, closeClient := remoteClient()
	defer closeClient()
	if a.Client != nil {
		copy := *a.Client
		client = &copy
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, errors.New("tracker API request failed")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, res.StatusCode, fmt.Errorf("tracker API returned HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil || len(raw) > maxBody || containsTransportCredential(credentialContext(ctx, r.View), raw, token) {
		return nil, res.StatusCode, errors.New("tracker API result rejected")
	}
	return raw, res.StatusCode, nil
}
