package graph

import (
	"strings"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/domain"
)

func validateTaskDefaults(g *domain.Graph, r *Report) {
	task := g.Spec.Task
	if task == nil {
		return
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
