package secrets_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/secrets"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, secrets.KeySize) }

func TestSealOpenRoundTrip(t *testing.T) {
	box, err := secrets.NewBox(key(1))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal([]byte("sk-test"), "llm/providers/a/apiKey")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "sk-test") || !strings.HasPrefix(sealed, "v1."+box.PrimaryKeyID()+".") {
		t.Fatalf("sealed value leaks or has the wrong shape: %q", sealed)
	}
	again, _ := box.Seal([]byte("sk-test"), "llm/providers/a/apiKey")
	if again == sealed {
		t.Fatal("two seals of the same value must differ (random nonce)")
	}
	pt, err := box.Open(sealed, "llm/providers/a/apiKey")
	if err != nil || string(pt) != "sk-test" {
		t.Fatalf("open: %q %v", pt, err)
	}
}

func TestOpenRejectsMovedTamperedAndForeignValues(t *testing.T) {
	box, _ := secrets.NewBox(key(1))
	sealed, _ := box.Seal([]byte("secret"), "record-a")
	if _, err := box.Open(sealed, "record-b"); !errors.Is(err, secrets.ErrCorrupt) {
		t.Fatalf("value moved to another record: want ErrCorrupt, got %v", err)
	}
	tampered := sealed[:len(sealed)-2] + "AA"
	if _, err := box.Open(tampered, "record-a"); !errors.Is(err, secrets.ErrCorrupt) {
		t.Fatalf("tampered: want ErrCorrupt, got %v", err)
	}
	for _, bad := range []string{"", "plain", "v2.x.y", "v1.nokey", "v1." + box.PrimaryKeyID() + ".!!!", "v1." + box.PrimaryKeyID() + ".AA"} {
		if _, err := box.Open(bad, "record-a"); !errors.Is(err, secrets.ErrCorrupt) {
			t.Errorf("Open(%q): want ErrCorrupt, got %v", bad, err)
		}
	}
	other, _ := secrets.NewBox(key(2))
	if _, err := other.Open(sealed, "record-a"); !errors.Is(err, secrets.ErrUnknownKey) {
		t.Fatalf("foreign key: want ErrUnknownKey, got %v", err)
	}
}

func TestRotationWithPreviousKeys(t *testing.T) {
	old, _ := secrets.NewBox(key(1))
	sealed, _ := old.Seal([]byte("token"), "r")
	rotated, err := secrets.NewBox(key(2), key(1))
	if err != nil {
		t.Fatal(err)
	}
	if !rotated.NeedsReseal(sealed) {
		t.Fatal("value sealed with the previous key must need a reseal")
	}
	pt, err := rotated.Open(sealed, "r")
	if err != nil || string(pt) != "token" {
		t.Fatalf("previous key must still open: %q %v", pt, err)
	}
	resealed, _ := rotated.Seal(pt, "r")
	if rotated.NeedsReseal(resealed) {
		t.Fatal("fresh seal must use the primary key")
	}
	if _, err := old.Open(resealed, "r"); !errors.Is(err, secrets.ErrUnknownKey) {
		t.Fatalf("old box cannot open new-key values: %v", err)
	}
}

func TestNoKeyRefusesToSeal(t *testing.T) {
	var box *secrets.Box
	if box.Enabled() {
		t.Fatal("nil box must not be enabled")
	}
	if _, err := box.Seal([]byte("x"), "r"); !errors.Is(err, secrets.ErrNoKey) {
		t.Fatalf("want ErrNoKey, got %v", err)
	}
	if _, err := box.Open("v1.x.y", "r"); !errors.Is(err, secrets.ErrNoKey) {
		t.Fatalf("want ErrNoKey, got %v", err)
	}
	if _, err := secrets.NewBox([]byte("short")); err == nil {
		t.Fatal("short key must be rejected")
	}
	if _, err := secrets.NewBox(key(1), []byte("short")); err == nil {
		t.Fatal("short previous key must be rejected")
	}
}

func TestParseKey(t *testing.T) {
	gen, err := secrets.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if k, err := secrets.ParseKey(gen); err != nil || len(k) != secrets.KeySize {
		t.Fatalf("generated key: %v", err)
	}
	if k, err := secrets.ParseKey(strings.Repeat("ab", 32)); err != nil || k[0] != 0xab {
		t.Fatalf("hex key: %v", err)
	}
	for _, bad := range []string{"", "   ", "not base64 !!", "c2hvcnQ="} {
		if _, err := secrets.ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) must fail", bad)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	gen, _ := secrets.GenerateKey()

	box, src, err := secrets.Load(secrets.Options{Key: gen, KeyFile: filepath.Join(dir, "ignored")})
	if err != nil || src != secrets.SourceEnv || !box.Enabled() {
		t.Fatalf("env key: %v %v", src, err)
	}

	box, src, err = secrets.Load(secrets.Options{KeyFile: filepath.Join(dir, "missing")})
	if err != nil || src != secrets.SourceNone || box.Enabled() {
		t.Fatalf("missing file without generate must disable secrets: %v %v", src, err)
	}

	path := filepath.Join(dir, "sub", "secret.key")
	box, src, err = secrets.Load(secrets.Options{KeyFile: path, Generate: true})
	if err != nil || src != secrets.SourceGenerated || !box.Enabled() {
		t.Fatalf("generate: %v %v", src, err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("generated key file must be 0600: %v %v", st.Mode(), err)
	}
	sealed, _ := box.Seal([]byte("v"), "r")

	// A restart reads the generated file and opens what was sealed before.
	box2, src, err := secrets.Load(secrets.Options{KeyFile: path, Generate: true})
	if err != nil || src != secrets.SourceFile || box2.PrimaryKeyID() != box.PrimaryKeyID() {
		t.Fatalf("reload: %v %v", src, err)
	}
	if pt, err := box2.Open(sealed, "r"); err != nil || string(pt) != "v" {
		t.Fatalf("open after restart: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "bad.key"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := secrets.Load(secrets.Options{KeyFile: filepath.Join(dir, "bad.key")}); err == nil {
		t.Fatal("invalid key file must fail loudly")
	}
	if _, _, err := secrets.Load(secrets.Options{Key: gen, Previous: []string{"bad"}}); err == nil {
		t.Fatal("invalid previous key must fail loudly")
	}
}

func TestRedact(t *testing.T) {
	got := secrets.Redact(`401: invalid key "sk-abc123" for sk-abc123`, "sk-abc123", "", "ab")
	if strings.Contains(got, "sk-abc123") || !strings.Contains(got, "[redacted]") {
		t.Fatalf("Redact = %q", got)
	}
}
