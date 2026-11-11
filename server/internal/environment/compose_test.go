package environment

import (
	"context"
	"strings"
	"testing"
)

func TestComposeDependencyReview(t *testing.T) {
	dir := t.TempDir()
	source := `services:
  dev: {image: alpine, depends_on: [cache]}
  cache: {image: alpine, command: [sleep, infinity], depends_on: [clock]}
  clock: {image: alpine, command: [sleep, infinity]}
  ignored: {image: alpine, privileged: true}
`
	writeDiscoveryFile(t, dir, "compose.dev.yaml", source)
	sel := discoverSelection(t, dir, "dev")
	env, err := Resolve(context.Background(), dir, sel)
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Companions) != 2 || env.Companions[0].Name != "clock" || env.Companions[1].Name != "cache" {
		t.Fatal(env.Companions)
	}
	if err := ValidateAccepted(context.Background(), dir, env); err != nil {
		t.Fatal(err)
	}
	env.Companions[0].Image = "different"
	if err := ValidateAccepted(context.Background(), dir, env); err == nil {
		t.Fatal("modified helper accepted")
	}
}
func TestComposeRejectsUnsupportedDependencies(t *testing.T) {
	for _, body := range []string{
		"dev: {image: alpine, depends_on: [missing]}",
		"dev: {image: alpine, depends_on: [cache]}, cache: {image: alpine, depends_on: [dev]}",
		"dev: {image: alpine, depends_on: {cache: {condition: service_healthy}}}, cache: {image: alpine}",
		"dev: {image: alpine, depends_on: [cache]}, cache: {image: alpine, ports: ['80:80']}",
		"dev: {image: alpine, depends_on: [cache]}, cache: {image: alpine, environment: {KEY: value}}",
		"dev: {image: alpine, depends_on: [cache]}, cache: {image: '${IMAGE}'}",
		"dev: {image: alpine, depends_on: [cache]}, cache: {image: alpine, command: 'sleep infinity'}",
		"dev: {image: alpine, depends_on: [cache]}, cache: {build: '.'}",
		"dev: {image: alpine, depends_on: [cache]}, cache: {image: alpine, volumes: ['/host:/data']}",
		"dev: {image: alpine, depends_on: [a,b,c,d]}, a: {image: alpine}, b: {image: alpine}, c: {image: alpine}, d: {image: alpine}",
	} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			writeDiscoveryFile(t, dir, "compose.yaml", "services: {"+body+"}")
			_, err := Resolve(context.Background(), dir, discoverSelection(t, dir, "dev"))
			if err == nil {
				t.Fatal("unsupported configuration accepted")
			}
		})
	}
}
func TestComposeVariantsAreNotMerged(t *testing.T) {
	dir := t.TempDir()
	writeDiscoveryFile(t, dir, "compose.yaml", "services: {dev: {image: alpine}}")
	writeDiscoveryFile(t, dir, "compose.override.yml", "services: {dev: {ports: ['80:80']}}")
	d, err := Discover(context.Background(), dir)
	if err != nil || len(d.Candidates) != 2 {
		t.Fatal(d, err)
	}
	for _, c := range d.Candidates {
		env, err := Resolve(context.Background(), dir, Selection{Source: d.Source, Path: c.Path, SHA256: c.SHA256, Service: "dev"})
		if strings.Contains(c.Path, "override") {
			if err == nil {
				t.Fatal("implicit merge")
			}
		} else if err != nil || env.Image != "alpine" {
			t.Fatal(env, err)
		}
	}
}
