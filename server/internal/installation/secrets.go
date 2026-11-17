package installation

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const MaxSecretBytes = 16 << 10

var envPattern = regexp.MustCompile(`^WEFT_SECRET_[A-Z0-9_]+$`)

type SecretRef struct {
	Env      string `json:"env,omitempty"`
	File     string `json:"file,omitempty"`
	Preserve bool   `json:"preserve,omitempty"`
}
type Resolver struct {
	Dir       string
	DeniedEnv []string
}

func (r Resolver) Resolve(ref SecretRef) (string, error) {
	count := 0
	if ref.Env != "" {
		count++
	}
	if ref.File != "" {
		count++
	}
	if ref.Preserve {
		count++
	}
	if count != 1 {
		return "", fmt.Errorf("secretRef: select exactly one of env, file or preserve")
	}
	if ref.Preserve {
		return "", nil
	}
	var value string
	if ref.Env != "" {
		if !envPattern.MatchString(ref.Env) || strings.HasPrefix(ref.Env, "WEFT_SECRET_KEY") {
			return "", fmt.Errorf("secretRef.env: forbidden variable")
		}
		for _, name := range r.DeniedEnv {
			if name != "" && name == ref.Env {
				return "", fmt.Errorf("secretRef.env: forbidden variable")
			}
		}
		value = os.Getenv(ref.Env)
	} else {
		if r.Dir == "" {
			return "", fmt.Errorf("secretRef.file: secret reference unresolved")
		}
		if !filepath.IsLocal(ref.File) || strings.Contains(ref.File, "\\") {
			return "", fmt.Errorf("secretRef.file: path must remain inside secrets.dir")
		}
		for _, part := range strings.Split(ref.File, "/") {
			if part == ".." {
				return "", fmt.Errorf("secretRef.file: traversal is forbidden")
			}
		}
		// os.Root enforces containment during path resolution, including symlink races.
		root, err := os.OpenRoot(r.Dir)
		if err != nil {
			return "", fmt.Errorf("secretRef.file: secret reference unresolved")
		}
		defer root.Close()
		info, err := root.Stat(ref.File)
		if err != nil || !info.Mode().IsRegular() || info.Size() > MaxSecretBytes {
			return "", fmt.Errorf("secretRef.file: unresolved, escaped or invalid file")
		}
		f, err := root.OpenFile(ref.File, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return "", fmt.Errorf("secretRef.file: secret reference unresolved")
		}
		defer f.Close()
		info, err = f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("secretRef.file: expected regular file")
		}
		b, err := io.ReadAll(io.LimitReader(f, MaxSecretBytes+1))
		if err != nil || len(b) > MaxSecretBytes {
			return "", fmt.Errorf("secretRef.file: unreadable or oversized file")
		}
		value = string(b)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("secretRef: secret reference unresolved")
	}
	if len(value) > MaxSecretBytes {
		return "", fmt.Errorf("secretRef: exceeds 16 KiB")
	}
	return value, nil
}
