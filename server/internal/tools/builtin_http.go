package tools

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const capHTTP = "http.fetch"

const (
	defaultFetchBytes = 512 << 10
	maxFetchBytes     = 5 << 20
)

func httpTools(cfg BuiltinConfig) []Tool {
	return []Tool{&funcTool{
		name: "http.fetch", capability: capHTTP,
		description: "Fetch a URL over HTTP(S) and return the status, content type and body. HTML is converted to plain text. Private and internal network addresses are refused.",
		schema: schema(`{"type":"object","properties":{` +
			`"url":{"type":"string","description":"http:// or https:// URL"},` +
			`"method":{"type":"string","enum":["GET","POST","PUT","PATCH","DELETE","HEAD"]},` +
			`"headers":{"type":"object","additionalProperties":{"type":"string"}},` +
			`"body":{"type":"string"},` +
			`"maxBytes":{"type":"integer","minimum":1,"maximum":5242880}},"required":["url"],"additionalProperties":false}`),
		fn: func(ctx context.Context, inv *Invocation) (*Result, error) { return httpFetch(ctx, inv, cfg) },
	}}
}

// blockedNets are refused by the SSRF guard in addition to the stdlib
// loopback/private/link-local classifications.
var blockedNets = mustCIDRs("100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "0.0.0.0/8", "240.0.0.0/4", "64:ff9b::/96")

func mustCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}

// IsPublicIP reports whether ip is safe to contact from an agent: not
// loopback, private (RFC1918/ULA), link-local (incl. cloud metadata
// 169.254.169.254), multicast, unspecified, CGNAT or reserved.
func IsPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

// guardedDialer checks the resolved address of every connection, which
// also covers redirects and DNS rebinding (the check happens after resolution).
func guardedDialer(allowPrivate bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		var lastErr error = fmt.Errorf("%w: no usable address for %s", ErrDenied, host)
		for _, ip := range ips {
			if !allowPrivate && !IsPublicIP(ip.IP) {
				lastErr = fmt.Errorf("%w: %s resolves to non-public address %s", ErrDenied, host, ip.IP)
				continue
			}
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

func httpFetch(ctx context.Context, inv *Invocation, cfg BuiltinConfig) (*Result, error) {
	var a struct {
		URL      string            `json:"url"`
		Method   string            `json:"method"`
		Headers  map[string]string `json:"headers"`
		Body     string            `json:"body"`
		MaxBytes int64             `json:"maxBytes"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	u, err := url.Parse(a.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid url %q: only http and https URLs are allowed", a.URL)
	}
	if u.User != nil {
		return nil, errors.New("URLs with embedded credentials are not allowed")
	}
	if err := CheckHost(inv.Grant, u.Hostname()); err != nil {
		return nil, err
	}
	if a.Method == "" {
		a.Method = http.MethodGet
	}
	a.Method = strings.ToUpper(a.Method)
	if a.MaxBytes <= 0 {
		a.MaxBytes = defaultFetchBytes
	}
	if a.MaxBytes > maxFetchBytes {
		a.MaxBytes = maxFetchBytes
	}
	grant := inv.Grant
	client := &http.Client{
		Timeout: cfg.HTTPTimeout,
		Transport: &http.Transport{
			DialContext:           guardedDialer(cfg.AllowPrivateNetworks),
			Proxy:                 nil, // a proxy would bypass the address guard
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: cfg.HTTPTimeout,
			MaxIdleConns:          4,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to unsupported scheme %q", req.URL.Scheme)
			}
			return CheckHost(grant, req.URL.Hostname())
		},
	}
	var body io.Reader
	if a.Body != "" {
		body = strings.NewReader(a.Body)
	}
	req, err := http.NewRequestWithContext(ctx, a.Method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", cfg.UserAgent)
	for k, v := range a.Headers {
		if strings.EqualFold(k, "Host") {
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrDenied) {
			return nil, err
		}
		var ue *url.Error
		if errors.As(err, &ue) && errors.Is(ue.Err, ErrDenied) {
			return nil, ue.Err
		}
		return nil, fmt.Errorf("fetch %s: %w", u.Redacted(), err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, a.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	truncated := int64(len(raw)) > a.MaxBytes
	if truncated {
		raw = raw[:a.MaxBytes]
	}
	ct := resp.Header.Get("Content-Type")
	text := string(raw)
	if strings.Contains(strings.ToLower(ct), "text/html") {
		text = HTMLToText(text)
	}
	content := fmt.Sprintf("HTTP %s\ncontent-type: %s\n\n%s", resp.Status, ct, text)
	if truncated {
		content += fmt.Sprintf("\n…[body truncated at %d bytes]", a.MaxBytes)
	}
	return &Result{Content: content, IsError: resp.StatusCode >= 400, Truncated: truncated}, nil
}

var (
	reDropBlocks = regexp.MustCompile(`(?is)<(script|style|noscript|svg|head)\b.*?</(script|style|noscript|svg|head)>`)
	reComments   = regexp.MustCompile(`(?s)<!--.*?-->`)
	reBlockTags  = regexp.MustCompile(`(?i)</?(p|div|br|li|ul|ol|h[1-6]|tr|table|section|article|header|footer|pre|blockquote)\b[^>]*>`)
	reTags       = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces     = regexp.MustCompile(`[ \t\f\v]+`)
	reBlankLines = regexp.MustCompile(`\n\s*\n\s*\n+`)
)

// HTMLToText is a deliberately simple tag stripper: enough to give a model
// the readable content of a page without a DOM dependency.
func HTMLToText(s string) string {
	s = reComments.ReplaceAllString(s, "")
	s = reDropBlocks.ReplaceAllString(s, "")
	s = reBlockTags.ReplaceAllString(s, "\n")
	s = reTags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = reSpaces.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	s = strings.Join(lines, "\n")
	s = reBlankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
