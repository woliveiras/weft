package projectconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

const checkoutPrefix = "project-checkouts/"

var taskCheckoutID = regexp.MustCompile(`^task-run-[a-z0-9]+$`)

// TaskCheckout is platform-owned admission provenance, outside the repository
// an agent can edit. A repository marker never authorizes cleanup or inspection.
type TaskCheckout struct {
	ProjectID   string `json:"projectId"`
	GraphID     string `json:"graphId"`
	Base        string `json:"base"`
	Path        string `json:"path"`
	ArchivePath string `json:"archivePath"`
}

func (b TaskCheckout) valid(p *Project, graphID, dir string) bool {
	return p != nil && b.ProjectID == p.ID && b.GraphID == graphID && taskCheckoutID.MatchString(graphID) && commitPattern.MatchString(b.Base) &&
		filepath.IsAbs(dir) && filepath.Clean(dir) == dir && b.Path == dir && filepath.Base(dir) == graphID &&
		b.ArchivePath == filepath.Join(filepath.Dir(dir), ".task-checkouts", graphID+".git") && dir != p.Path
}

func (m Manager) GetTaskCheckout(ctx context.Context, graphID string) (*TaskCheckout, error) {
	if !taskCheckoutID.MatchString(graphID) {
		return nil, ErrInvalid
	}
	s, err := m.Store.GetSetting(ctx, checkoutPrefix+graphID)
	if err != nil {
		return nil, err
	}
	var b TaskCheckout
	if err = json.Unmarshal(s.Value, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// CreateTaskCheckout copies only the reviewed commit's ancestry into a fresh
// repository. Its .git is local to the one workspace mounted in the sandbox;
// source refs, hooks, remotes, configuration and dirty files are never copied.
// Failed preparation retains its durable binding and partial checkout for
// inspection; neither retry nor an existing directory is silently replaced.
func (m Manager) CreateTaskCheckout(ctx context.Context, p *Project, commit, root, graphID, actor string) (string, error) {
	if err := Verify(ctx, p, commit); err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	b := TaskCheckout{ProjectID: p.ID, GraphID: graphID, Base: commit, Path: filepath.Join(root, graphID), ArchivePath: filepath.Join(root, ".task-checkouts", graphID+".git")}
	if !b.valid(p, graphID, b.Path) {
		return "", ErrInvalid
	}
	if _, err = os.Lstat(b.Path); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			err = fmt.Errorf("%w: task workspace already exists; no automatic replacement", storage.ErrConflict)
		}
		return "", err
	}
	if _, err = os.Lstat(b.ArchivePath); !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: retained task repository already exists or is unavailable", storage.ErrConflict)
	}
	data, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	if _, err = m.Store.PutSetting(ctx, checkoutPrefix+graphID, data, 0, actor); err != nil {
		return "", err
	}
	if err = createIndependentCheckout(ctx, p.Path, commit, b.Path, "weft/"+graphID); err != nil {
		return "", err
	}
	return b.Path, nil
}

// checkoutCommand never uses caller Git routing, host credentials/configuration,
// lazy network fetches, checkout filters or repository executable hooks.
func checkoutCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	argv := append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "protocol.allow=never", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_LFS_SKIP_SMUDGE=1", "GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0"}
	return cmd
}

func checkoutGit(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	b, err := checkoutCommand(ctx, dir, args...).Output()
	if err != nil {
		return "", fmt.Errorf("%w: task Git operation failed", ErrInvalid)
	}
	return strings.TrimSpace(string(b)), nil
}

func createIndependentCheckout(ctx context.Context, source, commit, dir, branch string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	format, err := checkoutGit(ctx, source, "rev-parse", "--show-object-format")
	if err != nil {
		return err
	}
	if format != "sha1" && format != "sha256" {
		return ErrInvalid
	}
	if err = os.Mkdir(dir, 0700); err != nil {
		return err
	}
	if _, err = checkoutGit(ctx, dir, "init", "--quiet", "--template=", "--object-format="+format); err != nil {
		return err
	}
	// Stream a new pack, rather than clone --local/--shared: no hardlinked
	// source objects, alternates, unrelated refs or source config can escape.
	pack := checkoutCommand(ctx, source, "pack-objects", "--stdout", "--revs")
	pack.Stdin = strings.NewReader(commit + "\n")
	stream, err := pack.StdoutPipe()
	if err != nil {
		return err
	}
	index := checkoutCommand(ctx, dir, "index-pack", "--stdin")
	index.Stdin = stream
	if err = pack.Start(); err != nil {
		return fmt.Errorf("%w: task object transfer failed", ErrInvalid)
	}
	indexErr := index.Run()
	_ = stream.Close()
	packErr := pack.Wait()
	if indexErr != nil || packErr != nil {
		return fmt.Errorf("%w: task object transfer failed", ErrInvalid)
	}
	_, err = checkoutGit(ctx, dir, "checkout", "--quiet", "-b", branch, commit)
	return err
}

// TaskInspectionBase chooses metadata only from platform admission provenance.
// Existing linked worktrees keep their legacy source metadata boundary.
func (m Manager) TaskInspectionBase(ctx context.Context, p *Project, graphID, dir string) (string, error) {
	b, err := m.GetTaskCheckout(ctx, graphID)
	if errors.Is(err, storage.ErrNotFound) {
		return p.Path, nil
	}
	if err != nil {
		return "", err
	}
	if !b.valid(p, graphID, dir) {
		return "", fmt.Errorf("%w: task checkout binding changed", ErrInvalid)
	}
	if err = localTaskMetadata(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func localTaskMetadata(dir string) error {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil || real != dir {
		return fmt.Errorf("%w: task checkout path changed", ErrInvalid)
	}
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: task metadata is not local", ErrInvalid)
	}
	for _, name := range []string{"commondir", "objects/info/alternates", "objects/info/http-alternates"} {
		if _, err := os.Lstat(filepath.Join(dir, ".git", name)); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: task metadata redirects outside its checkout", ErrInvalid)
		}
	}
	for _, name := range []string{"objects", "refs"} {
		path := filepath.Join(dir, ".git", name)
		real, err := filepath.EvalSymlinks(path)
		if err != nil || real != path {
			return fmt.Errorf("%w: task metadata path changed", ErrInvalid)
		}
	}
	return nil
}
