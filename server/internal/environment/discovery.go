package environment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"gopkg.in/yaml.v3"
)

const maxDefinitionBytes = 256 << 10
const maxDiscoveryEntries = 10000

// Discovery is advisory: it never accepts a definition or runs repository code.
type Discovery struct {
	Source     string                 `json:"source"`
	Status     string                 `json:"status"`
	Candidates []EnvironmentCandidate `json:"candidates"`
}

type EnvironmentCandidate struct {
	Targets  []string `json:"targets,omitempty"`
	Path     string   `json:"path"`
	SHA256   string   `json:"sha256"`
	Services []string `json:"services,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// Discover scans an existing checkout. os.Root confines reads even when a
// repository changes symlinks concurrently. Symlinks are deliberately rejected.
func Discover(ctx context.Context, dir string) (Discovery, error) {
	result := Discovery{Source: "none", Status: "proposal_required", Candidates: []EnvironmentCandidate{}}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return result, fmt.Errorf("open repository for environment discovery: %w", err)
	}
	defer root.Close()
	groups := make([][]string, 4)
	for i, name := range []string{".weft/environment.yaml", ".devcontainer/devcontainer.json"} {
		_, err := root.Lstat(name)
		if err == nil {
			groups[i] = []string{name}
			break
		}
		if !os.IsNotExist(err) {
			return result, fmt.Errorf("inspect environment definition: %w", err)
		}
	}
	if len(groups[0])+len(groups[1]) == 0 {
		count := 0
		err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			count++
			if count > maxDiscoveryEntries {
				return fmt.Errorf("repository discovery exceeds %d entries", maxDiscoveryEntries)
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "node_modules", "vendor", ".venv":
					return fs.SkipDir
				}
				return nil
			}
			base := entry.Name()
			if base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") || strings.HasSuffix(base, ".Dockerfile") {
				groups[2] = append(groups[2], name)
			}
			if composeFilename(base) {
				groups[3] = append(groups[3], name)
			}
			return nil
		})
		if err != nil {
			return result, fmt.Errorf("scan environment definitions: %w", err)
		}
	}
	for i, names := range groups {
		if len(names) == 0 {
			continue
		}
		result.Source = []string{"weft", "devcontainer", "dockerfile", "compose"}[i]
		result.Status = "review_required"
		sort.Strings(names)
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			candidate := EnvironmentCandidate{Path: name}
			data, err := readDefinition(root, name)
			if err == nil {
				sum := sha256.Sum256(data)
				candidate.SHA256 = hex.EncodeToString(sum[:])
				candidate.Services, err = inspectDefinition(result.Source, data)
				if err == nil && result.Source == "dockerfile" {
					var stages []string
					stages, err = graph.DockerfileStages(string(data))
					for _, stage := range stages {
						if stage != "" {
							candidate.Targets = append(candidate.Targets, stage)
						}
					}
				}
			}
			if err != nil {
				candidate.Error = err.Error()
				result.Status = "invalid"
			}
			result.Candidates = append(result.Candidates, candidate)
		}
		break
	}
	return result, nil
}

func readDefinition(root *os.Root, name string) ([]byte, error) {
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := root.Lstat(path.Join(parts[:i+1]...))
		if err != nil {
			return nil, fmt.Errorf("cannot inspect definition path")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlinks are not supported in definition paths")
		}
	}
	info, err := root.Stat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("definition must be a regular file")
	}
	if info.Size() > maxDefinitionBytes {
		return nil, fmt.Errorf("definition exceeds %d bytes", maxDefinitionBytes)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("cannot open definition")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxDefinitionBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read definition")
	}
	if len(data) > maxDefinitionBytes {
		return nil, fmt.Errorf("definition exceeds %d bytes", maxDefinitionBytes)
	}
	return data, nil
}

// Inspection intentionally does not return source contents or resolve Compose
// interpolation. Candidates still require format/policy validation on acceptance.
func inspectDefinition(source string, data []byte) ([]string, error) {
	if source == "dockerfile" {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 1 && strings.EqualFold(fields[0], "FROM") {
				return nil, nil
			}
		}
		return nil, fmt.Errorf("Dockerfile requires a FROM instruction")
	}
	var doc map[string]any
	if source == "devcontainer" {
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("invalid devcontainer JSON; comments and trailing commas are not supported yet")
		}
		for key := range doc {
			if key != "name" && key != "image" && key != "postCreateCommand" {
				return nil, fmt.Errorf("unsupported devcontainer field %q", key)
			}
		}
		image, ok := doc["image"].(string)
		if !ok || strings.TrimSpace(image) == "" {
			return nil, fmt.Errorf("devcontainer requires an image")
		}
		if command, ok := doc["postCreateCommand"]; ok {
			if _, ok := command.(string); !ok {
				return nil, fmt.Errorf("postCreateCommand must be a string")
			}
		}
		return nil, nil
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(&doc); err != nil || len(doc) == 0 {
		return nil, fmt.Errorf("invalid environment YAML mapping")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected one YAML document")
	}
	if source == "weft" {
		var spec domain.EnvironmentSpec
		typed := yaml.NewDecoder(strings.NewReader(string(data)))
		typed.KnownFields(true)
		if err := typed.Decode(&spec); err != nil {
			return nil, fmt.Errorf("invalid or unsupported environment fields")
		}

		image, _ := doc["image"].(string)
		dockerfile, _ := doc["dockerfile"].(string)
		if (strings.TrimSpace(image) == "") == (strings.TrimSpace(dockerfile) == "") {
			return nil, fmt.Errorf("set exactly one of image or dockerfile")
		}
		return nil, nil
	}
	services, ok := doc["services"].(map[string]any)
	if !ok || len(services) == 0 {
		return nil, fmt.Errorf("Compose requires a nonempty services mapping")
	}
	names := make([]string, 0, len(services))
	for name, service := range services {
		if _, ok := service.(map[string]any); !ok {
			return nil, fmt.Errorf("Compose service %q must be a mapping", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// Variants are independent candidates, never implicitly merged as overrides.
func composeFilename(name string) bool {
	if name == "compose.yaml" || name == "compose.yml" || name == "docker-compose.yaml" || name == "docker-compose.yml" {
		return true
	}
	return (strings.HasPrefix(name, "compose.") || strings.HasPrefix(name, "docker-compose.")) && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml"))
}
