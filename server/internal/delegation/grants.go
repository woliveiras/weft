package delegation

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/woliveiras/weft/server/internal/domain"
)

var toolID = regexp.MustCompile(`^[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+)+$`)

func ValidateTools(names []string) error {
	if len(names) > 32 {
		return fmt.Errorf("profiles support at most 32 explicit capabilities")
	}
	seen := map[string]bool{}
	for _, name := range names {
		if len(name) > 256 || !toolID.MatchString(name) || seen[name] || name == "review.request" || name == "environment.propose" || name == "environment.status" {
			return fmt.Errorf("invalid, duplicate or unsupported delegated capability %q", name)
		}
		seen[name] = true
	}
	return nil
}

// InheritGrants selects exact parent declarations without rewriting constraints.
// Wildcards are not expanded. A missing capability fails admission; it is never
// silently elevated or replaced by a more permissive profile declaration.
func InheritGrants(parent []domain.ToolGrant, names []string) ([]domain.ToolGrant, error) {
	if err := ValidateTools(names); err != nil {
		return nil, err
	}
	out := []domain.ToolGrant{}
	for _, name := range names {
		found := false
		for _, grant := range parent {
			if grant.Name != name {
				continue
			}
			found = true
			grant.Paths = slices.Clone(grant.Paths)
			grant.Commands = slices.Clone(grant.Commands)
			grant.Hosts = slices.Clone(grant.Hosts)
			out = append(out, grant)
		}
		if !found {
			return nil, fmt.Errorf("delegation: parent does not explicitly grant %s", name)
		}
	}
	return out, nil
}
