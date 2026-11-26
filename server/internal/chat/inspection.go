package chat

import (
	"fmt"
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
	case "tools", "network", "retention":
		return "administration"
	}
	return ""
}

type InventoryReference struct {
	Position int    `json:"position"`
	ID       string `json:"id"`
}
type Inventory struct {
	Area  string               `json:"area"`
	Items []InventoryReference `json:"items"`
}
type Inspection struct {
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

func (v Inventory) Context() string {
	switch v.Area {
	case "tasks", "workflows", "agents", "projects":
	default:
		return ""
	}
	if len(v.Items) == 0 || len(v.Items) > InspectionLimit {
		return ""
	}
	parts := []string{}
	last := 0
	for _, item := range v.Items {
		if !validReferenceID(item.ID) || item.Position <= last || item.Position > InspectionLimit {
			return ""
		}
		parts = append(parts, fmt.Sprintf("%d=%s", item.Position, item.ID))
		last = item.Position
	}
	return "Weft listed " + v.Area + " in this order: " + strings.Join(parts, ", ") + "."
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
	return Interpretation{Text: "Use /inspect followed by tasks, workflows, agents, projects, skills, approvals, activity, triggers, review-routes, mcp, scm, issue-connections, models, chat-model, plugins, tools, network, retention, usage, prices, or budgets."}, true
}
