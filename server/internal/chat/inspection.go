package chat

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

const InspectExecution = "execution.inspect"

const InspectConsole = "console.inspect"
const InspectionLimit = 8

// The resource is a closed list, never an arbitrary HTTP path. Inspection
// responses include a link to the corresponding manual console area.
func InspectionDestination(area string) string {
	switch area {
	case "tasks", "workflows", "agents", "projects", "skills", "approvals", "activity", "models", "chat-model", "plugins", "usage", "prices", "budgets":
		return area
	case "mcp", "scm", "issue-connections":
		return "connections"
	case "triggers", "review-routes":
		return "workflows"
	case "tools", "network", "retention", "delegation":
		return area
	}
	return ""
}

type InventoryReference struct {
	Position int    `json:"position"`
	ID       string `json:"id"`
}
type Inventory struct {
	TaskMode   string               `json:"taskMode,omitempty"`
	TaskStatus string               `json:"taskStatus,omitempty"`
	Total      *int                 `json:"total,omitempty"`
	Area       string               `json:"area"`
	Items      []InventoryReference `json:"items"`
}
type Inspection struct {
	Search     *ResourceSearch
	References []InventoryReference
	Area       string
	Lines      []string
	More       bool
}

func (v *Inspection) Add(line string) {
	if len(v.Lines) == InspectionLimit {
		v.More = true
		return
	}
	if !safeText(line, 320) || strings.ContainsAny(line, "\n\r\t") {
		line = "Details available on the console page."
	}
	v.Lines = append(v.Lines, line)
}

// Add an identifier only when its corresponding line was displayed verbatim.
func (v *Inspection) AddResource(id, line string) {
	before := len(v.Lines)
	v.Add(line)
	if len(v.Lines) > before && v.Lines[before] == line && validReferenceID(id) {
		v.References = append(v.References, InventoryReference{Position: before + 1, ID: id})
	}
}

func (v *Inspection) CatalogResource(id, line string, entry CatalogCandidate) {
	if v.Search != nil {
		v.Search.AddCatalog(id, line, entry)
		return
	}
	v.AddResource(id, line)
}

func (v *Inspection) Named(id, name, line string) {
	if v.Search != nil {
		v.Search.Add(id, name, line)
		return
	}
	v.AddResource(id, line)
}

func (v Inventory) Context() string {
	if !SearchableArea(v.Area) {
		return ""
	}
	summary := ""
	if v.Area == "tasks" && v.Total != nil && *v.Total >= 0 && (v.TaskStatus == "" || slices.Contains(taskStates, v.TaskStatus)) && slices.Contains(taskQueryModes, v.TaskMode) {
		summary = fmt.Sprintf("Previous task query: mode=%s, status=%q, total=%d. Re-query for the current state.", v.TaskMode, v.TaskStatus, *v.Total)
	}
	if len(v.Items) == 0 {
		return summary
	}
	if len(v.Items) > InspectionLimit {
		return ""
	}
	parts := []string{}
	last := 0
	for _, item := range v.Items {
		if !validDiscoveryReference(item.ID) || item.Position <= last || item.Position > InspectionLimit {
			return ""
		}
		parts = append(parts, fmt.Sprintf("%d=%s", item.Position, item.ID))
		last = item.Position
	}
	return strings.TrimSpace(summary + " Weft listed " + v.Area + " in this order: " + strings.Join(parts, ", ") + ".")
}
func (v Inspection) Text(now time.Time) string {
	lines := []string{fmt.Sprintf("%s · %s", v.Area, now.UTC().Format(time.RFC3339))}
	if len(v.Lines) == 0 {
		lines = append(lines, "No matching records.")
	}
	for _, line := range v.Lines {
		lines = append(lines, "• "+line)
	}
	if v.More {
		lines = append(lines, "Additional records are available on the console page.")
	}
	return strings.Join(lines, "\n")
}

func ExplicitInspection(text string) (Interpretation, bool) {
	fields := strings.Fields(text)
	if len(fields) == 3 && fields[0] == "/inspect" && fields[1] == "execution" && validReferenceID(fields[2]) {
		return Interpretation{Plan: Plan{Kind: InspectExecution, ExecutionID: fields[2], Title: "Inspect execution"}}, true
	}
	if len(fields) == 0 || fields[0] != "/inspect" {
		return Interpretation{}, false
	}
	if len(fields) == 2 && InspectionDestination(fields[1]) != "" {
		return Interpretation{Plan: Plan{Kind: InspectConsole, Area: fields[1]}}, true
	}
	return Interpretation{Text: "Use /inspect followed by tasks, workflows, agents, projects, skills, approvals, activity, triggers, review-routes, mcp, scm, issue-connections, models, chat-model, plugins, tools, network, retention, delegation, usage, prices, or budgets. Use /inspect execution <id> for a specific execution."}, true
}
