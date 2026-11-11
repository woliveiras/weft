package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestSandboxCapacityConfiguration(t *testing.T) {
	c := Default()
	c.Auth.Disabled = true
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	if c.Isolation.Docker.Capacity.TotalCPU != "4" || c.Isolation.Docker.Capacity.DefaultCPU != "1" || c.Isolation.Docker.Capacity.MaxActive != 4 || c.Isolation.Docker.Capacity.DefaultMemory != "1g" {
		t.Fatal(c.Isolation.Docker.Capacity)
	}
	for _, value := range []string{"{totalCPU: 0}", "{defaultCPU: -1}", "{totalCPU: 0.5, defaultCPU: 1}", "{maxActive: 0}", "{totalMemory: -1g}", "{totalMemory: 64m, defaultMemory: 128m}"} {
		c := Default()
		c.Auth.Disabled = true
		if e := yaml.Unmarshal([]byte("isolation:\n  docker:\n    capacity: "+value), c); e != nil {
			t.Fatal(e)
		}
		if e := c.Validate(); e == nil {
			t.Fatal(value)
		}
	}
}

func TestSandboxIdleConfiguration(t *testing.T) {
	if Default().Isolation.Docker.IdleTimeout != 5*time.Minute {
		t.Fatal("idle default")
	}
	for _, value := range []string{"0s", "2m", "-1s"} {
		c := Default()
		c.Auth.Disabled = true
		if err := yaml.Unmarshal([]byte("isolation:\n  docker:\n    idleTimeout: "+value), c); err != nil {
			t.Fatal(err)
		}
		err := c.Validate()
		if (err != nil) != (value == "-1s") {
			t.Fatalf("%s: %v", value, err)
		}
	}
}
