package environment

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/distribution/reference"
	"github.com/woliveiras/weft/server/internal/graph"
)

// parseCopy keeps stage/image filesystem paths distinct from repository paths.
func parseCopy(instruction, arguments string) (string, []string, error) {
	from := ""
	if strings.HasPrefix(arguments, "--from=") && instruction == "COPY" {
		end := strings.IndexFunc(arguments, unicode.IsSpace)
		if end < 0 {
			return "", nil, fmt.Errorf("COPY --from requires source and destination")
		}
		from = strings.TrimPrefix(arguments[:end], "--from=")
		if from == "" {
			return "", nil, fmt.Errorf("COPY --from requires a stage")
		}
		arguments = strings.TrimSpace(arguments[end:])
	}
	operands, err := copyOperandsFrom(arguments, from != "")
	return from, operands, err
}

func validateStageCopies(source string) error {
	stages, err := graph.DockerfileStages(source)
	if err != nil {
		return err
	}
	lines, err := graph.DockerfileLines(source)
	if err != nil {
		return err
	}
	names := map[string]int{}
	for i, name := range stages {
		if name != "" {
			names[name] = i
		}
	}
	edges := make([][]int, len(stages))
	current := -1
	for _, line := range lines {
		fields := strings.Fields(line)
		instruction := strings.ToUpper(fields[0])
		if instruction == "FROM" {
			current++
			if parent, ok := names[strings.ToLower(fields[1])]; ok && parent < current {
				edges[current] = append(edges[current], parent)
			}
		}
		if instruction != "COPY" && instruction != "ADD" {
			continue
		}
		from, _, err := parseCopy(instruction, strings.TrimSpace(strings.TrimPrefix(line, fields[0])))
		if err != nil {
			return err
		}
		if current < 0 {
			return fmt.Errorf("COPY/ADD must follow FROM")
		}
		if from == "" {
			continue
		}
		parent, ok := names[strings.ToLower(from)]
		if !ok {
			// Numeric references always denote stages, never registry names.
			numeric := strings.TrimLeft(from, "+-")
			if numeric != "" && strings.Trim(numeric, "0123456789") == "" {
				parent, err = strconv.Atoi(from)
				if err != nil || parent < 0 || parent >= len(stages) || strconv.Itoa(parent) != from {
					return fmt.Errorf("COPY --from stage index is invalid")
				}
			} else {
				if _, err := reference.ParseNormalizedNamed(from); err != nil || from == "scratch" {
					// Do not echo malformed input: it could contain embedded credentials.
					return fmt.Errorf("COPY --from requires a valid container image reference or Dockerfile stage")
				}
				continue // External image: no local dependency edge.
			}
		}
		edges[current] = append(edges[current], parent)
	}
	// Detect self-reference and cycles, including forward COPY dependencies.
	state := make([]uint8, len(stages))
	var visit func(int) error
	visit = func(i int) error {
		if state[i] == 1 {
			return fmt.Errorf("circular Dockerfile stage dependency at index %d", i)
		}
		if state[i] == 2 {
			return nil
		}
		state[i] = 1
		for _, parent := range edges[i] {
			if err := visit(parent); err != nil {
				return err
			}
		}
		state[i] = 2
		return nil
	}
	for i := range stages {
		if err := visit(i); err != nil {
			return err
		}
	}
	return nil
}
