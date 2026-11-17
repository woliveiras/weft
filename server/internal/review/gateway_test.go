package review

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type httpDriver struct {
	fakeDriver
	address string
}

func (d *httpDriver) Dial(ctx context.Context, r *Record) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "tcp", d.address)
}
func TestGatewayHTTPIntegration(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(PreviewHeader) != "" || strings.Contains(r.Header.Get("Cookie"), previewCookie) {
			t.Error("gateway credential leaked upstream")
		}
		if r.Header.Get("Authorization") != "Bearer application-token" {
			t.Error("application authorization lost")
		}
		w.Header().Set("Set-Cookie", "app=value; Path=/")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(201)
		_, _ = io.Copy(w, r.Body)
	}))
	defer app.Close()
	m.Driver = &httpDriver{address: strings.TrimPrefix(app.URL, "http://")}
	if e := m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	gateway := &Gateway{Manager: m, BaseURL: "http://localhost:8082", ConsoleOrigins: []string{"http://localhost:8080"}}
	server := httptest.NewServer(gateway)
	defer server.Close()
	token, e := m.Issue(ctx, r.ID, "operator", false)
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path, credential, origin string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader("payload"))
		req.Host = r.ID + ".localhost:8082"
		req.Header.Set(PreviewHeader, credential)
		req.Header.Set("Authorization", "Bearer application-token")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}
	if res := call("POST", "/api/data", token, ""); res.StatusCode != 201 {
		t.Fatal(res.Status)
	} else {
		b, _ := io.ReadAll(res.Body)
		if string(b) != "payload" || res.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatal(string(b), res.Header)
		}
	}
	if res := call("POST", "/api/data", token, "https://attacker.example"); res.StatusCode != 403 {
		t.Fatal("cross origin allowed")
	}
	if res := call("GET", "/", "", ""); res.StatusCode != 401 {
		t.Fatal("anonymous allowed")
	}
	browser, e := m.Issue(ctx, r.ID, "operator", true)
	if e != nil {
		t.Fatal(e)
	}
	exchange, _ := http.NewRequest("POST", server.URL+"/_weft/access", strings.NewReader(url.Values{"token": {browser}}.Encode()))
	exchange.Host = r.ID + ".localhost:8082"
	exchange.Header.Set("Origin", "http://localhost:8080")
	exchange.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(exchange)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 303 || len(res.Cookies()) != 1 || !res.Cookies()[0].HttpOnly {
		t.Fatal(res.Status, res.Header)
	}
	r, _ = m.Get(ctx, r.ID)
	if _, e = m.Decide(ctx, r.ID, "rejected", "managed", "operator", "", int64(r.Generation)); e != nil {
		t.Fatal(e)
	}
	if res := call("GET", "/", token, ""); res.StatusCode != 403 {
		t.Fatal("revoked access accepted")
	}
}
func TestGatewayConfiguration(t *testing.T) {
	for _, tc := range []struct {
		addr, base string
		ok         bool
	}{{"127.0.0.1:8082", "http://localhost:8082", true}, {"0.0.0.0:8082", "http://localhost:8082", false}, {"127.0.0.1:8082", "http://evil.example", false}, {":8082", "https://preview.example", true}, {"127.0.0.1:8082", "http://user:pass@localhost:8082", false}} {
		if e := ValidateGateway(tc.addr, tc.base); (e == nil) != tc.ok {
			t.Fatalf("%+v: %v", tc, e)
		}
	}
}

func TestStreamRevocationIntegration(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, b, e := w.(http.Hijacker).Hijack()
		if e != nil {
			return
		}
		defer c.Close()
		_, _ = b.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		if b.Flush() != nil {
			return
		}
		_, _ = io.Copy(c, c)
	}))
	defer app.Close()
	m.Driver = &httpDriver{address: strings.TrimPrefix(app.URL, "http://")}
	if e := m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	gateway := &Gateway{Manager: m, BaseURL: "http://localhost:8082"}
	srv := httptest.NewServer(gateway)
	defer srv.Close()
	token, e := m.Issue(ctx, r.ID, "operator", false)
	if e != nil {
		t.Fatal(e)
	}
	c, e := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(c, "GET /ws HTTP/1.1\r\nHost: %s.localhost:8082\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n%s: %s\r\n\r\n", r.ID, PreviewHeader, token)
	reader := bufio.NewReader(c)
	req, _ := http.NewRequest("GET", srv.URL+"/ws", nil)
	res, e := http.ReadResponse(reader, req)
	if e != nil {
		t.Fatal(e)
	}
	if res.StatusCode != 101 {
		t.Fatal(res.Status)
	}
	if _, e = c.Write([]byte("ping")); e != nil {
		t.Fatal(e)
	}
	b := make([]byte, 4)
	if _, e = io.ReadFull(reader, b); e != nil || string(b) != "ping" {
		t.Fatal(string(b), e)
	}
	if e = m.Cancel(ctx, "g", "n", "operator"); e != nil {
		t.Fatal(e)
	}
	_, e = reader.ReadByte()
	if e == nil {
		t.Fatal("revoked stream remained open")
	}
	if timeout, ok := e.(net.Error); ok && timeout.Timeout() {
		t.Fatal("revocation did not close stream")
	}
}
