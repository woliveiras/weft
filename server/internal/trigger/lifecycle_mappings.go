package trigger

import (
	"context"
	"fmt"

	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
)

// MappingPin selects an explicit immutable review of an independently managed
// definition. Empty pins preserve the existing comment/label lifecycle.
type MappingPin struct {
	Name     string `json:"name"`
	Revision int64  `json:"revision"`
}
type LifecycleMappings struct {
	WorkStarted MappingPin `json:"workStarted,omitzero"`
	PRDelivered MappingPin `json:"prDelivered,omitzero"`
}

func (v LifecycleMappings) empty() bool { return v == (LifecycleMappings{}) }
func (v LifecycleMappings) pin(stage string) MappingPin {
	switch stage {
	case "work_started":
		return v.WorkStarted
	case "pr_delivered":
		return v.PRDelivered
	}
	return MappingPin{}
}
func (v LifecycleMappings) validate(source SourceConfig) error {
	if v.empty() {
		return nil
	}
	if source.Kind != "jira-dc" {
		return fmt.Errorf("lifecycle mappings require a native Jira Data Center source")
	}
	for _, p := range []MappingPin{v.WorkStarted, v.PRDelivered} {
		if p == (MappingPin{}) {
			continue
		}
		if !connRe.MatchString(p.Name) || p.Revision < 1 {
			return fmt.Errorf("select a mapping name and positive revision together")
		}
	}
	return nil
}

// Validate only when a person reviews consent. A definition changed later
// invalidates its dispatch, not the independent task/PR delivery authority.
func (m Manager) reviewLifecycleMappings(ctx context.Context, in Input) error {
	if err := in.LifecycleMappings.validate(in.Source); err != nil {
		return err
	}
	if in.LifecycleMappings.empty() {
		return nil
	}
	if m.NativeConnections == nil {
		return fmt.Errorf("native Jira connections are unavailable")
	}
	c, err := m.NativeConnections.Get(ctx, in.Source.ConnectionID)
	if err != nil {
		return err
	}
	for _, p := range []MappingPin{in.LifecycleMappings.WorkStarted, in.LifecycleMappings.PRDelivered} {
		if p == (MappingPin{}) {
			continue
		}
		v, err := (jiradc.WorkflowMappings{Connections: *m.NativeConnections}).Get(ctx, p.Name)
		if err != nil {
			return err
		}
		if !v.Enabled || v.Revision != p.Revision || v.Connection != c.Name || v.ConnectionRevision != c.Revision || v.SiteURL != in.Source.SiteURL || v.ProjectID != in.Source.ProjectID {
			return fmt.Errorf("%w: select an enabled mapping for the reviewed tracker scope", storage.ErrConflict)
		}
	}
	return nil
}
