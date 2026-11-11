package domain

// Instruction snapshots are immutable data, never capability grants.
type InstructionEntry struct {
	Scope    string `json:"scope"`
	Source   string `json:"source"`
	Revision int64  `json:"revision,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	Body     string `json:"body,omitempty"`
	Included bool   `json:"included"`
	Reason   string `json:"reason,omitempty"`
}
type InstructionSnapshot struct {
	GraphID     string             `json:"graphId"`
	NodeID      string             `json:"nodeId"`
	ExecutionID string             `json:"executionId,omitempty"`
	Entries     []InstructionEntry `json:"entries"`
	Budget      int                `json:"budget"`
}

// SkillSelection keeps inheritance (nil) distinct from an explicit empty
// selection in both JSON and YAML. yaml.v3 normally encodes a nil slice as [].
type SkillSelection []string

func (s SkillSelection) MarshalYAML() (any, error) {
	if s == nil {
		return nil, nil
	}
	return []string(s), nil
}
