package environment

import (
	"fmt"
	"github.com/woliveiras/weft/server/internal/domain"
	"sort"
)

// Resolve only the selected dependency closure, in deterministic start order.
// Compose health conditions, optional dependencies and implicit merges are not
// silently approximated by container-start readiness.
func composeCompanions(services map[string]any, selected string) ([]domain.Companion, error) {
	visiting, done := map[string]bool{}, map[string]bool{}
	result := []domain.Companion{}
	var visit func(string) error
	visit = func(name string) error {
		if visiting[name] {
			return fmt.Errorf("Compose depends_on cycle at %s", name)
		}
		if done[name] {
			return nil
		}
		service, ok := services[name].(map[string]any)
		if !ok {
			return fmt.Errorf("missing dependency service %s", name)
		}
		visiting[name] = true
		deps := []string{}
		if raw, exists := service["depends_on"]; exists {
			list, ok := raw.([]any)
			if !ok {
				return fmt.Errorf("service %s: depends_on must be a list; health/optional conditions are unsupported", name)
			}
			for _, raw := range list {
				dep, ok := raw.(string)
				if !ok || dep == "" {
					return fmt.Errorf("invalid depends_on service")
				}
				deps = append(deps, dep)
			}
		}
		sort.Strings(deps)
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[name] = false
		done[name] = true
		if name == selected {
			return nil
		}
		for key := range service {
			if key != "image" && key != "command" && key != "depends_on" {
				return fmt.Errorf("companion %s: unsupported field %q (only image, command array and depends_on list are supported)", name, key)
			}
		}
		image, ok := service["image"].(string)
		if !ok {
			return fmt.Errorf("companion %s requires a prebuilt image", name)
		}
		item := domain.Companion{Name: name, Image: image}
		if raw, exists := service["command"]; exists {
			list, ok := raw.([]any)
			if !ok || len(list) == 0 {
				return fmt.Errorf("companion %s command must be a nonempty argument array", name)
			}
			for _, raw := range list {
				arg, ok := raw.(string)
				if !ok {
					return fmt.Errorf("companion command arguments must be strings")
				}
				item.Command = append(item.Command, arg)
			}
		}
		result = append(result, item)
		return domain.ValidateCompanions(result)
	}
	if err := visit(selected); err != nil {
		return nil, err
	}
	return result, nil
}
