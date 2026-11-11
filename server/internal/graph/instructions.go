package graph

import (
	"fmt"

	"github.com/woliveiras/weft/server/internal/domain"
)

func validateInstructions(g *domain.Graph, r *Report) {
	seen := map[string]bool{}
	if len(g.Spec.Instructions.SkillIDs) > 128 {
		r.errf("spec.instructions.skillIds", "at most 128 skills")
	}
	for i, id := range g.Spec.Instructions.SkillIDs {
		p := fmt.Sprintf("spec.instructions.skillIds[%d]", i)
		if !idRe.MatchString(id) || seen[id] {
			r.errf(p, "invalid or duplicate skill ID")
		}
		seen[id] = true
	}
	for i, n := range g.Spec.Nodes {
		local := map[string]bool{}
		for j, id := range n.SkillIDs {
			if !seen[id] || local[id] {
				r.errf(fmt.Sprintf("spec.nodes[%d].skillIds[%d]", i, j), "must be a unique subset of graph skillIds")
			}
			local[id] = true
		}
	}
}
