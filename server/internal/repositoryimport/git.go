package repositoryimport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const maxTransfer = int64(128 << 20)
const maxDuration = 3 * time.Minute

func safeGit(ctx context.Context, dir string, args ...string) (string, error) {
	argv := []string{"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "http.proxy=", "-c", "http.followRedirects=false", "-c", "protocol.allow=never", "-c", "protocol.http.allow=always", "-c", "fetch.fsckObjects=true", "-c", "transfer.fsckObjects=true", "-c", "fetch.unpackLimit=0", "-c", "gc.auto=0", "-C", dir}
	cmd := exec.CommandContext(ctx, "git", append(argv, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "GIT_LFS_SKIP_SMUDGE=1", "LC_ALL=C"}
	cmd.WaitDelay = time.Second
	isolateProcess(cmd)
	var out bytes.Buffer
	cmd.Stdout = &limitedOutput{writer: &out, left: 4096}
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", errors.New("Git operation failed")
	}
	return strings.TrimSpace(out.String()), nil
}

type limitedOutput struct {
	writer io.Writer
	left   int
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > w.left {
		p = p[:w.left]
	}
	if len(p) > 0 {
		_, err := w.writer.Write(p)
		if err != nil {
			return 0, err
		}
		w.left -= len(p)
	}
	return n, nil
}
func fetch(ctx context.Context, dir, source, ref string, client *http.Client, received *atomic.Int64, auth ...Authentication) (string, string) {
	endpoint, close, status, err := bridge(ctx, source, client, maxTransfer, received, auth...)
	if err != nil {
		return "", "Cannot initialize the bounded Git transport."
	}
	defer close()
	template := filepath.Join(dir, "empty-template")
	if err = os.Mkdir(template, 0700); err != nil {
		return "", "Cannot create the private Git template directory."
	}
	steps := [][]string{{"init", "-q", "--template=" + template, "-b", "imported"}, {"fetch", "--no-tags", "--depth=1", "--no-recurse-submodules", "--", endpoint, ref}, {"update-ref", "refs/heads/imported", "FETCH_HEAD"}, {"remote", "add", "origin", source}}
	for _, args := range steps {
		if _, err = safeGit(ctx, dir, args...); err != nil {
			if ctx.Err() != nil {
				return "", "Import cancelled or exceeded the three-minute deadline; partial files are retained."
			}
			switch status.Load() {
			case 401, 403:
				return "", "Repository authentication or access was denied. Select Git authentication, check repository read access and any organization approval, then retry with current credentials. MCP OAuth is separate."
			case 301, 302, 303, 307, 308:
				return "", "Repository redirected the request. Use its canonical HTTPS clone URL. Redirects are not followed."
			case 413:
				return "", "Repository exceeded the 128 MiB transfer limit; partial files are retained."
			case 415:
				return "", "The endpoint does not support smart Git HTTP. Use an HTTPS clone URL."
			case 502:
				return "", "Cannot reach a public HTTPS repository. Check its URL, DNS and network access."
			default:
				return "", "Git could not import this reference. Check the public clone URL, branch/tag/commit and installed Git. Partial files are retained."
			}
		}
	}
	head, err := safeGit(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", "Cannot resolve the imported commit."
	}
	return head, ""
}
