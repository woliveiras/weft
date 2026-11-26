package agentcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/storage"
)

const prefix = "agents/catalog/"
const workflowAgentPrefix = "workflow-agent-"

var (
	idPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	ErrInvalid = errors.New("invalid agent definition")
)

type Version struct {
	Version    int                    `json:"version"`
	Definition domain.AgentDefinition `json:"definition"`
	CreatedAt  time.Time              `json:"createdAt"`
	CreatedBy  string                 `json:"createdBy"`
	Operation  string                 `json:"operation,omitempty"`
}

type Origin struct {
	Workflow string `json:"workflow"`
	Node     string `json:"node"`
}

type Agent struct {
	ID       string    `json:"id"`
	Versions []Version `json:"versions"`
	Origin   *Origin   `json:"origin,omitempty"`
	Revision int64     `json:"-"`
}

type Summary struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Latest      int     `json:"latest"`
	Origin      *Origin `json:"origin,omitempty"`
}

type Manager struct{ Store storage.Store }

func decode(s *storage.Setting) (Agent, error) {
	var a Agent
	if err := json.Unmarshal(s.Value, &a); err != nil {
		return a, fmt.Errorf("decode agent catalog entry: %w", err)
	}
	if len(a.Versions) == 0 || a.ID != strings.TrimPrefix(s.Key, prefix) {
		return a, errors.New("invalid stored agent catalog entry")
	}
	a.Revision = s.Revision
	return a, nil
}

func (m Manager) Get(ctx context.Context, id string) (Agent, error) {
	if !idPattern.MatchString(id) {
		return Agent{}, storage.ErrNotFound
	}
	s, err := m.Store.GetSetting(ctx, prefix+id)
	if err != nil {
		return Agent{}, err
	}
	return decode(s)
}

func (m Manager) List(ctx context.Context) ([]Summary, error) {
	items, err := m.Store.ListSettings(ctx, prefix)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(items))
	for _, item := range items {
		a, err := decode(item)
		if err != nil {
			return nil, err
		}
		latest := a.Versions[len(a.Versions)-1]
		out = append(out, Summary{ID: a.ID, Name: latest.Definition.Name, Description: latest.Definition.Description, Latest: latest.Version, Origin: a.Origin})
	}
	return out, nil
}

func (m Manager) Publish(ctx context.Context, id string, expected int64, d domain.AgentDefinition, actor string) (Agent, error) {
	return m.publish(ctx, id, expected, d, actor, "")
}

// CreateForOperation records a server-owned operation ID atomically with the
// first version so the caller can reconcile a lost response without republishing.
func (m Manager) CreateForOperation(ctx context.Context, id string, d domain.AgentDefinition, actor, operation string) (Agent, error) {
	if operation == "" {
		return Agent{}, fmt.Errorf("%w: missing creation operation", ErrInvalid)
	}
	return m.publish(ctx, id, 0, d, actor, operation)
}

// PublishForOperation binds the new version to its reviewed operation in the
// same compare-and-set write that appends it to the catalog.
func (m Manager) PublishForOperation(ctx context.Context, id string, expected int64, d domain.AgentDefinition, actor, operation string) (Agent, error) {
	if operation == "" || expected < 1 {
		return Agent{}, fmt.Errorf("%w: publication requires an existing catalog revision and operation", ErrInvalid)
	}
	return m.publish(ctx, id, expected, d, actor, operation)
}

func (m Manager) publish(ctx context.Context, id string, expected int64, d domain.AgentDefinition, actor, operation string) (Agent, error) {
	switch {
	case id == "new" || id == "source" || (expected == 0 && strings.HasPrefix(id, workflowAgentPrefix)):
		return Agent{}, fmt.Errorf("%w: agent ID is reserved", ErrInvalid)
	case !idPattern.MatchString(id):
		return Agent{}, fmt.Errorf("%w: use a lowercase ID with letters, numbers and hyphens", ErrInvalid)
	}
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || len(d.Name) > 200 {
		return Agent{}, fmt.Errorf("%w: name must contain 1 to 200 bytes", ErrInvalid)
	}
	a := Agent{ID: id}
	if expected > 0 {
		current, err := m.Store.GetSetting(ctx, prefix+id)
		if err != nil {
			return a, err
		}
		if current.Revision != expected {
			return a, storage.ErrConflict
		}
		a, err = decode(current)
		if err != nil {
			return a, err
		}
	}
	a.Versions = append(a.Versions, Version{Version: len(a.Versions) + 1, Definition: d, CreatedAt: time.Now().UTC(), CreatedBy: actor, Operation: operation})
	raw, err := json.Marshal(a)
	if err != nil {
		return a, err
	}
	a.Revision, err = m.Store.PutSetting(ctx, prefix+id, raw, expected, actor)
	return a, err
}

// ApplyDefinition updates only shared fields. The node keeps its local
// topology, limits and approval policy, and can require a read-only workspace.
func ApplyDefinition(n *domain.Node, d domain.AgentDefinition) {
	n.Name, n.Description = d.Name, d.Description
	n.Runtime, n.Model, n.Prompt = d.Runtime, d.Model, d.Prompt
	n.Tools, n.Isolation = d.Tools, d.Isolation
	n.SkillIDs, n.RepositoryGuidance = d.SkillIDs, d.RepositoryGuidance
	n.Workspace.ReadOnly = n.Workspace.ReadOnly || d.ReadOnly
}

// FromNode promotes the effective settings from the saved source version.
// Only parameters used in that node's prompt are copied.
func FromNode(g *domain.Graph, n *domain.Node) domain.AgentDefinition {
	d := domain.AgentDefinition{
		Name: n.DisplayName(), Description: n.Description, Runtime: n.Runtime, Model: n.Model,
		Prompt: n.Prompt, Tools: n.Tools, Isolation: n.Isolation, ReadOnly: n.Workspace.ReadOnly,
		SkillIDs: n.SkillIDs, RepositoryGuidance: n.RepositoryGuidance,
	}
	used := make(map[string]bool)
	for _, tok := range graph.PromptTokens(n.Prompt) {
		used[tok] = true
	}
	for _, p := range g.Spec.Parameters {
		if used[p.Name] {
			d.Parameters = append(d.Parameters, domain.AgentParameter{Name: p.Name, Description: p.Description, Default: p.Value})
		}
	}
	return d
}

func parameterValues(params []domain.AgentParameter, input map[string]string) (map[string]string, error) {
	values := make(map[string]string, len(params))
	for _, p := range params {
		v, supplied := input[p.Name]
		if !supplied {
			v = p.Default
		}
		if p.Required && strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("parameter %q is required", p.Name)
		}
		if len(v) > 16<<10 {
			return nil, fmt.Errorf("parameter %q exceeds 16 KiB", p.Name)
		}
		values[p.Name] = v
	}
	for name := range input {
		if _, ok := values[name]; !ok {
			return nil, fmt.Errorf("parameter %q is not declared in this version", name)
		}
	}
	return values, nil
}

func (m Manager) Resolve(ctx context.Context, g *domain.Graph) ([]graph.Issue, error) {
	catalog := make(map[string]Agent)
	var issues []graph.Issue
	for i := range g.Spec.Nodes {
		n := &g.Spec.Nodes[i]
		binding := n.Agent
		if binding == nil {
			continue
		}
		path := fmt.Sprintf("spec.nodes[%d].agent", i)
		a, ok := catalog[binding.ID]
		if !ok {
			var err error
			a, err = m.Get(ctx, binding.ID)
			if errors.Is(err, storage.ErrNotFound) {
				issues = append(issues, graph.Issue{Path: path + ".id", Message: "agent definition not found"})
				continue
			}
			if err != nil {
				return nil, err
			}
			catalog[binding.ID] = a
		}
		if binding.Version <= 0 || binding.Version > len(a.Versions) || a.Versions[binding.Version-1].Version != binding.Version {
			issues = append(issues, graph.Issue{Path: path + ".version", Message: "select an existing agent version"})
			continue
		}
		definition := a.Versions[binding.Version-1].Definition
		values, err := parameterValues(definition.Parameters, binding.Parameters)
		if err != nil {
			issues = append(issues, graph.Issue{Path: path + ".parameters", Message: err.Error()})
			continue
		}
		binding.Parameters = values
		ApplyDefinition(n, definition)
	}
	return issues, nil
}
