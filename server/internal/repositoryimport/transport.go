// Package repositoryimport imports Git snapshots using explicitly selected
// authentication, without repository code execution. Task admission is separate.
package repositoryimport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/woliveiras/weft/server/internal/tools"
)

var ErrInvalid = errors.New("invalid repository import")
var pathPattern = regexp.MustCompile(`^/[A-Za-z0-9._~/-]+$`)
var refPattern = regexp.MustCompile(`^(HEAD|refs/(heads|tags)/[A-Za-z0-9._/-]+|[a-f0-9]{40}|[a-f0-9]{64})$`)

func ValidateURL(raw string) (string, error) {
	if len(raw) > 2048 || strings.TrimSpace(raw) != raw {
		return "", ErrInvalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Port() != "" || u.Opaque != "" || u.RawPath != "" || !pathPattern.MatchString(u.Path) {
		return "", ErrInvalid
	}
	if _, err := netip.ParseAddr(u.Hostname()); err == nil {
		return "", ErrInvalid
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return "", ErrInvalid
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return "", ErrInvalid
		}
	}
	u.Host = host
	u.Path = strings.TrimSuffix(u.Path, "/")
	if u.Path == "" || strings.Contains(u.Path, "//") {
		return "", ErrInvalid
	}
	if host == "github.com" {
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) != 2 {
			return "", ErrInvalid
		}
		if !strings.HasSuffix(u.Path, ".git") {
			u.Path += ".git"
		}
	}
	return u.String(), nil
}
func ValidateRef(ref string) bool {
	return len(ref) <= 256 && refPattern.MatchString(ref) && !strings.Contains(ref, "..") && !strings.Contains(ref, "//") && !strings.HasSuffix(ref, "/") && !strings.HasSuffix(ref, ".") && !strings.Contains(ref, ".lock")
}
func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || !tools.IsPublicIP(net.IP(ip.AsSlice())) {
		return false
	}
	for _, block := range []string{"192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32", "64:ff9b:1::/48", "2002::/16", "2001::/32", "::/96", "fec0::/10"} {
		if netip.MustParsePrefix(block).Contains(ip) {
			return false
		}
	}
	return true
}
func publicClient() *http.Client {
	t := &http.Transport{Proxy: nil, DisableCompression: true, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("repository DNS unavailable")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("non-public repository address denied")
			}
		}
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}}
	return &http.Client{Transport: t, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// A private loopback bridge is the only URL Git sees. The bridge forwards only
// upload-pack discovery/read requests to the validated HTTPS target. DNS is pinned
// at dial time. No redirects, proxies or cookies pass. Explicit credentials are
// attached only to the fixed upstream, never forwarded from the loopback request.
func bridge(ctx context.Context, source string, client *http.Client, limit int64, received *atomic.Int64, auth ...Authentication) (string, func(), *atomic.Int32, error) {
	scopedClient := *client
	scopedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	scopedClient.Jar = nil
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, nil, err
	}
	token := randomID()
	base := "/" + token
	status := &atomic.Int32{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		suffix := strings.TrimPrefix(r.URL.Path, base)
		if r.URL.Path != base+suffix || !((r.Method == "GET" && suffix == "/info/refs" && r.URL.RawQuery == "service=git-upload-pack") || (r.Method == "POST" && suffix == "/git-upload-pack" && r.URL.RawQuery == "")) {
			http.Error(w, "denied", 403)
			return
		}
		target := source + suffix
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		var body io.Reader
		if r.Method == "POST" {
			payload, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
			if e != nil {
				http.Error(w, "request too large", 413)
				return
			}
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, r.Method, target, body)
		if err != nil {
			http.Error(w, "invalid", 400)
			return
		}
		req.Header.Set("User-Agent", "Weft-repository-import")
		if len(auth) > 0 && auth[0].Mode == "token" {
			req.SetBasicAuth(auth[0].Username, auth[0].Token)
		}
		if r.Method == "POST" {
			req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
			if encoding := r.Header.Get("Content-Encoding"); encoding == "gzip" {
				req.Header.Set("Content-Encoding", encoding)
			}
		}
		if value := r.Header.Get("Git-Protocol"); value == "version=2" {
			req.Header.Set("Git-Protocol", value)
		}
		resp, err := scopedClient.Do(req)
		if err != nil {
			status.Store(502)
			http.Error(w, "repository unavailable", 502)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			status.Store(int32(resp.StatusCode))
			http.Error(w, "repository refused", 502)
			return
		}
		expected := "application/x-git-upload-pack-advertisement"
		if r.Method == "POST" {
			expected = "application/x-git-upload-pack-result"
		}
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), expected) {
			status.Store(415)
			http.Error(w, "smart Git HTTP required", 502)
			return
		}
		w.Header().Set("Content-Type", expected)
		buf := make([]byte, 32<<10)
		for {
			n, e := resp.Body.Read(buf)
			if n > 0 {
				total := received.Add(int64(n))
				if total > limit {
					status.Store(413)
					return
				}
				if _, err = w.Write(buf[:n]); err != nil {
					return
				}
			}
			if e != nil {
				if e != io.EOF {
					status.Store(502)
				}
				return
			}
		}
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	return "http://" + listener.Addr().String() + base, func() { _ = server.Close(); <-done }, status, nil
}
