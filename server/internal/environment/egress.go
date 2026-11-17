package environment

import (
	"bytes"
	"context"
	"fmt"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
)

const preparationProxyMemory int64 = 64 << 20
const preparationProxyCPU int64 = 250

type preparationKey struct{}
type preparationNetwork struct{ name, proxyURL string }

func preparationProxy(id string) string       { return buildContainer(id) + "-egress" }
func preparationNetworkName(id string) string { return buildContainer(id) + "-network" }

// Only the proxy has an uplink. The BuildKit worker and setup remain on the
// isolated bridge, so ignoring proxy variables never enables direct internet.
func (m *Manager) startPreparationProxy(ctx context.Context, g *domain.Graph, log io.Writer) (context.Context, error) {
	id := g.Metadata.ID
	network, proxy := preparationNetworkName(id), preparationProxy(id)
	if err := m.docker(ctx, nil, log, "network", "create", "--internal", "--opt", "com.docker.network.bridge.gateway_mode_ipv4=isolated", "--opt", "com.docker.network.bridge.gateway_mode_ipv6=isolated", "--label", "weft.environment.build=true", network); err != nil {
		return ctx, fmt.Errorf("create preparation network: %w", err)
	}
	args := []string{"run", "-d", "--name", proxy, "--network", network, "--restart", "no", "--memory", strconv.FormatInt(preparationProxyMemory, 10), "--memory-swap", strconv.FormatInt(preparationProxyMemory, 10), "--cpus", sandboxcapacity.FormatCPU(preparationProxyCPU), "--pids-limit", "128", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "10001:10001", "--workdir", "/", "--log-opt", "max-size=10m", "--log-opt", "max-file=2", "--label", "weft.sandbox=true", "--label", "weft.egress=true", "--label", "weft.graph=" + id, "--label", "weft.egress.phase=preparation", "--label", "weft.capacity-group=" + buildContainer(id), m.opt.EgressImage, "--hosts", strings.Join(g.Spec.Environment.Network.SetupAllowedHosts, ",")}
	if err := m.docker(ctx, nil, log, args...); err != nil {
		return ctx, fmt.Errorf("start preparation proxy: %w", err)
	}
	if err := m.docker(ctx, nil, log, "network", "connect", "bridge", proxy); err != nil {
		return ctx, fmt.Errorf("connect preparation proxy: %w", err)
	}
	var out bytes.Buffer
	if err := m.docker(ctx, nil, &out, "inspect", "--format", `{{(index .NetworkSettings.Networks "`+network+`").IPAddress}}`, proxy); err != nil {
		return ctx, err
	}
	address := strings.TrimSpace(out.String())
	if ip := net.ParseIP(address); ip == nil || ip.To4() == nil {
		return ctx, fmt.Errorf("preparation proxy returned an invalid IPv4 address")
	}
	fmt.Fprintln(log, "==> Preparation egress proxy ready; only approved HTTP/HTTPS hosts are reachable")
	return context.WithValue(ctx, preparationKey{}, preparationNetwork{network, "http://" + address + ":3128"}), nil
}

// The proxy variables are transport configuration, not part of the resulting
// image. Clear them when committing setup; runtime supplies its own policy.
func preparationArgs(ctx context.Context, args []string) ([]string, error) {
	n, ok := ctx.Value(preparationKey{}).(preparationNetwork)
	result := append([]string(nil), args...)
	if len(args) == 0 {
		return result, nil
	}
	if args[0] == "run" {
		for i := 1; i+1 < len(args); i++ {
			if args[i] == "--network" && args[i+1] == "egress" {
				if !ok {
					return nil, fmt.Errorf("preparation egress proxy is not admitted")
				}
				result[i+1] = n.name
				extra := []string{"--dns", "127.0.0.1"}
				for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
					extra = append(extra, "-e", key+"="+n.proxyURL)
				}
				extra = append(extra, "-e", "NO_PROXY=", "-e", "no_proxy=")
				result = append(append([]string{"run"}, extra...), result[1:]...)
				return result, nil
			}
		}
	}
	if ok && args[0] == "commit" {
		result = []string{"commit"}
		for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "NO_PROXY", "no_proxy"} {
			result = append(result, "--change", "ENV "+key+"=")
		}
		result = append(result, args[1:]...)
	}
	return result, nil
}
