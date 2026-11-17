package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// Companion is an explicitly reviewed, ephemeral runtime service. No host
// mounts, environment injection, published ports or privileges are supported.
type Companion struct {
	Name    string   `json:"name" yaml:"name"`
	Image   string   `json:"image" yaml:"image"`
	Command []string `json:"command,omitempty" yaml:"command,omitempty"`
}

var companionName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

func ValidateCompanions(items []Companion) error {
	if len(items) > 3 {
		return fmt.Errorf("at most three companion services are supported")
	}
	seen := map[string]bool{}
	for _, item := range items {
		if !companionName.MatchString(item.Name) || seen[item.Name] {
			return fmt.Errorf("invalid or duplicate companion name %q", item.Name)
		}
		seen[item.Name] = true
		if item.Image == "" || strings.HasPrefix(item.Image, "-") || strings.ContainsAny(item.Image, "$ \t\n\x00") {
			return fmt.Errorf("companion %s needs a literal image reference", item.Name)
		}
		if len(item.Command) > 64 {
			return fmt.Errorf("companion command is too long")
		}
		for _, arg := range item.Command {
			if len(arg) > 4096 || strings.ContainsAny(arg, "$\x00") {
				return fmt.Errorf("companion commands cannot use interpolation or oversized arguments")
			}
		}
	}
	return nil
}
