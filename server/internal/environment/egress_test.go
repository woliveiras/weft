package environment

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
)

func TestPreparationArguments(t *testing.T) {
	if _, err := preparationArgs(context.Background(), []string{"run", "--network", "egress", "alpine"}); err == nil {
		t.Fatal("unadmitted egress accepted")
	}
	ctx := context.WithValue(context.Background(), preparationKey{}, preparationNetwork{"internal-test", "http://172.20.0.2:3128"})
	args, err := preparationArgs(ctx, []string{"run", "--network", "egress", "alpine"})
	joined := strings.Join(args, " ")
	if err != nil || !strings.Contains(joined, "--network internal-test") || !strings.Contains(joined, "--dns 127.0.0.1") || !strings.Contains(joined, "HTTPS_PROXY=http://172.20.0.2:3128") {
		t.Fatal(joined, err)
	}
	args, err = preparationArgs(ctx, []string{"commit", "worker", "image"})
	if err != nil || len(args) != 15 || args[len(args)-2] != "worker" || args[len(args)-1] != "image" {
		t.Fatal(args, err)
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "NO_PROXY", "no_proxy"} {
		if !strings.Contains(strings.Join(args, " "), "--change ENV "+key+"=") {
			t.Fatal(args)
		}
	}
	untouched := []string{"run", "--network", "none", "alpine"}
	got, err := preparationArgs(ctx, untouched)
	if err != nil || strings.Join(got, " ") != strings.Join(untouched, " ") {
		t.Fatal(got, err)
	}
}

func preparationFixture(t *testing.T) (*harness, *domain.Graph) {
	h := capacityHarness(t)
	h.m.opt.Capacity.Policy.MaxActive = 2
	h.m.opt.Capacity.Policy.TotalMemory = "256m"
	h.m.opt.Policy = environmentpolicy.Policy{SetupNetworks: []string{"egress"}, EgressHosts: []string{"example.com"}}
	h.docker.set(t, "proxy-address", "172.20.0.2")
	g := graphWith(&domain.EnvironmentSpec{Dockerfile: "FROM alpine\nRUN echo test", Setup: []string{"echo setup"}, Network: domain.EnvironmentNetwork{Setup: "egress", SetupAllowedHosts: []string{"example.com"}}})
	return h, g
}

func TestPreparationEgressPipelineIntegration(t *testing.T) {
	for _, fail := range []string{"", "network-create", "network-connect", "run", "inspect", "buildx-create", "buildx-build"} {
		t.Run(fail, func(t *testing.T) {
			h, g := preparationFixture(t)
			if fail != "" {
				h.docker.set(t, "fail-"+fail, "")
			}
			if _, err := h.m.Build(context.Background(), g, false, "developer"); err != nil {
				t.Fatal(err)
			}
			v := h.wait(t, g)
			if fail != "" {
				if v.State != StateFailed {
					t.Fatalf("dependency failure lost: %+v", v)
				}
				return
			}
			if v.State != StateReady {
				t.Fatalf("%+v", v)
			}
			calls := strings.Join(h.docker.calls(t), "\n")
			for _, want := range []string{"gateway_mode_ipv4=isolated", "network connect bridge", "--read-only", "--buildkitd-config /dev/stdin", "network=weft-envbuild-bookaneer-network", "--build-arg HTTPS_PROXY=http://172.20.0.2:3128", "--dns 127.0.0.1", "--change ENV HTTP_PROXY="} {
				if !strings.Contains(calls, want) {
					t.Fatalf("missing %s: %s", want, calls)
				}
			}
		})
	}
}

func TestPreparationHostHashAndPolicy(t *testing.T) {
	h, g := preparationFixture(t)
	e := g.Spec.Environment
	original := Hash(e)
	e.Network.AllowedHosts = []string{"runtime.example"}
	if Hash(e) != original {
		t.Fatal("runtime hosts changed build hash")
	}
	e.Network.AllowedHosts = nil
	e.Network.SetupAllowedHosts = []string{"other.example", "example.com"}
	changed := Hash(e)
	if changed == original {
		t.Fatal("preparation hosts did not invalidate build")
	}
	e.Network.SetupAllowedHosts = []string{"example.com", "other.example"}
	if Hash(e) != changed {
		t.Fatal("host ordering changed hash")
	}
	if err := h.m.ValidatePolicy(context.Background(), e); err == nil {
		t.Fatal("installation ceiling bypassed")
	}
	e.Network.SetupAllowedHosts = nil
	if err := h.m.ValidatePolicy(context.Background(), e); err == nil {
		t.Fatal("empty hosts accepted")
	}
	e.Network.SetupAllowedHosts = []string{"example.com"}
	h.m.opt.Capacity = nil
	if err := h.m.ValidatePolicy(context.Background(), e); err == nil {
		t.Fatal("unbounded egress accepted")
	}
}

func TestPreparationCapacityIncludesProxy(t *testing.T) {
	h, g := preparationFixture(t)
	h.m.opt.Capacity.Policy.MaxActive = 1
	called := false
	run := func(context.Context) (string, []CacheMount, error) { called = true; return "", nil, nil }
	if _, _, err := h.m.withBuildCapacity(context.Background(), g, io.Discard, run); err == nil || called {
		t.Fatal("proxy omitted from count budget")
	}
	h.m.opt.Capacity.Policy.MaxActive = 2
	h.m.opt.Capacity.Policy.TotalMemory = "64m"
	if _, _, err := h.m.withBuildCapacity(context.Background(), g, io.Discard, run); err == nil || called {
		t.Fatal("proxy omitted from memory budget")
	}
	h.m.opt.Capacity.Policy.TotalMemory = "256m"
	h.m.opt.Capacity.Policy.TotalCPU = "1"
	if _, _, err := h.m.withBuildCapacity(context.Background(), g, io.Discard, run); err == nil || called {
		t.Fatal("proxy omitted from CPU budget")
	}
	h.m.opt.Capacity.Policy.TotalCPU = "4"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	h.docker.set(t, "proxy-address", "not-an-ip")
	if _, _, err := h.m.withBuildCapacity(ctx, g, io.Discard, run); err == nil || called {
		t.Fatal("invalid proxy address admitted")
	}
}

func TestPreparationSharedSetupIntegration(t *testing.T) {
	h, g := preparationFixture(t)
	ctx := context.Background()
	_, _, err := h.m.withBuildCapacity(ctx, g, io.Discard, func(ctx context.Context) (string, []CacheMount, error) {
		image, e := h.m.setupSharedWorkspace(ctx, g, h.ws, "prepared", nil, io.Discard)
		return image, nil, e
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(h.docker.calls(t), "\n")
	for _, want := range []string{"--network weft-envbuild-bookaneer-network", "--dns 127.0.0.1", "--change ENV HTTPS_PROXY="} {
		if !strings.Contains(calls, want) {
			t.Fatal(calls)
		}
	}
}

func TestPreparationCleanupFailureRetainsCapacity(t *testing.T) {
	h, g := preparationFixture(t)
	ctx := context.Background()
	_, _, err := h.m.withBuildCapacity(ctx, g, io.Discard, func(context.Context) (string, []CacheMount, error) {
		h.docker.set(t, "containers", "owned-proxy")
		h.docker.set(t, "fail-rm", "")
		return "image", nil, nil
	})
	if err == nil {
		t.Fatal("cleanup failure hidden")
	}
	blocked, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	if err := h.m.opt.Capacity.ReserveCPU(blocked, "another", 64<<20, 250, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); err == nil {
		t.Fatal("failed cleanup released the durable reservation")
	}
	wait, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, _, err = h.m.withBuildCapacity(wait, g, io.Discard, func(context.Context) (string, []CacheMount, error) {
		t.Fatal("admitted despite failed cleanup")
		return "", nil, nil
	})
	if err == nil {
		t.Fatal("failed cleanup admitted another build")
	}
}

func TestPreparationCancellationReleasesCapacity(t *testing.T) {
	h, g := preparationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, _, err := h.m.withBuildCapacity(ctx, g, io.Discard, func(ctx context.Context) (string, []CacheMount, error) { cancel(); return "", nil, ctx.Err() })
	if err == nil {
		t.Fatal("cancelled pipeline succeeded")
	}
	_, _, err = h.m.withBuildCapacity(context.Background(), g, io.Discard, func(context.Context) (string, []CacheMount, error) { return "image", nil, nil })
	if err != nil {
		t.Fatalf("cancelled pipeline retained capacity after successful cleanup: %v", err)
	}
}
