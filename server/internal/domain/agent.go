package domain

// AgentBinding pins a reusable definition and its parameters to one workflow node.
type AgentBinding struct {
	ID         string            `json:"id" yaml:"id"`
	Version    int               `json:"version" yaml:"version"`
	Parameters map[string]string `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

type AgentParameter struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// AgentDefinition contains settings shared across workflows. Workflow nodes own
// topology, timing, sizing and their per-workflow workspace settings.
type AgentDefinition struct {
	Name               string                    `json:"name"`
	Description        string                    `json:"description,omitempty"`
	Runtime            RuntimeRef                `json:"runtime"`
	Model              ModelRef                  `json:"model,omitempty"`
	Prompt             string                    `json:"prompt,omitempty"`
	Tools              []ToolGrant               `json:"tools,omitempty"`
	Isolation          IsolationSpec             `json:"isolation,omitempty"`
	ReadOnly           bool                      `json:"readOnly,omitempty"`
	SkillIDs           SkillSelection            `json:"skillIds"`
	RepositoryGuidance *RepositoryGuidancePolicy `json:"repositoryGuidance,omitempty"`
	Parameters         []AgentParameter          `json:"parameters,omitempty"`
}
