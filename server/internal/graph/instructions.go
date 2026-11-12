package graph

import (
	"fmt"
	"slices"

	"github.com/woliveiras/weft/server/internal/domain"
)

func validateInstructions(g *domain.Graph, r *Report) {
	validateRepositoryGuidance(g.Spec.Instructions.RepositoryGuidance, "spec.instructions.repositoryGuidance", r)
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
		validateRepositoryGuidance(n.RepositoryGuidance, fmt.Sprintf("spec.nodes[%d].repositoryGuidance", i), r)
		local := map[string]bool{}
		for j, id := range n.SkillIDs {
			if !seen[id] || local[id] {
				r.errf(fmt.Sprintf("spec.nodes[%d].skillIds[%d]", i, j), "must be a unique subset of graph skillIds")
			}
			local[id] = true
		}
	}
}

func validateRepositoryGuidance(policy *domain.RepositoryGuidancePolicy, field string, r *Report) {
	if policy == nil {
		return
	}
	seen := map[string]bool{}
	for _, source := range policy.BlockedSources {
		if !slices.Contains(domain.RepositoryGuidanceSources, source) || seen[source] {
			r.errf(field+".blockedSources", "source families must be unique and one of agents, claude or github")
			return
		}
		seen[source] = true
	}
}
