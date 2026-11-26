package chat

import "github.com/woliveiras/weft/server/internal/config"

// ManualDestination accepts only named console pages. The model cannot supply a URL.
func ManualDestination(name string) bool {
	switch name {
	case "tasks", "workflows", "agents", "projects", "activity", "approvals",
		"models", "chat-model", "plugins", "usage", "budgets", "prices",
		"connections", "connection-setup", "skills", "administration", "configuration-export",
		"delegation", "network", "retention", "resources", "tools", "task-editor", "project-import", "source-control-setup":
		return true
	default:
		return false
	}
}

func ManualHandoff(plan Plan, role config.Role) Interpretation {
	reply := Interpretation{Plan: plan}
	if !ManualDestination(plan.Destination) {
		reply.Text = "That console page is unavailable."
		return reply
	}
	reply.Destination = plan.Destination
	switch plan.Destination {
	case "task-editor":
		if !role.Allows(config.RoleOperator) {
			reply.Destination = "tasks"
		}
		reply.Text = "Open Tasks to inspect the request, workflow selection and submission controls."
	case "project-import":
		if !role.Allows(config.RoleAdmin) {
			reply.Destination = "projects"
		}
		reply.Text = "Open Projects to inspect registrations and import options."
	case "source-control-setup":
		if !role.Allows(config.RoleAdmin) {
			reply.Destination = "connections"
		}
		reply.Text = "Open Connections for repository access and provider credentials."
	case "configuration-export":
		if !role.Allows(config.RoleAdmin) {
			reply.Destination = ""
			reply.Text = "An administrator can download the installation configuration."
		} else {
			reply.Text = "Download the current installation configuration as YAML. The export uses preservation markers for stored credentials."
		}
	case "connection-setup":
		if role.Allows(config.RoleAdmin) {
			reply.Text = "Open the connection form to choose the integration and enter credentials there."
		} else {
			reply.Text = "An administrator can set up a connection. You can inspect existing connections."
			reply.Destination = "connections"
		}
	default:
		reply.Text = "Open the console page to inspect this area."
	}
	return reply
}
