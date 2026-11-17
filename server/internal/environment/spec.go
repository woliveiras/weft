// Package environment builds and tracks project environments: the container
// images agents' commands run in. A graph declares one in spec.environment
// (a base image or Dockerfile, setup commands, caches); the Manager builds it
// on the sandbox daemon, records its state, and hands the image to docker
// sandboxes. Agents never build or reach Docker themselves.
package environment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
)

// SandboxUID is the user commands run as; workspace files belong to it.
const SandboxUID = 10001

// SandboxHome is HOME inside environment images.
const SandboxHome = "/home/sandbox"

// wrapperVersion changes the hash of every environment when the generated
// user layer changes, so existing images are rebuilt.
const wrapperVersion = 2

// Hash identifies the image a definition produces. Settings that do not
// change the image (run network, timeout, setup limits) are excluded, so
// editing them never forces a rebuild.
func Hash(env *domain.EnvironmentSpec) string { return definitionDigest(env)[:12] }

func definitionDigest(env *domain.EnvironmentSpec) string {
	caches := append([]string(nil), env.Caches...)
	for i := range caches {
		caches[i] = path.Clean(caches[i])
	}
	sort.Strings(caches)
	b, _ := json.Marshal(struct {
		SetupHosts    []string `json:"setupHosts,omitempty"`
		V             int      `json:"v"`
		Image         string   `json:"image,omitempty"`
		Dockerfile    string   `json:"dockerfile,omitempty"`
		Setup         []string `json:"setup,omitempty"`
		Network       string   `json:"network"`
		Caches        []string `json:"caches,omitempty"`
		Target        string   `json:"target,omitempty"`
		Context       string   `json:"context,omitempty"`
		ContextSHA256 string   `json:"contextSHA256,omitempty"`
	}{sortedHosts(env.Network.SetupAllowedHosts), wrapperVersion, strings.TrimSpace(env.Image), strings.TrimSpace(env.Dockerfile), env.Setup, SetupNetwork(env), caches, env.Target, env.Context, contextHash(env)})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// SetupNetwork is the network of setup commands (default bridge).
func SetupNetwork(env *domain.EnvironmentSpec) string {
	if env.Network.Setup == "" {
		return "bridge"
	}
	return env.Network.Setup
}

// Repository is the image repository of a graph's environments.
func Repository(graphID string) string { return "weft-env/" + graphID }

// ImageRef is the final image of a definition.
func ImageRef(graphID, hash string) string { return Repository(graphID) + ":" + hash }

// CacheVolume is the named volume backing one cache directory.
func CacheVolume(graphID, dir string) string {
	sum := sha256.Sum256([]byte(path.Clean(dir)))
	return fmt.Sprintf("weft-env-%s-%s", graphID, hex.EncodeToString(sum[:])[:8])
}

// UserLayer is the Dockerfile Weft adds on top of the base image: a
// sandbox user with UID 10001 (so files written in the workspace keep the
// platform's owner, and agents never run as root), HOME, and the cache
// directories owned by that user. It needs /bin/sh in the base image.
func UserLayer(base string, caches []string) string {
	dirs := []string{SandboxHome, "/workspace"}
	for _, c := range caches {
		dirs = append(dirs, path.Clean(c))
	}
	quoted := make([]string, len(dirs))
	for i, d := range dirs {
		quoted[i] = "'" + strings.ReplaceAll(d, "'", `'\''`) + "'"
	}
	return fmt.Sprintf(`FROM %s
USER root
RUN set -e; \
    if ! grep -q '^[^:]*:[^:]*:%[2]d:' /etc/passwd 2>/dev/null; then \
      if command -v useradd >/dev/null 2>&1; then useradd --uid %[2]d --create-home --home-dir %[3]s --shell /bin/sh sandbox; \
      elif command -v adduser >/dev/null 2>&1; then adduser -D -u %[2]d -h %[3]s -s /bin/sh sandbox; \
      else echo 'sandbox:x:%[2]d:%[2]d::%[3]s:/bin/sh' >> /etc/passwd; fi; \
    fi; \
    mkdir -p %[4]s; \
    chown -R %[2]d %[4]s
ENV HOME=%[3]s
USER %[2]d
WORKDIR /workspace
`, base, SandboxUID, SandboxHome, strings.Join(quoted, " "))
}

// SetupScript joins setup commands into one shell script that stops at the
// first failure and echoes each command into the build log.
func SetupScript(cmds []string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	for _, c := range cmds {
		fmt.Fprintf(&b, "echo %s\n%s\n", shellQuote("$ "+c), c)
	}
	return b.String()
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func contextHash(env *domain.EnvironmentSpec) string {
	if env.Origin == nil {
		return ""
	}
	return env.Origin.ContextSHA256
}

// BuildKit calls its ordinary bridge-backed network "default".
func buildNetwork(env *domain.EnvironmentSpec) string {
	if SetupNetwork(env) == "none" {
		return "none"
	}
	return "default"
}

func sortedHosts(hosts []string) []string {
	result := append([]string(nil), hosts...)
	sort.Strings(result)
	return result
}
