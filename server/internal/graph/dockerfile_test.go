package graph

import "testing"

func TestDockerfileStageTargets(t *testing.T) {
	source := "FROM alpine AS build\nFROM build AS \\\n runtime\nFROM alpine\n"
	stages, err := DockerfileStages(source)
	if err != nil || len(stages) != 3 || stages[1] != "runtime" || stages[2] != "" {
		t.Fatalf("stages %v %v", stages, err)
	}
	for _, target := range []string{"", "build", "runtime", "BUILD"} {
		if err := ValidateBuildTarget(source, target); err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range []string{"missing", "0", "--target", "runtime extra", "$TARGET"} {
		if err := ValidateBuildTarget(source, target); err == nil {
			t.Fatalf("accepted %s", target)
		}
	}
	for _, source := range []string{"FROM", "FROM alpine AS", "FROM alpine AS build\nFROM alpine AS BUILD", "FROM alpine AS 0", "FROM alpine AS scratch", "FROM alpine \\"} {
		if _, err := DockerfileStages(source); err == nil {
			t.Fatalf("accepted %s", source)
		}
	}
}
