package api_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/environment"
)

func TestEnvironmentProxyDiagnosticsAPI(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "docker")
	script := `#!/bin/sh
case "$1" in
ps) echo managed-proxy;;
inspect) echo '{"managed":"true","graph":"proxy-project","phase":"runtime","state":"exited"}';;
logs) echo '{"msg":"egress request","time":"2026-10-16T12:00:00Z","host":"example.org","reason":"host_not_allowed","outcome":"denied","durationMs":0,"private":"never expose"}';;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	srv, _ := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	doc := "apiVersion: weft.io/v1\nkind: Graph\nmetadata: {name: proxy-project}\nspec:\n  nodes: [{id: worker, runtime: {kind: echo}}]\n"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
		t.Fatal(code)
	}
	for _, token := range []string{viewerTok, operatorTok, adminTok} {
		var history environment.BlockedHistory
		if code := call(t, srv, "GET", "/api/v1/graphs/proxy-project/environment/blocked-history", token, "", "", &history); code != 200 || history.Limit != 1000 || history.RetentionDays != 7 {
			t.Fatal(code, history)
		}
	}
	if code := call(t, srv, "GET", "/api/v1/graphs/missing/environment/blocked-history", viewerTok, "", "", nil); code != 404 {
		t.Fatal(code)
	}
	if code := call(t, srv, "GET", "/api/v1/graphs/proxy-project/environment/blocked-history", "", "", "", nil); code != 401 {
		t.Fatal(code)
	}
	for _, token := range []string{viewerTok, operatorTok, adminTok} {
		var view environment.ProxyView
		if code := call(t, srv, "GET", "/api/v1/graphs/proxy-project/environment/proxies", token, "", "", &view); code != 200 || len(view.Proxies) != 1 || view.Proxies[0].State != "exited" || len(view.Proxies[0].Requests) != 1 {
			t.Fatal(code, view)
		}
	}
	if code := call(t, srv, "GET", "/api/v1/graphs/missing/environment/proxies", viewerTok, "", "", nil); code != 404 {
		t.Fatal(code)
	}
	if code := call(t, srv, "GET", "/api/v1/graphs/proxy-project/environment/proxies", "", "", "", nil); code != 401 {
		t.Fatal(code)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "GET", "/api/v1/graphs/proxy-project/environment/blocked-history", viewerTok, "", "", nil); code != 200 {
		t.Fatal(code)
	}
	if code := call(t, srv, "GET", "/api/v1/graphs/proxy-project/environment/proxies", viewerTok, "", "", nil); code != 500 {
		t.Fatal(code)
	}
}
