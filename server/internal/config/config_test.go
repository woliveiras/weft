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
