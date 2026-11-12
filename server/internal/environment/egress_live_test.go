package environment

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestPreparationEgressRealSandbox(t *testing.T) {
	bin, proxyImage := os.Getenv("WEFT_TEST_ENV_DOCKER_BIN"), os.Getenv("WEFT_TEST_EGRESS_PROXY_IMAGE")
	if bin == "" || proxyImage == "" {
		t.Skip("run make test-egress")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("prep-egress-test-%d", time.Now().UnixNano())
	ws := reuseWorkspaces{root: t.TempDir(), prefix: id + "-workspace-"}
	hosts := []string{"example.com", "127.0.0.1.nip.io", "registry-1.docker.io", "auth.docker.io", "production.cloudflare.docker.com", "production.cloudfront.docker.com", "dl-cdn.alpinelinux.org"}
	policy := environmentpolicy.Policy{SetupNetworks: []string{"egress"}, EgressHosts: hosts}
	budget := &sandboxcapacity.Manager{Store: store, Scope: id, Policy: sandboxcapacity.Policy{MaxActive: 3, TotalMemory: "2g", DefaultMemory: "512m", TotalCPU: "4", DefaultCPU: "1"}}
	m, err := New(Options{Store: store, Workspaces: ws, DockerBin: bin, EgressImage: proxyImage, Policy: policy, Capacity: budget})
	if err != nil {
		t.Fatal(err)
	}
	// Curl validates TLS through CONNECT, and uses the same explicit proxy
	// variables as package managers in both Dockerfile RUN and setup.
	proof := `curl -fsS --max-time 20 -o /dev/null http://example.com || exit 1; curl -fsS --max-time 20 -o /dev/null https://example.com || exit 1; if curl -fsS --max-time 5 -o /dev/null http://example.org; then exit 1; fi; if curl -fsS --max-time 5 -o /dev/null http://127.0.0.1.nip.io; then exit 1; fi; if curl -fsS --noproxy '*' --max-time 2 -o /dev/null http://1.1.1.1; then exit 1; fi; if nslookup example.com >/dev/null 2>&1; then exit 1; fi`
	env := &domain.EnvironmentSpec{Dockerfile: "FROM alpine:3.22\nRUN apk add --no-cache curl\nRUN " + proof + " && echo build-ok > /tmp/build-proof\n", Setup: []string{proof, "test $(cat /tmp/build-proof) = build-ok", "echo setup-ok > /tmp/setup-proof"}, Network: domain.EnvironmentNetwork{Setup: "egress", SetupAllowedHosts: hosts}}
	g := graphWith(env)
	g.Metadata.ID = id
	dir, err := ws.Ensure(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	final := ImageRef(id, Hash(env))
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		if err := m.cleanBuildResources(c, id); err != nil {
			t.Error(err)
		}
		for _, resource := range []struct{ list, remove []string }{
			{[]string{"image", "ls", "--format", "{{.Repository}}:{{.Tag}}", final}, []string{"image", "rm", final}},
			{[]string{"volume", "ls", "--filter", "name=^" + ws.HostPath(dir) + "$", "--format", "{{.Name}}"}, []string{"volume", "rm", ws.HostPath(dir)}},
		} {
			var out bytes.Buffer
			if err := m.docker(c, nil, &out, resource.list...); err != nil {
				t.Error(err)
				continue
			}
			if strings.TrimSpace(out.String()) != "" {
				out.Reset()
				if err := m.docker(c, nil, &out, resource.remove...); err != nil {
					t.Errorf("cleanup: %s %v", out.String(), err)
				}
			}
		}
	})
	if err := m.ValidateDefinition(ctx, g, dir); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	image, _, err := m.withBuildCapacity(ctx, g, &log, func(ctx context.Context) (string, []CacheMount, error) {
		result, caches, e := m.steps(ctx, g, Hash(env), &log)
		var output bytes.Buffer
		if le := m.docker(ctx, nil, &output, "logs", preparationProxy(id)); le != nil {
			t.Error(le)
		}
		t.Logf("proxy log: %s", output.String())
		if e == nil {
			output.Reset()
			if stopErr := m.docker(ctx, nil, &output, "stop", preparationProxy(id)); stopErr != nil {
				t.Fatal(stopErr)
			}
			if _, setupErr := m.setupSharedWorkspace(ctx, g, dir, result, nil, &log); setupErr == nil {
				t.Fatal("setup succeeded after proxy stopped")
			}
		}
		return result, caches, e
	})
	if err != nil {
		t.Fatalf("real preparation: %v\n%s", err, log.String())
	}
	status, statusErr := m.ProxyStatus(ctx, id)
	if statusErr != nil || len(status.Proxies) != 0 || status.LastPreparation == nil || len(status.LastPreparation.Requests) == 0 {
		t.Fatal("preparation diagnostics did not survive cleanup", status, statusErr)
	}
	history, historyErr := (&Manager{opt: m.opt}).BlockedHistory(ctx, id)
	if historyErr != nil || len(history.Requests) == 0 {
		t.Fatal("preparation history lost after cleanup", history, historyErr)
	}
	if _, err := m.SaveRetention(ctx, RetentionPolicy{Days: 1, MaxEvents: 1}, 0, "live-test-admin"); err != nil {
		t.Fatal(err)
	}
	history, historyErr = (&Manager{opt: m.opt}).BlockedHistory(ctx, id)
	if historyErr != nil || len(history.Requests) != 1 || history.RetentionDays != 1 || history.Limit != 1 {
		t.Fatal("preparation ignored configured retention", history, historyErr)
	}
	expired := history.Requests[0]
	expired.Time = time.Now().Add(-8 * 24 * time.Hour)
	retained, historyErr := m.updateBlocked(ctx, id, []BlockedRequest{expired}, false)
	if historyErr != nil || len(retained.Requests) != len(history.Requests) {
		t.Fatal("retention failed", retained, historyErr)
	}
	var out bytes.Buffer
	if err := m.docker(ctx, nil, &out, "run", "--rm", "--network", "none", "--entrypoint", "sh", image, "-c", `test "$(cat /tmp/setup-proof)" = setup-ok && test -z "$HTTP_PROXY$HTTPS_PROXY$http_proxy$https_proxy"`); err != nil {
		t.Fatalf("committed image: %s %v", out.String(), err)
	}
	out.Reset()
	if err := m.docker(ctx, nil, &out, "network", "ls", "--filter", "name=^"+preparationNetworkName(id)+"$", "--format", "{{.Name}}"); err != nil || strings.TrimSpace(out.String()) != "" {
		t.Fatalf("network leaked: %s %v", out.String(), err)
	}
	t.Log("real BuildKit RUN and setup allowed approved HTTP, denied other hosts/private DNS/direct egress, and cleared proxy configuration from the resulting image")
}
