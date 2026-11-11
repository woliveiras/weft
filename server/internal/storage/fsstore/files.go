package fsstore

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// encodeName turns an arbitrary key into a portable file name. Lowercase
// letters, digits, '.', '_' and '-' are kept so names stay readable;
// everything else - including uppercase letters, because macOS and Windows
// file systems are case-insensitive by default and "Notes" must not collide
// with "notes" - is written as %XX. A leading '.' is escaped too so encoded
// names never look like the store's hidden temp files.
func encodeName(s string) string {
	if s == "" {
		return "%"
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		keep := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || (c == '.' && i > 0)
		if keep {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02x", c)
		}
	}
	name := b.String()
	if isReservedWindowsName(name) {
		name += "_"
	}
	return name
}

func decodeName(name string) (string, error) {
	if name == "%" {
		return "", nil
	}
	name = strings.TrimSuffix(name, "_")
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		if name[i] != '%' {
			b.WriteByte(name[i])
			continue
		}
		if i+2 >= len(name) {
			return "", fmt.Errorf("fsstore: bad encoded name %q", name)
		}
		var c byte
		if _, err := fmt.Sscanf(name[i+1:i+3], "%02x", &c); err != nil {
			return "", fmt.Errorf("fsstore: bad encoded name %q: %w", name, err)
		}
		b.WriteByte(c)
		i += 2
	}
	return b.String(), nil
}

func isReservedWindowsName(name string) bool {
	base := strings.ToLower(name)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "con", "prn", "aux", "nul",
		"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		return true
	}
	return false
}

// writeFileAtomic writes data so that readers (and a restart after a crash)
// see either the old file or the complete new one: write a temp file in the
// same directory, fsync it, rename it over the target, fsync the directory.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return syncDir(dir)
}

// removeFile deletes a file (missing is fine) and makes the removal durable.
func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// syncDir fsyncs a directory so a rename or unlink in it survives power
// loss. Windows cannot open directories for syncing; NTFS journals metadata
// itself, so it is skipped there.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil && !isUnsupportedSync(err) {
		return err
	}
	return nil
}

func isUnsupportedSync(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "invalid argument") || strings.Contains(msg, "not supported")
}

// cleanTemp removes temp files left by a crash in the middle of a write.
func cleanTemp(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		name := d.Name()
		if d.IsDir() && strings.HasPrefix(name, ".trash-") {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasPrefix(name, ".tmp-") {
			return os.Remove(path)
		}
		return nil
	})
}
