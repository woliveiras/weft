package environmentpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

const NetworkSettingsKey = "environment/network-policy"

// NetworkSettings deliberately excludes feature restrictions owned by server
// configuration. Administrators can manage network policy without changing them.
type NetworkSettings struct {
	SetupNetworks []string `json:"setupNetworks"`
	RunNetworks   []string `json:"runNetworks"`
	EgressHosts   []string `json:"egressHosts"`
}

func (n NetworkSettings) Validate() error {
	if len(n.SetupNetworks) == 0 || len(n.RunNetworks) == 0 {
		return fmt.Errorf("select at least one network for each phase")
	}
	for _, list := range [][]string{n.SetupNetworks, n.RunNetworks} {
		seen := map[string]bool{}
		for _, mode := range list {
			if seen[mode] {
				return fmt.Errorf("duplicate network mode")
			}
			seen[mode] = true
		}
	}
	return (Policy{SetupNetworks: n.SetupNetworks, RunNetworks: n.RunNetworks, EgressHosts: n.EgressHosts}).Validate()
}
func (n NetworkSettings) apply(p Policy) Policy {
	p.SetupNetworks = n.SetupNetworks
	p.RunNetworks = n.RunNetworks
	p.EgressHosts = n.EgressHosts
	return p
}

type SettingsView struct {
	Revision  int64           `json:"revision"`
	Source    string          `json:"source"`
	Network   NetworkSettings `json:"network"`
	UpdatedAt *time.Time      `json:"updatedAt,omitempty"`
	UpdatedBy string          `json:"updatedBy,omitempty"`
}
type Settings struct {
	Store      storage.Store
	Base       Policy
	DefaultRun string
}

func (s Settings) Get(ctx context.Context) (SettingsView, error) {
	if err := s.Base.Validate(); err != nil {
		return SettingsView{}, err
	}
	v := SettingsView{Source: "server", Network: NetworkSettings{SetupNetworks: networks(s.Base.SetupNetworks), RunNetworks: networks(s.Base.RunNetworks), EgressHosts: s.Base.EgressHosts}}
	rec, err := s.Store.GetSetting(ctx, NetworkSettingsKey)
	if errors.Is(err, storage.ErrNotFound) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	v.Network = NetworkSettings{}
	if err = json.Unmarshal(rec.Value, &v.Network); err != nil {
		return SettingsView{}, fmt.Errorf("decode stored network policy: %w", err)
	}
	if err = v.Network.Validate(); err != nil {
		return SettingsView{}, fmt.Errorf("invalid stored network policy: %w", err)
	}
	v.Source = "installation"
	v.Revision = rec.Revision
	v.UpdatedAt = &rec.UpdatedAt
	v.UpdatedBy = rec.UpdatedBy
	return v, nil
}
func (s Settings) Effective(ctx context.Context) (Policy, error) {
	v, err := s.Get(ctx)
	if err != nil {
		return Policy{}, err
	}
	return v.Network.apply(s.Base), nil
}
func (s Settings) Save(ctx context.Context, n NetworkSettings, revision int64, actor string) (SettingsView, error) {
	if revision < 0 {
		return SettingsView{}, fmt.Errorf("invalid revision")
	}
	if err := n.Validate(); err != nil {
		return SettingsView{}, err
	}
	b, err := json.Marshal(n)
	if err != nil {
		return SettingsView{}, err
	}
	next, err := s.Store.PutSetting(ctx, NetworkSettingsKey, b, revision, actor)
	if err != nil {
		return SettingsView{}, err
	}
	// Return the exact acknowledged revision, even if another administrator writes
	// immediately afterwards. The next read observes the latest persisted value.
	now := time.Now().UTC()
	return SettingsView{Revision: next, Source: "installation", Network: n, UpdatedBy: actor, UpdatedAt: &now}, nil
}

type NetworkUsage struct {
	Graph   string            `json:"graph"`
	State   domain.GraphState `json:"state"`
	Hosts   []string          `json:"hosts"`
	Blocked bool              `json:"blocked"`
	Reason  string            `json:"reason,omitempty"`
}
type Preview struct {
	SettingsView
	Usage []NetworkUsage `json:"usage"`
}

func (s Settings) Preview(ctx context.Context, n NetworkSettings, revision int64) (Preview, error) {
	if err := n.Validate(); err != nil {
		return Preview{}, err
	}
	v, err := s.Get(ctx)
	if err != nil {
		return Preview{}, err
	}
	if v.Revision != revision {
		return Preview{}, storage.ErrConflict
	}
	graphs, err := s.Store.ListGraphs(ctx)
	if err != nil {
		return Preview{}, err
	}
	result := Preview{SettingsView: v, Usage: []NetworkUsage{}}
	result.Network = n
	policy := n.apply(s.Base)
	for _, g := range graphs {
		rec, err := s.Store.GetGraphRecord(ctx, g.Metadata.ID)
		if err != nil {
			return Preview{}, err
		}
		usage := NetworkUsage{Graph: g.Metadata.ID, State: rec.State, Hosts: []string{}}
		if e := g.Spec.Environment; e != nil {
			set := map[string]bool{}
			for _, h := range append(append([]string{}, e.Network.AllowedHosts...), e.Network.SetupAllowedHosts...) {
				set[h] = true
			}
			for h := range set {
				usage.Hosts = append(usage.Hosts, h)
			}
			sort.Strings(usage.Hosts)
			if err := policy.Check(e); err != nil {
				usage.Blocked = true
				usage.Reason = err.Error()
			}
		}
		for _, node := range g.Spec.Nodes {
			if node.Isolation.Mode != "docker" {
				continue
			}
			mode := node.Isolation.Network
			if mode == "" && node.Isolation.Image == "" && g.Spec.Environment != nil {
				mode = g.Spec.Environment.Network.Run
			}
			if mode == "" {
				mode = s.DefaultRun
			}
			if err := policy.RunNetwork(mode); err != nil {
				usage.Blocked = true
				usage.Reason = err.Error()
			}
		}
		result.Usage = append(result.Usage, usage)
	}
	sort.Slice(result.Usage, func(i, j int) bool { return result.Usage[i].Graph < result.Usage[j].Graph })
	return result, nil
}
