package graph

import (
	"strings"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/domain"
)

func validateTaskDefaults(g *domain.Graph, r *Report) {
	if repos := g.Spec.Workspace.Repositories; len(repos) > 0 {
		if !strings.HasPrefix(g.Metadata.ID, domain.TaskRunGraphPrefix) || domain.TaskExecutionID(g.Metadata.Labels[domain.LabelTask]) != g.Metadata.ID {
			r.errf("spec.workspace.repositories", "repository bindings belong to an admitted task")
		}
		if err := domain.ValidateTaskRepositories(repos); err != nil {
			r.errf("spec.workspace.repositories", "%s", err)
		}
	}
	task := g.Spec.Task
	if task == nil {
		return
	}
	if task.AuthorizationProfile != "" && !task.AuthorizationProfile.IsProfile() {
		r.errf("spec.task.authorizationProfile", "expected manual, assisted or automatic")
	}
	if task.ProjectIDs != nil {
		if task.ProjectID != "" || task.SCMConnection != "" {
			r.errf("spec.task.projectIds", "cannot combine repository suggestions with a singular project or delivery connection")
		}
		seen := map[string]bool{}
		for _, id := range task.ProjectIDs {
			if id == "" || len(id) > 128 || strings.TrimSpace(id) != id || strings.ContainsAny(id, "\r\n\x00") || seen[id] {
				r.errf("spec.task.projectIds", "expected distinct registered project identifiers")
				break
			}
			seen[id] = true
		}
	}
	for _, field := range []struct{ name, value string }{
		{"nodeId", task.NodeID}, {"resultNodeId", task.ResultNodeID},
		{"channelId", task.ChannelID}, {"messageType", task.MessageType},
		{"projectId", task.ProjectID}, {"scmConnection", task.SCMConnection},
	} {
		if len(field.value) > 128 || !utf8.ValidString(field.value) || strings.ContainsAny(field.value, "\x00\r\n") || strings.TrimSpace(field.value) != field.value {
			r.errf("spec.task."+field.name, "must be valid text within 128 bytes without surrounding whitespace or control separators")
		}
	}
	for _, field := range []struct{ name, value string }{{"nodeId", task.NodeID}, {"resultNodeId", task.ResultNodeID}} {
		if field.value == "" {
			continue
		}
		if node := g.Node(field.value); node == nil || node.Disabled {
			r.errf("spec.task."+field.name, "must identify an enabled agent")
		}
	}
	if task.ChannelID == "" {
		if task.MessageType != "" {
			r.errf("spec.task.messageType", "requires a task input channel")
		}
		return
	}
	channel := g.Channel(task.ChannelID)
	if channel == nil || channel.Type != domain.ChannelQueue || channel.From != "" || channel.When != "" || len(channel.To) != 1 || task.NodeID == "" || channel.To[0] != task.NodeID {
		r.errf("spec.task.channelId", "must identify an unconditional queue ingress with one receiver matching task.nodeId")
	}
	if strings.TrimSpace(task.MessageType) == "" {
		r.errf("spec.task.messageType", "a task input channel requires a message type")
		return
	}
	if channel != nil && len(channel.Types) > 0 {
		for _, pattern := range channel.Types {
			if domain.GlobMatch(pattern, task.MessageType) {
				return
			}
		}
		r.errf("spec.task.messageType", "must match an allowed input channel type")
	}
}
