package chat

import (
	"fmt"
	"strings"
	"time"
)

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

type Inspection struct {
	Area  string
	Lines []string
	More  bool
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
	if len(fields) == 0 || fields[0] != "/inspect" {
		return Interpretation{}, false
	}
	if len(fields) == 2 && InspectionDestination(fields[1]) != "" {
		return Interpretation{Plan: Plan{Kind: InspectConsole, Area: fields[1]}}, true
	}
	return Interpretation{Text: "Use /inspect followed by tasks, workflows, agents, projects, skills, approvals, activity, triggers, review-routes, mcp, scm, issue-connections, models, chat-model, plugins, tools, network, retention, usage, prices, or budgets."}, true
}
