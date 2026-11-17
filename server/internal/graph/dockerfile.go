package graph

import (
	"fmt"
	"regexp"
	"strings"
)

var stageName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]*$`)

// DockerfileLines joins continuations using the default Dockerfile escape.
func DockerfileLines(source string) ([]string, error) {
	var lines []string
	pending := ""
	for _, raw := range strings.Split(source, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, "\\") {
			pending += strings.TrimSuffix(line, "\\") + " "
			continue
		}
		line = pending + line
		pending = ""
		if line != "" {
			lines = append(lines, line)
		}
	}
	if pending != "" {
		return nil, fmt.Errorf("unterminated Dockerfile continuation")
	}
	return lines, nil
}

// DockerfileStages preserves unnamed stages as empty entries for numeric COPY references.
func DockerfileStages(source string) ([]string, error) {
	lines, err := DockerfileLines(source)
	if err != nil {
		return nil, err
	}
	var stages []string
	seen := map[string]bool{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if !strings.EqualFold(fields[0], "FROM") {
			continue
		}
		if len(fields) != 2 && (len(fields) != 4 || !strings.EqualFold(fields[2], "AS")) {
			return nil, fmt.Errorf("invalid FROM instruction")
		}
		name := ""
		if len(fields) == 4 {
			name = strings.ToLower(fields[3])
			if !stageName.MatchString(name) || seen[name] || name == "scratch" {
				return nil, fmt.Errorf("invalid or duplicate build stage %q", fields[3])
			}
			seen[name] = true
		}
		stages = append(stages, name)
	}
	return stages, nil
}

func ValidateBuildTarget(source, target string) error {
	if target == "" {
		return nil
	}
	if !stageName.MatchString(target) {
		return fmt.Errorf("invalid build target %q", target)
	}
	stages, err := DockerfileStages(source)
	if err != nil {
		return err
	}
	for _, name := range stages {
		if name == strings.ToLower(target) {
			return nil
		}
	}
	return fmt.Errorf("build target %q is not a named Dockerfile stage", target)
}
