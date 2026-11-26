package graph

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"gopkg.in/yaml.v3"
)

func taskGraph() *domain.Graph {
	return &domain.Graph{Metadata: domain.GraphMetadata{ID: "task-defaults"}, Spec: domain.GraphSpec{
		Task:     &domain.TaskDefaults{NodeID: "worker", ResultNodeID: "result", ChannelID: "requests", MessageType: "work.request", ProjectID: "project-a", SCMConnection: "team-scm"},
		Nodes:    []domain.Node{{ID: "worker", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}}, {ID: "result", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}}},
		Channels: []domain.Channel{{ID: "requests", To: []string{"worker"}, Types: []string{"work.*"}}},
	}}
}

func TestTaskDefaultsValidation(t *testing.T) {
	for _, test := range []struct {
		name, path string
		change     func(*domain.Graph)
	}{
		{"missing entry", "nodeId", func(g *domain.Graph) { g.Spec.Task.NodeID = "missing" }},
		{"disabled entry", "nodeId", func(g *domain.Graph) { g.Spec.Nodes[0].Disabled = true }},
		{"disabled result", "resultNodeId", func(g *domain.Graph) { g.Spec.Nodes[1].Disabled = true }},
		{"missing result", "resultNodeId", func(g *domain.Graph) { g.Spec.Task.ResultNodeID = "missing" }},
		{"missing channel", "channelId", func(g *domain.Graph) { g.Spec.Task.ChannelID = "absent" }},
		{"producer", "channelId", func(g *domain.Graph) { g.Spec.Channels[0].From = "result" }},
		{"conditional", "channelId", func(g *domain.Graph) { g.Spec.Channels[0].When = "true" }},
		{"fanout", "channelId", func(g *domain.Graph) { g.Spec.Channels[0].Type = domain.ChannelFanout }},
		{"multiple receivers", "channelId", func(g *domain.Graph) { g.Spec.Channels[0].To = []string{"worker", "result"} }},
		{"different receiver", "channelId", func(g *domain.Graph) { g.Spec.Channels[0].To = []string{"result"} }},
		{"entry required for channel", "channelId", func(g *domain.Graph) { g.Spec.Task.NodeID = "" }},
		{"type required", "messageType", func(g *domain.Graph) { g.Spec.Task.MessageType = "" }},
		{"disallowed type", "messageType", func(g *domain.Graph) { g.Spec.Task.MessageType = "other.request" }},
		{"orphan type", "messageType", func(g *domain.Graph) { g.Spec.Task.ChannelID = "" }},
		{"long project", "projectId", func(g *domain.Graph) { g.Spec.Task.ProjectID = strings.Repeat("p", 129) }},
		{"byte bound", "projectId", func(g *domain.Graph) { g.Spec.Task.ProjectID = strings.Repeat("é", 65) }},
		{"long connection", "scmConnection", func(g *domain.Graph) { g.Spec.Task.SCMConnection = strings.Repeat("c", 129) }},
		{"long type", "messageType", func(g *domain.Graph) { g.Spec.Task.MessageType = strings.Repeat("t", 129) }},
		{"blank external name", "projectId", func(g *domain.Graph) { g.Spec.Task.ProjectID = " " }},
		{"control separator", "projectId", func(g *domain.Graph) { g.Spec.Task.ProjectID = "project\nother" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := taskGraph()
			test.change(g)
			report := Validate(g, nil)
			for _, issue := range report.Errors {
				if issue.Path == "spec.task."+test.path {
					return
				}
			}
			t.Fatalf("missing spec.task.%s error: %+v", test.path, report.Errors)
		})
	}
	for _, change := range []func(*domain.Graph){
		func(g *domain.Graph) {},
		func(g *domain.Graph) { g.Spec.Task = nil },
		func(g *domain.Graph) { g.Spec.Task = &domain.TaskDefaults{} },
		func(g *domain.Graph) { g.Spec.Task = &domain.TaskDefaults{NodeID: "result"} },
		func(g *domain.Graph) { g.Spec.Task.ProjectID = strings.Repeat("é", 64) },
		func(g *domain.Graph) { g.Spec.Channels[0].Types = nil },
	} {
		g := taskGraph()
		change(g)
		if report := Validate(g, nil); !report.Valid {
			t.Fatalf("valid suggestions refused: %+v", report.Errors)
		}
	}
}

func TestTaskDefaultsDocumentRoundTrip(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			g := taskGraph()
			var data []byte
			var err error
			if format == "json" {
				data, err = json.Marshal(g)
			} else {
				data, err = yaml.Marshal(g)
			}
			if err != nil {
				t.Fatal(err)
			}
			decoded, unknown, err := Decode(data, format == "yaml")
			if err != nil || len(unknown) != 0 || decoded.Spec.Task == nil || !reflect.DeepEqual(decoded.Spec.Task, g.Spec.Task) {
				t.Fatalf("round trip: %+v %+v %v", decoded, unknown, err)
			}
		})
	}
	_, unknown, err := Decode([]byte(`{"spec":{"task":{"authorization":"full"}}}`), false)
	if err != nil || len(unknown) != 1 || unknown[0].Path != "spec.task.authorization" {
		t.Fatalf("unexpected default accepts authority: %+v %v", unknown, err)
	}
}
