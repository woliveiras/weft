// Package egress implements an explicit HTTP proxy with pinned public-IP dials.
// Network isolation around this process is required; proxy variables alone are
// not a security boundary.
package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

var ErrDenied = errors.New("destination denied")

type resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type Proxy struct {
	hosts    map[string]bool
	resolver resolver
	dial     func(context.Context, string, string) (net.Conn, error)
	log      *slog.Logger
	slots    chan struct{}
}

func New(hosts []string, log *slog.Logger) (*Proxy, error) {
	if err := domain.ValidateEgressHosts(hosts); err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("egress proxy requires allowed hosts")
	}
	if log == nil {
		log = slog.Default()
	}
	p := &Proxy{hosts: map[string]bool{}, resolver: net.DefaultResolver, dial: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, log: log, slots: make(chan struct{}, 64)}
	for _, host := range hosts {
		p.hosts[host] = true
	}
	return p, nil
}

var forbidden = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	// Translation/transition ranges must not encode a private IPv4 destination.
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

func public(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range forbidden {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func (p *Proxy) connect(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || !p.hosts[host] || (port != "80" && port != "443") {
		return nil, ErrDenied
	}
	ips, err := p.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("DNS resolution failed")
	}
	// Reject the entire answer if any address is unsafe, then dial an IP literal.
	for _, ip := range ips {
		if !public(ip) {
			return nil, fmt.Errorf("%w: non-public address", ErrDenied)
		}
	}
	var last error
	for _, ip := range ips {
		c, err := p.dial(ctx, "tcp", net.JoinHostPort(ip.String(), port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	if last != nil {
		return nil, fmt.Errorf("upstream unavailable")
	}
	return nil, fmt.Errorf("no destination")
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	start := time.Now()
	outcome, host, reason := "denied", "", "invalid_destination"
	defer func() {
		p.log.Info("egress request", "eventId", domain.NewID("egress"), "host", host, "outcome", outcome, "reason", reason, "durationMs", time.Since(start).Milliseconds())
	}()
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		outcome, reason = "failed", "capacity_exhausted"
		http.Error(w, "proxy capacity exhausted", 503)
		return
	}
	port := "80"
	if r.Method == http.MethodConnect {
		var err error
		host, port, err = net.SplitHostPort(r.Host)
		if err != nil || port != "443" {
			host = ""
			reason = "unsupported_port"
			http.Error(w, "CONNECT requires port 443", 403)
			return
		}
	} else {
		if r.URL.Scheme != "http" || r.URL.User != nil || r.URL.Host == "" || r.Header.Get("Upgrade") != "" {
			reason = "unsupported_protocol"
			http.Error(w, "only HTTP and HTTPS CONNECT are supported", 403)
			return
		}
		host = r.URL.Hostname()
		if r.URL.Port() != "" {
			port = r.URL.Port()
		}
		if port != "80" {
			host = ""
			reason = "unsupported_port"
			http.Error(w, "HTTP requires port 80", 403)
			return
		}
	}
	host = strings.ToLower(host)
	if err := domain.ValidateEgressHosts([]string{host}); err != nil {
		host = ""
		http.Error(w, "invalid destination", 403)
		return
	}
	if !p.hosts[host] {
		reason = "host_not_allowed"
		http.Error(w, "host not allowed", 403)
		return
	}
	if r.Method == http.MethodConnect {
		upstream, err := p.connect(r.Context(), "tcp", net.JoinHostPort(host, port))
		if err != nil {
			status := 502
			outcome, reason = "failed", "upstream_failure"
			if errors.Is(err, ErrDenied) {
				status = 403
				outcome, reason = "denied", "private_address"
			}
			http.Error(w, "upstream unavailable or denied", status)
			return
		}
		defer upstream.Close()
		outcome, reason = "failed", "upstream_failure"
		hj, ok := w.(http.Hijacker)
		if !ok {
			outcome, reason = "failed", "upstream_failure"
			http.Error(w, "tunnel unavailable", 500)
			return
		}
		client, buffer, err := hj.Hijack()
		if err != nil {
			return
		}
		defer client.Close()
		deadline := time.Now().Add(5 * time.Minute)
		_ = client.SetDeadline(deadline)
		_ = upstream.SetDeadline(deadline)
		if _, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		if err = buffer.Flush(); err != nil {
			return
		}
		outcome, reason = "allowed", "permitted"
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, buffer); _ = upstream.Close(); close(done) }()
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
		<-done
		return
	}
	transport := &http.Transport{Proxy: nil, DialContext: p.connect, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second}
	defer transport.CloseIdleConnections()
	proxy := &httputil.ReverseProxy{Transport: transport, Rewrite: func(pr *httputil.ProxyRequest) {
		pr.Out.URL.Scheme = "http"
		pr.Out.URL.Host = net.JoinHostPort(host, port)
		pr.Out.Host = host
		pr.Out.Header.Del("Proxy-Authorization")
		pr.Out.Header.Del("Proxy-Connection")
	}, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
		status := 502
		outcome, reason = "failed", "upstream_failure"
		if errors.Is(err, ErrDenied) {
			status = 403
			outcome, reason = "denied", "private_address"
		}
		http.Error(w, "upstream unavailable or denied", status)
	}}
	outcome, reason = "allowed", "permitted"
	proxy.ServeHTTP(w, r)
}
