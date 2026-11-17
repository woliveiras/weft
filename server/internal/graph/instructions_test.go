package graph

import (
	"encoding/json"
	"gopkg.in/yaml.v3"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestInstructionReferences(t *testing.T) {
	for _, tc := range []struct {
		name        string
		graph, node []string
		valid       bool
	}{
		{"inherit", []string{"a"}, nil, true}, {"none", []string{"a"}, []string{}, true},
		{"narrow", []string{"a", "b"}, []string{"b"}, true}, {"foreign", []string{"a"}, []string{"b"}, false},
		{"duplicate", []string{"a", "a"}, nil, false}, {"duplicate node", []string{"a"}, []string{"a", "a"}, false},
		{"invalid", []string{"../x"}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &domain.Graph{Spec: domain.GraphSpec{Instructions: domain.InstructionRefs{SkillIDs: tc.graph}, Nodes: []domain.Node{{ID: "n", SkillIDs: tc.node}}}}
			report := &Report{}
			validateInstructions(g, report)
			if (len(report.Errors) == 0) != tc.valid {
				t.Fatal(report)
			}
			data, err := json.Marshal(g)
			if err != nil {
				t.Fatal(err)
			}
			var decoded domain.Graph
			if err = json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			data, err = yaml.Marshal(g)
			if err != nil {
				t.Fatal(err)
			}
			var yamlGraph domain.Graph
			if err = yaml.Unmarshal(data, &yamlGraph); err != nil {
				t.Fatal(err)
			}
			if (yamlGraph.Spec.Nodes[0].SkillIDs == nil) != (tc.node == nil) {
				t.Fatal("YAML lost empty versus inherited subset")
			}
			if (decoded.Spec.Nodes[0].SkillIDs == nil) != (tc.node == nil) {
				t.Fatal("lost empty versus inherited subset")
			}
		})
	}
}

func TestRepositoryGuidanceValidatesSourceFamilies(t *testing.T) {
	for _, test := range []struct {
		sources []string
		valid   bool
	}{
		{nil, true},
		{[]string{"agents", "github"}, true},
		{[]string{"claude", "claude"}, false},
		{[]string{"other"}, false},
	} {
		policy := &domain.RepositoryGuidancePolicy{BlockedSources: test.sources}
		for _, node := range []bool{false, true} {
			g := &domain.Graph{Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker"}}}}
			if node {
				g.Spec.Nodes[0].RepositoryGuidance = policy
			} else {
				g.Spec.Instructions.RepositoryGuidance = policy
			}
			report := &Report{}
			validateInstructions(g, report)
			if (len(report.Errors) == 0) != test.valid {
				t.Errorf("sources %v (node %t) gave %+v", test.sources, node, report)
			}
		}
	}
}
