package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

const proxyHistoryNamespace = "environment-proxy"
const proxySampleLimit = 100

var proxyEventID = regexp.MustCompile(`^egress_[a-z0-9]{1,60}$`)

type ProxyRequest struct {
	EventID    string    `json:"eventId,omitempty"`
	Time       time.Time `json:"time"`
	Host       string    `json:"host"`
	Outcome    string    `json:"outcome"`
	Reason     string    `json:"reason"`
	DurationMS int64     `json:"durationMs"`
}
type ProxyStatus struct {
	Name       string         `json:"name"`
	Phase      string         `json:"phase"`
	State      string         `json:"state"`
	Requests   []ProxyRequest `json:"requests"`
	Warning    string         `json:"warning,omitempty"`
	CapturedAt time.Time      `json:"capturedAt"`
}
type ProxyView struct {
	Proxies         []ProxyStatus `json:"proxies"`
	LastPreparation *ProxyStatus  `json:"lastPreparation,omitempty"`
	Truncated       bool          `json:"truncated"`
}

// Docker logs are untrusted input. Only the proxy event schema is exposed;
// URLs, arbitrary text, payloads and raw daemon errors never reach the API.
func parseProxyRequests(data []byte) []ProxyRequest {
	result := []ProxyRequest{}
	for _, line := range bytes.Split(data, []byte("\n")) {
		var raw struct {
			ProxyRequest
			Message string `json:"msg"`
		}
		if json.Unmarshal(line, &raw) != nil || raw.Message != "egress request" || raw.Time.IsZero() || raw.DurationMS < 0 || raw.DurationMS > 24*60*60*1000 {
			continue
		}
		if !proxyEventID.MatchString(raw.EventID) {
			raw.EventID = ""
		}
		switch raw.Outcome {
		case "allowed", "denied", "failed":
		default:
			continue
		}
		if raw.Host != "" && domain.ValidateEgressHosts([]string{raw.Host}) != nil {
			raw.Host = ""
		}
		switch raw.Reason {
		case "permitted", "host_not_allowed", "private_address", "invalid_destination", "unsupported_protocol", "unsupported_port", "capacity_exhausted", "upstream_failure":
		default:
			raw.Reason = "unspecified"
		}
		result = append(result, raw.ProxyRequest)
		if len(result) > proxySampleLimit {
			result = result[1:]
		}
	}
	return result
}

type proxyOutput struct{ bytes.Buffer }

func (b *proxyOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256<<10 {
		return 0, fmt.Errorf("proxy diagnostics exceeded size limit")
	}
	return b.Buffer.Write(p)
}

func (m *Manager) readProxyStatus(ctx context.Context, name, graph string) (ProxyStatus, error) {
	v := ProxyStatus{Name: name, CapturedAt: time.Now().UTC(), Requests: []ProxyRequest{}}
	var out proxyOutput
	// Return a minimal projection, never container env/config or credentials.
	template := `{"managed":{{json (index .Config.Labels "weft.egress")}},"graph":{{json (index .Config.Labels "weft.graph")}},"phase":{{json (index .Config.Labels "weft.egress.phase")}},"state":{{json .State.Status}}}`
	if err := m.docker(ctx, nil, &out, "inspect", "--format", template, name); err != nil {
		return v, fmt.Errorf("proxy inspection unavailable")
	}
	var state struct{ Managed, Graph, Phase, State string }
	if err := json.Unmarshal(out.Bytes(), &state); err != nil || state.Managed != "true" || state.Graph != graph || (state.Phase != "runtime" && state.Phase != "preparation") {
		return v, fmt.Errorf("proxy ownership could not be verified")
	}
	v.Phase = state.Phase
	switch state.State {
	case "created", "running", "paused", "restarting", "removing", "exited", "dead":
		v.State = state.State
	default:
		v.State = "unknown"
	}
	out.Reset()
	if err := m.docker(ctx, nil, &out, "logs", "--tail", "100", "--since", "24h", name); err != nil {
		v.Warning = "Recent requests could not be read"
		return v, nil
	}
	v.Requests = parseProxyRequests(out.Bytes())
	if out.Len() > 0 && len(v.Requests) == 0 {
		v.Warning = "No structured request events found in this sample"
	}
	return v, nil
}

// ProxyStatus is a bounded, read-only snapshot, scoped by graph and checked
// again through container labels before reading any request logs.
func (m *Manager) ProxyStatus(ctx context.Context, graph string) (ProxyView, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	v := ProxyView{Proxies: []ProxyStatus{}}
	var out proxyOutput
	if err := m.docker(ctx, nil, &out, "ps", "-a", "--filter", "label=weft.egress=true", "--filter", "label=weft.graph="+graph, "--format", "{{.Names}}"); err != nil {
		return v, fmt.Errorf("proxy inventory unavailable")
	}
	names := strings.Fields(out.String())
	if len(names) > 32 {
		v.Truncated = true
		names = names[:32]
	}
	for _, name := range names {
		item, err := m.readProxyStatus(ctx, name, graph)
		if err != nil {
			item = ProxyStatus{Name: name, State: "unknown", Requests: []ProxyRequest{}, Warning: err.Error(), CapturedAt: time.Now().UTC()}
		}
		v.Proxies = append(v.Proxies, item)
	}
	data, err := m.opt.Store.KVGet(ctx, proxyHistoryNamespace, graph)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return v, err
	}
	if err == nil {
		var last ProxyStatus
		if err := json.Unmarshal(data, &last); err != nil {
			return v, fmt.Errorf("saved preparation proxy diagnostics unavailable")
		}
		v.LastPreparation = &last
	}
	return v, nil
}
func (m *Manager) capturePreparationProxy(ctx context.Context, graph string) error {
	v, err := m.readProxyStatus(ctx, preparationProxy(graph), graph)
	if err != nil {
		return err
	}
	if err := m.saveBlocked(ctx, graph, []ProxyStatus{v}); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return m.opt.Store.KVPut(ctx, proxyHistoryNamespace, graph, data)
}
