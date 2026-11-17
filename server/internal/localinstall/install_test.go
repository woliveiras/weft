package localinstall

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	web := filepath.Join(base, "web")
	if err := os.Mkdir(web, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(base, "state"), web
}
func TestProfileBoundaries(t *testing.T) {
	root, web := fixture(t)
	c, err := Profile(root, web, 8787)
	if err != nil {
		t.Fatal(err)
	}
	if c.Auth.Disabled || c.Server.Addr != "127.0.0.1:8787" || c.Storage.Type != "sqlite" || c.Queue.Type != "fs" || c.Secrets.KeyEnv != "" || !c.Server.ProtectMetrics {
		t.Fatal("unsafe native defaults")
	}
	for _, port := range []int{0, 80, 65536} {
		if _, err := Profile(root, web, port); err == nil {
			t.Fatal(port)
		}
	}
	if _, err := Profile("relative", web, 8787); err == nil {
		t.Fatal("relative data root accepted")
	}
}
func TestInitAndLockPreserveExistingData(t *testing.T) {
	root, web := fixture(t)
	if err := Init(root, web, 8787); err != nil {
		t.Fatal(err)
	}
	if err := Init(root, web, 8787); err == nil {
		t.Fatal("reinitialized data")
	}
	info, err := os.Stat(filepath.Join(root, "admin.token"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("token permissions", err)
	}
	t.Setenv("WEFT_AUTH_DISABLED", "true")
	t.Setenv("WEFT_ADDR", ":9999")
	cfg, release, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Disabled || cfg.Server.Addr != "127.0.0.1:8787" {
		t.Fatal("imported another installation environment")
	}
	if _, _, err := Open(root); err == nil {
		t.Fatal("duplicate instance lock")
	}
	release()
	_, release, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err = os.WriteFile(filepath.Join(root, "installation.json"), []byte(`{"version":999}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Open(root); err == nil {
		t.Fatal("unknown format accepted")
	}
}
func TestMissingAssetsAndPrivateDirectory(t *testing.T) {
	root, web := fixture(t)
	if err := Init(root, web+"-missing", 8787); err == nil {
		t.Fatal("missing assets accepted")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("data created without assets")
	}
	if err := Init(root, web, 8787); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(root); err == nil {
		t.Fatal("shared data root accepted")
	}
}

func TestInvalidLogFormatDoesNotOpenProfile(t *testing.T) {
	if err := RunWithLogFormat(t.Context(), "/nonexistent-profile", "invalid"); err == nil || err.Error() != "log format must be console, text or json" {
		t.Fatal(err)
	}
}
