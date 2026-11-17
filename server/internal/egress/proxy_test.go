package egress

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(c context.Context, n, h string) ([]netip.Addr, error) {
	return f(c, n, h)
}
func testProxy(t *testing.T) *Proxy {
	t.Helper()
	p, err := New([]string{"example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPublicAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "0.0.0.0", "198.18.0.1", "192.0.2.1", "224.0.0.1", "255.255.255.255", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "64:ff9b::a00:1", "2002:a00:1::1", "2001:db8::1"} {
		if public(netip.MustParseAddr(ip)) {
			t.Errorf("unsafe %s", ip)
		}
	}
	for _, ip := range []string{"93.184.215.14", "2606:4700:4700::1111"} {
		if !public(netip.MustParseAddr(ip)) {
			t.Errorf("public %s rejected", ip)
		}
	}
}
func TestDNSPinningAndFailures(t *testing.T) {
	p := testProxy(t)
	calls := 0
	p.dial = func(_ context.Context, _ string, address string) (net.Conn, error) {
		calls++
		if address != "93.184.215.14:443" {
			t.Fatalf("unvalidated dial %s", address)
		}
		return nil, errors.New("offline")
	}
	p.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14"), netip.MustParseAddr("10.0.0.1")}, nil
	})
	if _, err := p.connect(context.Background(), "tcp", "example.com:443"); !errors.Is(err, ErrDenied) || calls != 0 {
		t.Fatal("mixed DNS answer accepted", err, calls)
	}
	p.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
	})
	if _, err := p.connect(context.Background(), "tcp", "example.com:443"); err == nil || calls != 1 {
		t.Fatal(err, calls)
	}
	p.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return nil, errors.New("DNS offline") })
	if _, err := p.connect(context.Background(), "tcp", "example.com:443"); err == nil || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestHTTPAndConnectIntegration(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credential forwarded")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://forbidden.example/secret", 302)
			return
		}
		_, _ = io.WriteString(w, "upstream-ok")
	})
	plain := httptest.NewServer(upstream)
	defer plain.Close()
	secure := httptest.NewTLSServer(upstream)
	defer secure.Close()
	p := testProxy(t)
	p.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
	})
	p.dial = func(ctx context.Context, n, address string) (net.Conn, error) {
		if address != "93.184.215.14:80" && address != "93.184.215.14:443" {
			t.Errorf("unvalidated dial %s", address)
		}
		dest := plain.Listener.Addr().String()
		if strings.HasSuffix(address, ":443") {
			dest = secure.Listener.Addr().String()
		}
		return (&net.Dialer{}).DialContext(ctx, n, dest)
	}
	var logs bytes.Buffer
	p.log = slog.New(slog.NewJSONHandler(&logs, nil))
	proxy := httptest.NewServer(p)
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	roots := x509.NewCertPool()
	roots.AddCert(secure.Certificate())
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	for _, scheme := range []string{"http", "https"} {
		resp, err := client.Get(scheme + "://example.com/private?token=secret")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || string(data) != "upstream-ok" {
			t.Fatal(resp.Status, string(data))
		}
	}
	resp, err := client.Get("http://example.com/redirect")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("redirect escaped allowlist", resp.Status)
	}
	// The HTTP path logs synchronously. Tunnel close logs after the client closes.
	transport.CloseIdleConnections()
	// Log validation uses a separate synchronous HTTP handler below to avoid a concurrent buffer read.
}

func TestRejectedRequestsAndSafeLogs(t *testing.T) {
	p := testProxy(t)
	var logs bytes.Buffer
	p.log = slog.New(slog.NewJSONHandler(&logs, nil))
	for _, target := range []string{"http://forbidden.example/private?token=secret", "http://user:password@example.com/", "http://example.com:8080/", "http://127.0.0.1/", "ftp://example.com/"} {
		w := httptest.NewRecorder()
		p.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
		if w.Code != 403 {
			t.Fatal(target, w.Code)
		}
	}
	for _, target := range []string{"example.com:22", "127.0.0.1:443", "other.example:443"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("CONNECT", "http://"+target, nil)
		r.Host = target
		p.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(target, w.Code)
		}
	}
	if strings.Contains(logs.String(), "secret") || strings.Contains(logs.String(), "password") || strings.Contains(logs.String(), "private?") {
		t.Fatal("sensitive URL logged")
	}
	if !strings.Contains(logs.String(), `"outcome":"denied"`) {
		t.Fatal("missing denial event")
	}
	for range cap(p.slots) {
		p.slots <- struct{}{}
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest("GET", "http://example.com", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestAllowedHostResolvingPrivatelyIsDenied(t *testing.T) {
	p := testProxy(t)
	p.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	p.dial = func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("private address dialed")
		return nil, nil
	}
	for _, method := range []string{"GET", "CONNECT"} {
		r := httptest.NewRequest(method, "http://example.com", nil)
		if method == "CONNECT" {
			r.Host = "example.com:443"
		}
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(method, w.Code)
		}
	}
}

func TestDenialReasonsAreSafeStructuredEvents(t *testing.T) {
	var log bytes.Buffer
	p, err := New([]string{"example.com"}, slog.New(slog.NewJSONHandler(&log, nil)))
	if err != nil {
		t.Fatal(err)
	}
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://forbidden.example/private?secret=token", nil))
	if !strings.Contains(log.String(), `"reason":"host_not_allowed"`) || strings.Contains(log.String(), "secret") || strings.Contains(log.String(), "private?") {
		t.Fatal(log.String())
	}
	log.Reset()
	p.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://example.com", nil))
	if !strings.Contains(log.String(), `"reason":"private_address"`) {
		t.Fatal(log.String())
	}
}
