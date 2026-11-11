package repositoryimport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Authentication is an explicit per-attempt selection, never persisted or echoed.
// Tokens live only in request/worker memory and are not sent to Git subprocesses.
type Authentication struct {
	Mode     string `json:"mode"`
	Username string `json:"username,omitempty"`
	Token    string `json:"token,omitempty"`
}

func (a Authentication) validate(native bool) error {
	switch a.Mode {
	case "", "anonymous":
		if a.Username != "" || a.Token != "" {
			return ErrInvalid
		}
	case "token":
		if !validCredential(a.Username, 256) || strings.Contains(a.Username, ":") || !validCredential(a.Token, 8192) {
			return ErrInvalid
		}
	case "local_git", "local_gh":
		if !native || a.Username != "" || a.Token != "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func validCredential(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}
func (a Authentication) resolve(ctx context.Context, source, dir string) (Authentication, error) {
	if a.Mode != "local_git" && a.Mode != "local_gh" {
		return a, nil
	}
	u, err := url.Parse(source)
	if err != nil {
		return Authentication{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if a.Mode == "local_git" {
		// Run outside the downloaded repository. Only explicitly selected host helpers
		// may execute; neither approve/store nor erase is called.
		cmd = exec.CommandContext(ctx, "git", "-c", "credential.interactive=false", "-c", "credential.useHttpPath=true", "-c", "core.askPass=", "credential", "fill")
		cmd.Stdin = strings.NewReader("protocol=https\nhost=" + u.Host + "\npath=" + strings.TrimPrefix(u.Path, "/") + "\n\n")
	} else {
		cmd = exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", u.Host)
	}
	cmd.Dir = dir
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") && !strings.HasPrefix(e, "GH_") && !strings.HasPrefix(e, "GITHUB_") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=", "GH_PROMPT_DISABLED=1")
	var out bytes.Buffer
	cmd.Stdout = &authOutput{writer: &out, left: 16384}
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	isolateProcess(cmd)
	if err = cmd.Run(); err != nil {
		return Authentication{}, errors.New("Local authentication unavailable. Configure the selected Git helper or GitHub CLI on the native Weft host, or provide a token and retry.")
	}
	result := Authentication{Mode: "token"}
	if a.Mode == "local_gh" {
		result.Username = "x-access-token"
		result.Token = strings.TrimSpace(out.String())
	} else {
		for _, line := range strings.Split(out.String(), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok {
				switch key {
				case "username":
					result.Username = value
				case "password":
					result.Token = value
				}
			}
		}
	}
	if result.validate(false) != nil {
		return Authentication{}, errors.New("Local authentication did not return supported HTTPS username/token credentials. Provide an explicit token and retry.")
	}
	return result, nil
}

type authOutput struct {
	writer io.Writer
	left   int
}

func (w *authOutput) Write(p []byte) (int, error) {
	if len(p) > w.left {
		return 0, errors.New("credential output too large")
	}
	w.left -= len(p)
	return w.writer.Write(p)
}
