package review

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/woliveiras/weft/server/internal/observability"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

const PreviewHeader = "X-Weft-Preview-Token"
const previewCookie = "weft_preview"

type Gateway struct {
	Manager        *Manager
	BaseURL        string
	ConsoleOrigins []string
	slots          sync.Map
}

func (g *Gateway) URL(id string) string {
	u, e := url.Parse(g.BaseURL)
	if e != nil {
		return ""
	}
	host := id + "." + u.Hostname()
	if u.Port() != "" {
		host = net.JoinHostPort(host, u.Port())
	}
	u.Host = host
	return strings.TrimRight(u.String(), "/")
}
func (g *Gateway) session(r *http.Request) (string, error) {
	base, e := url.Parse(g.BaseURL)
	if e != nil {
		return "", e
	}
	host := r.Host
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	suffix := "." + base.Hostname()
	if !strings.HasSuffix(host, suffix) {
		return "", ErrInvalid
	}
	id := strings.TrimSuffix(host, suffix)
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		return "", ErrInvalid
	}
	expected, _ := url.Parse(g.URL(id))
	if r.Host != expected.Host {
		return "", ErrInvalid
	}
	return id, nil
}
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	id, e := g.session(r)
	if e != nil {
		http.Error(w, "Unknown preview", http.StatusNotFound)
		return
	}
	if r.URL.Path == "/_weft/connect" {
		g.connect(w, r, id)
		return
	}
	if r.URL.Path == "/_weft/access" {
		g.exchange(w, r, id)
		return
	}
	token := r.Header.Get(PreviewHeader)
	cookieAuth := token == ""
	if cookieAuth {
		if c, e := r.Cookie(previewCookie); e == nil {
			token = c.Value
		}
	}
	if token == "" {
		http.Error(w, "Open this preview from Weft or supply a preview credential", http.StatusUnauthorized)
		return
	}
	record, e := g.Manager.Authorize(r.Context(), id, token, false)
	if e != nil {
		observability.PreviewAccess.WithLabelValues("denied").Inc()
		http.Error(w, "Preview access expired or denied", http.StatusForbidden)
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" && origin != g.URL(id) {
		http.Error(w, "Cross-origin preview access denied", http.StatusForbidden)
		return
	}
	if cookieAuth && r.Method != "GET" && r.Method != "HEAD" && origin != g.URL(id) {
		http.Error(w, "Preview origin required", http.StatusForbidden)
		return
	}
	if cookieAuth && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "Cross-site preview access denied", http.StatusForbidden)
		return
	}
	raw, _ := g.slots.LoadOrStore(id, make(chan struct{}, 32))
	slot := raw.(chan struct{})
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	default:
		http.Error(w, "Preview connection limit reached", 429)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	// Revalidate open streams; revocation also closes hijacked WebSockets.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, e := g.Manager.Authorize(ctx, id, token, false); e != nil {
					cancel()
					return
				}
			}
		}
	}()
	r = r.WithContext(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	transport := &http.Transport{DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second, DialContext: func(c context.Context, _, _ string) (net.Conn, error) {
		conn, e := g.Manager.Driver.Dial(c, record)
		if e == nil {
			context.AfterFunc(ctx, func() { _ = conn.Close() })
		}
		return conn, e
	}}
	defer transport.CloseIdleConnections()
	proxy := &httputil.ReverseProxy{Transport: transport, Rewrite: func(pr *httputil.ProxyRequest) {
		pr.Out.URL.Scheme = "http"
		pr.Out.URL.Host = "preview"
		pr.Out.Host = r.Host
		pr.Out.Header.Del(PreviewHeader)
		pr.Out.Header.Del("Proxy-Authorization")
		pr.Out.Header.Del("Cookie")
		for _, c := range pr.In.Cookies() {
			if c.Name != previewCookie {
				pr.Out.AddCookie(c)
			}
		}
		pr.Out.Header.Set("X-Forwarded-Proto", strings.SplitN(g.BaseURL, ":", 2)[0])
		pr.Out.Header.Set("X-Forwarded-Host", r.Host)
	}, ModifyResponse: func(res *http.Response) error {
		cookies := res.Cookies()
		res.Header.Del("Set-Cookie")
		for _, c := range cookies {
			if c.Name != previewCookie {
				c.Domain = ""
				res.Header.Add("Set-Cookie", c.String())
			}
		}
		res.Header.Del("Access-Control-Allow-Origin")
		res.Header.Del("Access-Control-Allow-Credentials")
		res.Header.Set("Referrer-Policy", "no-referrer")
		res.Header.Set("Cache-Control", "no-store")
		return nil
	}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, e error) {
		http.Error(w, "Preview application is unavailable", http.StatusBadGateway)
	}}
	started := time.Now()
	observability.PreviewAccess.WithLabelValues("authorized").Inc()
	proxy.ServeHTTP(w, r)
	observability.PreviewRequestSeconds.Observe(time.Since(started).Seconds())
}
func (g *Gateway) exchange(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	allowed := r.Header.Get("Origin") == g.URL(id)
	for _, o := range g.ConsoleOrigins {
		if o == r.Header.Get("Origin") {
			allowed = true
		}
	}
	if !allowed {
		http.Error(w, "Console origin denied", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if e := r.ParseForm(); e != nil {
		http.Error(w, "Invalid exchange", 400)
		return
	}
	tok := r.PostForm.Get("token")
	if _, e := g.Manager.Authorize(r.Context(), id, tok, true); e != nil {
		http.Error(w, "Exchange expired or already used", 403)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: previewCookie, Value: tok, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(g.BaseURL, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: 900})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func ValidateGateway(addr, base string) error {
	u, e := url.Parse(base)
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("invalid preview base URL")
	}
	host, _, e := net.SplitHostPort(addr)
	if e != nil {
		return e
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("HTTP preview listener must bind loopback; use TLS for remote access")
		}
		if u.Hostname() != "localhost" {
			return fmt.Errorf("local HTTP preview base must use localhost")
		}
	} else if u.Scheme != "https" {
		return fmt.Errorf("preview requires HTTP loopback or HTTPS")
	}
	return nil
}

// The handshake page receives a one-use credential only from the configured
// console origin. It exchanges it same-origin so browser referrer policies and
// third-party cookie blocking do not require weakening origin validation.
func (g *Gateway) connect(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != "GET" {
		http.Error(w, "GET required", 405)
		return
	}
	origins, _ := json.Marshal(g.ConsoleOrigins)
	script := `const origins=` + string(origins) + `;window.addEventListener('message',async event=>{if(event.source!==window.opener||!origins.includes(event.origin)||event.data?.type!=='weft-preview-access')return;const token=event.data.token;window.opener=null;try{const response=await fetch('/_weft/access',{method:'POST',body:new URLSearchParams({token})});if(!response.ok)throw new Error('Preview access denied');location.replace('/');}catch(e){document.body.textContent=e.message;}});for(const origin of origins){window.opener?.postMessage({type:'weft-preview-ready'},origin);}`
	hash := sha256.Sum256([]byte(script))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
	fmt.Fprintf(w, "<!doctype html><title>Open preview</title><body>Connecting to your preview…<script>%s</script>", script)
}
