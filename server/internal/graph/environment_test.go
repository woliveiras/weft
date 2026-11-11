package graph

import (
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestValidateEnvironment(t *testing.T) {
	check := func(env domain.EnvironmentSpec) map[string]bool {
		t.Helper()
		g := &domain.Graph{Metadata: domain.GraphMetadata{Name: "e"}, Spec: domain.GraphSpec{
			Environment: &env, Nodes: []domain.Node{{ID: "a", Prompt: "p"}},
		}}
		paths := map[string]bool{}
		for _, i := range Validate(g, nil).Errors {
			paths[i.Path] = true
		}
		return paths
	}
	ok := domain.EnvironmentSpec{Image: "golang:1.26", Setup: []string{"go mod download"}, Caches: []string{"/home/sandbox/go/pkg/mod"},
		Network: domain.EnvironmentNetwork{Setup: "bridge", Run: "none"}, Timeout: domain.Duration(time.Hour)}
	if errs := check(ok); len(errs) != 0 {
		t.Fatalf("valid environment rejected: %v", errs)
	}
	if errs := check(domain.EnvironmentSpec{Dockerfile: "# comment\nfrom alpine:3.22\n"}); len(errs) != 0 {
		t.Fatalf("Dockerfile rejected: %v", errs)
	}
	for name, c := range map[string]struct {
		env  domain.EnvironmentSpec
		path string
	}{
		"neither":         {domain.EnvironmentSpec{}, "spec.environment"},
		"both":            {domain.EnvironmentSpec{Image: "a", Dockerfile: "FROM a"}, "spec.environment"},
		"bad image":       {domain.EnvironmentSpec{Image: "--privileged"}, "spec.environment.image"},
		"no FROM":         {domain.EnvironmentSpec{Dockerfile: "RUN true"}, "spec.environment.dockerfile"},
		"empty setup":     {domain.EnvironmentSpec{Image: "a", Setup: []string{"ok", " "}}, "spec.environment.setup[1]"},
		"bad network":     {domain.EnvironmentSpec{Image: "a", Network: domain.EnvironmentNetwork{Run: "host"}}, "spec.environment.network.run"},
		"relative cache":  {domain.EnvironmentSpec{Image: "a", Caches: []string{"cache"}}, "spec.environment.caches[0]"},
		"workspace cache": {domain.EnvironmentSpec{Image: "a", Caches: []string{"/workspace/node_modules"}}, "spec.environment.caches[0]"},
		"system cache":    {domain.EnvironmentSpec{Image: "a", Caches: []string{"/usr"}}, "spec.environment.caches[0]"},
		"dup cache":       {domain.EnvironmentSpec{Image: "a", Caches: []string{"/home/sandbox/.cache", "/home/sandbox/.cache/"}}, "spec.environment.caches[1]"},
		"long timeout":    {domain.EnvironmentSpec{Image: "a", Timeout: domain.Duration(3 * time.Hour)}, "spec.environment.timeout"},
	} {
		if errs := check(c.env); !errs[c.path] {
			t.Errorf("%s: want error at %s, got %v", name, c.path, errs)
		}
	}
}

func TestValidateRepositoryContext(t *testing.T) {
	for _, name := range []string{".", "service", "packages/app"} {
		env := &domain.EnvironmentSpec{Dockerfile: "FROM alpine", Context: name, Origin: &domain.EnvironmentOrigin{}}
		if err := ValidateEnvironment(env).Err(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, env := range []*domain.EnvironmentSpec{
		{Dockerfile: "FROM alpine", Context: "."},
		{Image: "alpine", Context: ".", Origin: &domain.EnvironmentOrigin{}},
		{Dockerfile: "FROM alpine", Context: "../outside", Origin: &domain.EnvironmentOrigin{}},
		{Dockerfile: "FROM alpine", Context: "/absolute", Origin: &domain.EnvironmentOrigin{}},
		{Dockerfile: "FROM alpine", Context: "$VARIABLE", Origin: &domain.EnvironmentOrigin{}},
	} {
		if err := ValidateEnvironment(env).Err(); err == nil {
			t.Fatalf("accepted invalid context: %+v", env)
		}
	}
}
