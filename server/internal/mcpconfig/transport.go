package mcpconfig

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// DiscoveryError carries only platform-authored diagnostics, never upstream text.
type DiscoveryError struct{ Reason string }

func (e *DiscoveryError) Error() string  { return e.Reason }
func discoveryError(reason string) error { return &DiscoveryError{Reason: reason} }

const protocol = "2025-06-18"
const statelessProfile = "stateless-2026-07-28"
const statelessProtocol = "2026-07-28"

type transportProfileKey struct{}

const maxBody = 2 << 20

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001::/32", "::/96", "fec0::/10"} {
		if netip.MustParsePrefix(s).Contains(ip) {
			return false
		}
	}
	return true
}
func remoteClient() (*http.Client, func()) {
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if e != nil {
			return nil, discoveryError("MCP DNS lookup failed")
		}
		if len(ips) == 0 {
			return nil, discoveryError("MCP DNS returned no addresses")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, discoveryError("MCP private or reserved network denied")
			}
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return discoveryError("MCP redirects denied") }}, transport.CloseIdleConnections
}

func Discover(ctx context.Context, endpoint, token string) ([]Tool, error) {
	client, close := remoteClient()
	defer close()
	return DiscoverWithClient(ctx, endpoint, token, client)
}

// Call never retries tools/call: a lost response may follow a committed remote mutation.
func Call(ctx context.Context, endpoint, token, name string, args json.RawMessage) (json.RawMessage, error) {
	client, close := remoteClient()
	defer close()
	return CallWithClient(ctx, endpoint, token, name, args, client)
}
func CallWithClient(ctx context.Context, endpoint, token, name string, args json.RawMessage, client *http.Client) (json.RawMessage, error) {
	var result json.RawMessage
	_, err := sessionOperation(ctx, endpoint, token, client, func(call rpcCall) error {
		raw, err := call(2, "tools/call", map[string]any{"name": name, "arguments": args})
		if err != nil {
			return err
		}
		if containsTransportCredential(ctx, raw, token) {
			return discoveryError("MCP result contains credential material")
		}
		result = raw
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type rpcCall func(int, string, any) (json.RawMessage, error)

// DiscoverWithClient supports trusted platform transports and in-process contract tests.
// No HTTP client or transport can be supplied through the public API.
func DiscoverWithClient(ctx context.Context, endpoint, token string, client *http.Client) ([]Tool, error) {
	return sessionOperation(ctx, endpoint, token, client, nil)
}

var errSessionExpired = discoveryError("MCP session expired")

// Recovery is bounded to one new session within the original operation deadline.
// Discovery can restart; dispatched tools/call requests are never replayed.
func sessionOperation(ctx context.Context, endpoint, token string, client *http.Client, action func(rpcCall) error) ([]Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	dispatched := false
	var guarded func(rpcCall) error
	if action != nil {
		guarded = func(call rpcCall) error {
			if dispatched {
				return discoveryError("MCP session expired during tool call; remote outcome unknown; no automatic retry")
			}
			return action(func(id int, method string, params any) (json.RawMessage, error) {
				if method == "tools/call" {
					dispatched = true
				}
				return call(id, method, params)
			})
		}
	}
	catalog, err := sessionAttempt(ctx, endpoint, token, client, guarded)
	if errors.Is(err, errSessionExpired) && ctx.Err() == nil {
		return sessionAttempt(ctx, endpoint, token, client, guarded)
	}
	return catalog, err
}
func sessionAttempt(ctx context.Context, endpoint, token string, client *http.Client, action func(rpcCall) error) (catalog []Tool, err error) {
	session := ""
	stateless := ctx.Value(transportProfileKey{}) == statelessProfile
	defer func() {
		if session == "" {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		req, e := http.NewRequestWithContext(cleanup, http.MethodDelete, endpoint, nil)
		if e == nil {
			req.Header.Set("Mcp-Session-Id", session)
			req.Header.Set("MCP-Protocol-Version", protocol)
			applyTransportHeaders(ctx, req, token)
			var res *http.Response
			res, e = client.Do(req)
			if e == nil {
				res.Body.Close()
				if res.StatusCode != 200 && res.StatusCode != 204 && res.StatusCode != 405 && res.StatusCode != 404 {
					e = discoveryError("session close failed")
				}
			}
		}
		if e != nil && err == nil {
			catalog = nil
			err = discoveryError("MCP session cleanup failed")
		}
	}()
	call := func(id int, method string, params any) (json.RawMessage, error) {
		if stateless {
			encoded, e := json.Marshal(params)
			if e != nil {
				return nil, discoveryError("invalid MCP request parameters")
			}
			var fields map[string]any
			if json.Unmarshal(encoded, &fields) != nil || fields == nil {
				return nil, discoveryError("invalid MCP request parameters")
			}
			fields["_meta"] = map[string]any{"io.modelcontextprotocol/protocolVersion": statelessProtocol, "io.modelcontextprotocol/clientCapabilities": map[string]any{}, "io.modelcontextprotocol/clientInfo": map[string]string{"name": "Weft", "version": "1"}}
			params = fields
		}
		msg := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id > 0 {
			msg["id"] = id
		}
		body, _ := json.Marshal(msg)
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if e != nil {
			return nil, e
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if stateless {
			req.Header.Set("MCP-Protocol-Version", statelessProtocol)
			req.Header.Set("Mcp-Method", method)
			if method == "tools/call" {
				name, _ := params.(map[string]any)["name"].(string)
				if !toolName.MatchString(name) {
					return nil, discoveryError("invalid MCP tool name")
				}
				req.Header.Set("Mcp-Name", name)
			}
		} else if id != 1 {
			req.Header.Set("MCP-Protocol-Version", protocol)
		}
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		applyTransportHeaders(ctx, req, token)
		res, e := client.Do(req)
		if e != nil {
			var safe *DiscoveryError
			if errors.As(e, &safe) {
				return nil, safe
			}
			if ctx.Err() != nil {
				return nil, discoveryError("MCP discovery cancelled or timed out")
			}
			return nil, discoveryError("MCP request failed; check TLS and reachability")
		}
		defer res.Body.Close()
		if res.StatusCode == http.StatusNotFound && session != "" && method != "initialize" {
			session = "" // The peer has already terminated it.
			return nil, errSessionExpired
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, discoveryError(fmt.Sprintf("MCP HTTP status %d", res.StatusCode))
		}
		if id == 0 {
			if res.StatusCode != 202 {
				return nil, discoveryError("MCP notification was not accepted")
			}
			return nil, nil
		}
		if id == 1 && !stateless {
			session = res.Header.Get("Mcp-Session-Id")
			if len(session) > 1024 || strings.IndexFunc(session, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 {
				return nil, discoveryError("invalid MCP session")
			}
		}
		reader := io.LimitReader(res.Body, maxBody+1)
		var raw []byte
		if strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
			scanner := bufio.NewScanner(reader)
			scanner.Buffer(make([]byte, 4096), maxBody)
			var lines []string
			total := 0
			for scanner.Scan() {
				line := scanner.Text()
				total += len(line) + 1
				if total > maxBody {
					return nil, discoveryError("MCP stream too large")
				}
				if line == "" && len(lines) > 0 {
					candidate := []byte(strings.Join(lines, "\n"))
					var env struct {
						ID int `json:"id"`
					}
					if json.Unmarshal(candidate, &env) == nil && env.ID == id {
						raw = candidate
						break
					}
					lines = nil
				}
				if strings.HasPrefix(line, "data:") {
					lines = append(lines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
				}
			}
			if e := scanner.Err(); e != nil {
				return nil, discoveryError("invalid MCP stream")
			}
		} else if strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
			raw, e = io.ReadAll(reader)
			if e != nil {
				return nil, e
			}
		} else {
			return nil, discoveryError("unsupported MCP response content type")
		}
		if len(raw) > maxBody {
			return nil, discoveryError("MCP response too large")
		}
		var env struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int             `json:"id"`
			Result  json.RawMessage `json:"result"`
			Error   json.RawMessage `json:"error"`
		}
		if json.Unmarshal(raw, &env) != nil || env.JSONRPC != "2.0" || env.ID != id || len(env.Error) > 0 || len(env.Result) == 0 {
			return nil, discoveryError("invalid or failed MCP response")
		}
		if stateless {
			var result map[string]json.RawMessage
			if json.Unmarshal(env.Result, &result) != nil || result == nil {
				return nil, discoveryError("invalid MCP result")
			}
			if _, exists := result["inputRequests"]; exists {
				return nil, discoveryError("MCP additional input requests are unsupported; no automatic retry")
			}
		}
		return env.Result, nil
	}
	return protocolOperation(ctx, call, stateless, token, action)
}

// protocolOperation keeps catalogue validation identical across trusted transports.
func protocolOperation(ctx context.Context, call rpcCall, stateless bool, token string, action func(rpcCall) error) ([]Tool, error) {
	if !stateless {
		init, e := call(1, "initialize", map[string]any{"protocolVersion": protocol, "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "Weft", "version": "1"}})
		if e != nil {
			return nil, e
		}
		var hello struct {
			Protocol     string                     `json:"protocolVersion"`
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		}
		if json.Unmarshal(init, &hello) != nil || hello.Protocol != protocol || hello.Capabilities["tools"] == nil {
			return nil, discoveryError("MCP tools capability or protocol unavailable")
		}
		if _, e = call(0, "notifications/initialized", map[string]any{}); e != nil {
			return nil, e
		}
	}
	if action != nil {
		return nil, action(call)
	}
	out := []Tool{}
	cursor := ""
	seen := map[string]bool{}
	names := map[string]bool{}
	bytesTotal := 0
	for page := 0; page < 20; page++ {
		params := map[string]string{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, e := call(page+2, "tools/list", params)
		if e != nil {
			return nil, e
		}
		if containsTransportCredential(ctx, raw, token) {
			return nil, discoveryError("MCP catalogue contains credential material")
		}
		bytesTotal += len(raw)
		if bytesTotal > maxBody {
			return nil, discoveryError("MCP catalogue too large")
		}
		var list struct {
			Tools []Tool `json:"tools"`
			Next  string `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &list) != nil || list.Tools == nil {
			return nil, discoveryError("invalid MCP tool catalogue")
		}
		for _, t := range list.Tools {
			var schema map[string]any
			if !toolName.MatchString(t.Name) || names[t.Name] || len(t.Description) > 8192 || json.Unmarshal(t.InputSchema, &schema) != nil || schema["type"] != "object" {
				return nil, discoveryError("invalid MCP tool definition")
			}
			if stateless && containsHeaderAnnotation(schema) {
				return nil, discoveryError("MCP custom parameter headers are unsupported by this profile")
			}
			names[t.Name] = true
			out = append(out, t)
			if len(out) > 500 {
				return nil, discoveryError("MCP catalogue exceeds 500 tools")
			}
		}
		cursor = list.Next
		if cursor == "" {
			return out, nil
		}
		if seen[cursor] || len(cursor) > 4096 {
			return nil, discoveryError("invalid MCP cursor")
		}
		seen[cursor] = true
	}
	return nil, discoveryError("MCP catalogue exceeds 20 pages")
}

// Reject annotated catalogues rather than silently omit required wire metadata.
func containsHeaderAnnotation(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		if _, ok := v["x-mcp-header"]; ok {
			return true
		}
		for _, child := range v {
			if containsHeaderAnnotation(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsHeaderAnnotation(child) {
				return true
			}
		}
	}
	return false
}
