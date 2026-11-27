package config

import "testing"

func TestOAuthPublicURLSetting(t *testing.T) {
	c := Default()
	if c.Server.PublicURL != "http://localhost:8080" {
		t.Fatal(c.Server.PublicURL)
	}
	t.Setenv("WEFT_PUBLIC_URL", "https://weft.example")
	applyEnv(c)
	if c.Server.PublicURL != "https://weft.example" {
		t.Fatal(c.Server.PublicURL)
	}
}

func TestKubernetesImageSelectsInstallation(t *testing.T) {
	c := Default()
	if c.Isolation.Kubernetes != nil {
		t.Fatal("unexpected Kubernetes profile")
	}
	t.Setenv("WEFT_KUBERNETES_SANDBOX_IMAGE", "installed/sandbox:v1")
	applyEnv(c)
	if c.Isolation.Kubernetes == nil || c.Isolation.Kubernetes.Image != "installed/sandbox:v1" {
		t.Fatal(c.Isolation)
	}
}
