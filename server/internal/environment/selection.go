package environment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"gopkg.in/yaml.v3"
)

var ErrInvalidDefinition = errors.New("environment definition is invalid or changed; discover and review it again")

// Selection refers to the exact candidate shown by discovery, never arbitrary
// caller-supplied source text. Preview is read-only; acceptance uses graph PUT.
type Selection struct {
	Target  string `json:"target,omitempty"`
	Source  string `json:"source"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Service string `json:"service,omitempty"`
}

func Resolve(ctx context.Context, dir string, sel Selection) (*domain.EnvironmentSpec, error) {
	env, err := resolve(ctx, dir, sel)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDefinition, err)
	}
	return env, nil
}

func resolve(ctx context.Context, dir string, sel Selection) (*domain.EnvironmentSpec, error) {
	discovery, err := Discover(ctx, dir)
	if err != nil {
		return nil, err
	}
	if discovery.Source != sel.Source {
		return nil, fmt.Errorf("repository source priority changed")
	}
	found := false
	for _, c := range discovery.Candidates {
		if c.Path == sel.Path && c.SHA256 == sel.SHA256 && c.Error == "" && sel.SHA256 != "" {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("selected candidate changed or is unavailable")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	origin := &domain.EnvironmentOrigin{Source: sel.Source, Path: sel.Path, SHA256: sel.SHA256, Service: sel.Service, Target: sel.Target, Files: map[string]string{}}
	read := func(name string) ([]byte, error) {
		if !fsLocal(name) {
			return nil, fmt.Errorf("file path must stay inside the repository")
		}
		data, err := readDefinition(root, name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		origin.Files[name] = hex.EncodeToString(sum[:])
		return data, nil
	}
	data, err := read(sel.Path)
	if err != nil {
		return nil, err
	}
	if origin.Files[sel.Path] != sel.SHA256 {
		return nil, fmt.Errorf("selected file changed during review")
	}
	env := &domain.EnvironmentSpec{}
	switch sel.Source {
	case "weft":
		decoder := yaml.NewDecoder(strings.NewReader(string(data)))
		decoder.KnownFields(true)
		if err := decoder.Decode(env); err != nil {
			return nil, fmt.Errorf("invalid environment fields")
		}
		if env.Origin != nil {
			return nil, fmt.Errorf("repository definitions cannot set origin")
		}
		if env.Dockerfile != "" && !strings.Contains(env.Dockerfile, "\n") && !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(env.Dockerfile)), "FROM ") {
			origin.DockerfilePath = env.Dockerfile
			content, err := read(env.Dockerfile)
			if err != nil {
				return nil, err
			}
			env.Dockerfile = string(content)
		}
	case "devcontainer":
		var doc struct {
			Name    string `json:"name"`
			Image   string `json:"image"`
			Command string `json:"postCreateCommand"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&doc); err != nil {
			return nil, fmt.Errorf("unsupported devcontainer fields")
		}
		env.Image = doc.Image
		if doc.Command != "" {
			env.Setup = []string{doc.Command}
		}
	case "dockerfile":
		env.Dockerfile = string(data)
		origin.DockerfilePath = sel.Path
	case "compose":
		var doc map[string]any
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("invalid Compose document")
		}
		for key := range doc {
			if key != "services" && key != "name" && key != "version" {
				return nil, fmt.Errorf("unsupported Compose field %q", key)
			}
		}
		services, ok := doc["services"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid Compose services")
		}
		service, ok := services[sel.Service].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("select an existing Compose service")
		}
		for key := range service {
			if key != "image" && key != "build" && key != "depends_on" {
				return nil, fmt.Errorf("unsupported selected service field %q; only image, build and depends_on are supported", key)
			}
		}
		env.Companions, err = composeCompanions(services, sel.Service)
		if err != nil {
			return nil, err
		}
		if image, ok := service["image"]; ok {
			var valid bool
			env.Image, valid = image.(string)
			if !valid {
				return nil, fmt.Errorf("service image must be a string")
			}
		}
		if build, ok := service["build"]; ok {
			if env.Image != "" {
				return nil, fmt.Errorf("select a service with image or build, not both")
			}
			contextDir, dockerfile := ".", "Dockerfile"
			switch b := build.(type) {
			case string:
				contextDir = b
			case map[string]any:
				for key := range b {
					if key != "context" && key != "dockerfile" && key != "target" {
						return nil, fmt.Errorf("unsupported Compose build field %q", key)
					}
				}
				if v, exists := b["target"]; exists {
					var ok bool
					env.Target, ok = v.(string)
					if !ok {
						return nil, fmt.Errorf("build target must be a string")
					}
				}
				if v, exists := b["context"]; exists {
					var ok bool
					contextDir, ok = v.(string)
					if !ok {
						return nil, fmt.Errorf("build context must be a string")
					}
				}
				if v, exists := b["dockerfile"]; exists {
					var ok bool
					dockerfile, ok = v.(string)
					if !ok {
						return nil, fmt.Errorf("Dockerfile path must be a string")
					}
				}
			default:
				return nil, fmt.Errorf("unsupported Compose build")
			}
			if !fsLocal(contextDir) || !fsLocal(dockerfile) {
				return nil, fmt.Errorf("build paths must stay inside the repository")
			}
			env.Context = path.Join(path.Dir(sel.Path), contextDir)
			origin.DockerfilePath = path.Join(env.Context, dockerfile)
			content, err := read(origin.DockerfilePath)
			if err != nil {
				return nil, err
			}
			env.Dockerfile = string(content)
		}
	default:
		return nil, fmt.Errorf("unsupported source")
	}
	if sel.Source != "compose" && len(env.Companions) > 0 {
		return nil, fmt.Errorf("repository companions require Compose review")
	}
	if sel.Source != "compose" && sel.Service != "" {
		return nil, fmt.Errorf("service is only valid for Compose")
	}
	if sel.Target != "" {
		if sel.Source != "dockerfile" {
			return nil, fmt.Errorf("target selection is only valid for standalone Dockerfiles; use the definition target field")
		}
		env.Target = sel.Target
	}
	// Repository files may not widen execution networking or set resource budgets.
	// Operators can explicitly configure those separately as a manual environment.
	if env.Network.Run != "" && env.Network.Run != "none" && env.Network.Run != "egress" {
		return nil, fmt.Errorf("repository execution network must be none or reviewed egress")
	}
	if env.CPU != "" || env.Memory != "" {
		return nil, fmt.Errorf("repository resource overrides are not supported; configure a manual environment")
	}
	if strings.Contains(env.Image, "$") {
		return nil, fmt.Errorf("image interpolation is not supported")
	}
	// Make effective network defaults visible in the review, not implicit.
	if env.Network.Setup == "" {
		env.Network.Setup = "bridge"
	}
	if env.Network.Run == "" {
		env.Network.Run = "none"
	}
	if env.Dockerfile != "" {
		if err := validateRepositoryDockerfile(env.Dockerfile); err != nil {
			return nil, err
		}
	}
	env.Origin = origin
	if env.Dockerfile != "" && env.Context == "" {
		env.Context = "."
	}
	if err := graph.ValidateEnvironment(env).Err(); err != nil {
		return nil, err
	}
	if env.Context != "" {
		snapshot, err := SnapshotContext(ctx, dir, env)
		if err != nil {
			return nil, fmt.Errorf("capture build context: %w", err)
		}
		origin.ContextSHA256 = snapshot.SHA256
		snapshot.Close()
	}
	return env, nil
}

func fsLocal(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\$\x00") || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// Repository COPY/ADD use local context inputs only. Remote sources, custom
// frontends and additional privileged build features remain unsupported.
func validateRepositoryDockerfile(text string) error {
	if err := validateStageCopies(text); err != nil {
		return err
	}
	pending := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			directive := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "#")))
			key, _, isDirective := strings.Cut(directive, "=")
			if isDirective && (strings.TrimSpace(key) == "syntax" || strings.TrimSpace(key) == "escape") {
				return fmt.Errorf("custom Dockerfile parser directives are not supported")
			}
			continue
		}
		if strings.HasSuffix(line, "\\") {
			pending += strings.TrimSuffix(line, "\\") + " "
			continue
		}
		line = pending + line
		pending = ""
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 2 {
			return fmt.Errorf("Dockerfile instruction requires an argument")
		}
		switch strings.ToUpper(fields[0]) {
		case "COPY", "ADD":
			_, _, err := parseCopy(strings.ToUpper(fields[0]), strings.TrimSpace(strings.TrimPrefix(line, fields[0])))
			if err != nil {
				return err
			}
		case "FROM", "RUN", "ENV", "ARG", "WORKDIR", "USER", "SHELL", "LABEL", "CMD", "ENTRYPOINT", "EXPOSE", "STOPSIGNAL", "HEALTHCHECK":
			if strings.HasPrefix(fields[1], "--") || strings.Contains(line, "<<") {
				return fmt.Errorf("Dockerfile flags and heredocs are not supported")
			}
		default:
			return fmt.Errorf("unsupported Dockerfile instruction %q", fields[0])
		}
	}
	if pending != "" {
		return fmt.Errorf("unterminated Dockerfile continuation")
	}
	return nil
}

// ValidateAccepted re-resolves the source immediately before a build. It checks
// every referenced file and the actual normalized settings, not just the image hash.
func ValidateAccepted(ctx context.Context, dir string, env *domain.EnvironmentSpec) error {
	if err := graph.ValidateEnvironment(env).Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidDefinition, err)
	}
	if env.Origin == nil {
		return nil
	}
	o := env.Origin
	if o.Source == "agent" {
		return validateProposalFiles(ctx, dir, env)
	}
	resolved, err := Resolve(ctx, dir, Selection{Source: o.Source, Path: o.Path, SHA256: o.SHA256, Service: o.Service, Target: o.Target})
	if err != nil {
		return err
	}
	// JSON comparison treats omitted empty slices consistently after persistence.
	a, _ := json.Marshal(env)
	b, _ := json.Marshal(resolved)
	if !bytes.Equal(a, b) {
		return fmt.Errorf("%w: definition or referenced files changed after review", ErrInvalidDefinition)
	}
	return nil
}

// Docker handles wildcard expansion and local ADD archive extraction inside
// the daemon. No source may reference a remote URL, variable or parent path.
func copyOperandsFrom(arguments string, stage bool) ([]string, error) {
	if strings.HasPrefix(arguments, "--") {
		return nil, fmt.Errorf("COPY/ADD flags are not supported")
	}
	var operands []string
	if strings.HasPrefix(arguments, "[") {
		if err := json.Unmarshal([]byte(arguments), &operands); err != nil {
			return nil, fmt.Errorf("invalid COPY/ADD JSON operands")
		}
	} else {
		if strings.ContainsAny(arguments, "\"'") {
			return nil, fmt.Errorf("use JSON array syntax for COPY/ADD paths containing spaces")
		}
		operands = strings.Fields(arguments)
	}
	if len(operands) < 2 {
		return nil, fmt.Errorf("COPY/ADD require a local source and destination")
	}
	for _, source := range operands[:len(operands)-1] {
		name := source
		if stage {
			name = strings.TrimLeft(name, "/")
			if name == "" {
				name = "."
			}
		}
		if source == "" || strings.Contains(name, "<<") || !fsLocal(name) {
			return nil, fmt.Errorf("COPY/ADD sources must be local paths inside the context; remote URLs, variables and traversal are not supported")
		}
	}
	if strings.TrimSpace(operands[len(operands)-1]) == "" {
		return nil, fmt.Errorf("COPY/ADD destination is empty")
	}
	return operands, nil
}
