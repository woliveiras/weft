// Package projectconfig registers explicitly selected server-side repositories.
// Registration reads Git metadata; only reviewed task admission creates worktrees.
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
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

const prefix = "projects/"

var ErrInvalid = errors.New("invalid project")
var idPattern = regexp.MustCompile(`^prj_[a-z0-9]{1,80}$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

type Input struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type Project struct {
	ImportID      string `json:"importId,omitempty"`
	RepositoryURL string `json:"repositoryUrl,omitempty"`
	RepositoryRef string `json:"repositoryRef,omitempty"`
	ExecutionMode string `json:"executionMode,omitempty"`
	// Checks are the project's declared verification commands; task start
	// merges them with the workflow's (see domain.MergeChecks).
	Checks []domain.Check `json:"checks,omitempty"`
	Input
	ID        string    `json:"id"`
	Revision  int64     `json:"revision"`
	Head      string    `json:"head"`
	CreatedAt time.Time `json:"createdAt"`
}
type Manager struct{ Store storage.Store }

func Validate(in Input) error {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 || !utf8.ValidString(in.Name) || strings.ContainsRune(in.Name, 0) || !filepath.IsAbs(in.Path) || len(in.Path) > 4096 || strings.ContainsAny(in.Path, "\x00\n\r") {
		return fmt.Errorf("%w: name and absolute repository path are required", ErrInvalid)
	}
	return nil
}
func git(ctx context.Context, dir string, args ...string) (string, error) {
	return gitCommand(ctx, dir, false, args...)
}
func gitCommand(ctx context.Context, dir string, managed bool, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	argv := append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	// Caller-controlled Git routing cannot substitute a different repository.
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	if managed {
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_LFS_SKIP_SMUDGE=1"}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0")
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: Git operation failed; check repository access and Git installation", ErrInvalid)
	}
	return strings.TrimSpace(string(b)), nil
}
func Inspect(ctx context.Context, path string) (string, string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", fmt.Errorf("%w: repository path is unavailable", ErrInvalid)
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return "", "", err
	}
	root, err := git(ctx, real, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || canonical != real {
		return "", "", fmt.Errorf("%w: select the repository root", ErrInvalid)
	}
	head, err := git(ctx, real, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", "", err
	}
	if !commitPattern.MatchString(head) {
		return "", "", fmt.Errorf("%w: unsupported commit identifier", ErrInvalid)
	}
	return real, head, nil
}
func (m Manager) Create(ctx context.Context, in Input, actor string) (*Project, error) {
	if err := Validate(in); err != nil {
		return nil, err
	}
	path, head, err := Inspect(ctx, in.Path)
	if err != nil {
		return nil, err
	}
	p := &Project{Input: Input{Name: in.Name, Path: path}, ID: domain.NewID("prj"), Head: head, CreatedAt: time.Now().UTC()}
	if err = m.Store.AppendEvent(ctx, domain.NewEvent("project.registration_requested", "", map[string]any{"projectId": p.ID}).WithActor(actor)); err != nil {
		return nil, err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	p.Revision, err = m.Store.PutSetting(ctx, prefix+p.ID, b, 0, actor)
	return p, err
}
func decode(s *storage.Setting) (*Project, error) {
	var p Project
	if err := json.Unmarshal(s.Value, &p); err != nil {
		return nil, err
	}
	p.Revision = s.Revision
	return &p, nil
}
func (m Manager) Get(ctx context.Context, id string) (*Project, error) {
	if !idPattern.MatchString(id) {
		return nil, storage.ErrNotFound
	}
	r, err := m.Store.GetSetting(ctx, prefix+id)
	if err != nil {
		return nil, err
	}
	return decode(r)
}
func (m Manager) List(ctx context.Context) ([]*Project, error) {
	rs, err := m.Store.ListSettings(ctx, prefix)
	if err != nil {
		return nil, err
	}
	ps := []*Project{}
	for _, r := range rs {
		p, err := decode(r)
		if err != nil {
			return nil, err
		}
		ps = append(ps, p)
	}
	return ps, nil
}

// Verify rechecks the registered path and the immutable reviewed commit. Dirty
// files in the source checkout are deliberately not copied into the worktree.
func Verify(ctx context.Context, p *Project, commit string) error {
	if !commitPattern.MatchString(commit) {
		return fmt.Errorf("%w: select a full commit identifier", ErrInvalid)
	}
	path, _, err := Inspect(ctx, p.Path)
	if err != nil {
		return err
	}
	if path != p.Path {
		return fmt.Errorf("%w: repository path changed", ErrInvalid)
	}
	resolved, err := git(ctx, path, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		return err
	}
	if resolved != commit {
		return ErrInvalid
	}
	return nil
}
func CreateWorktree(ctx context.Context, p *Project, commit, root, taskGraphID string) (string, error) {
	if err := Verify(ctx, p, commit); err != nil {
		return "", err
	}
	if !regexp.MustCompile(`^task-run-[a-z0-9]+$`).MatchString(taskGraphID) {
		return "", ErrInvalid
	}
	dir := filepath.Join(root, taskGraphID)
	if _, err := os.Lstat(dir); err == nil {
		return "", fmt.Errorf("%w: task workspace already exists; no automatic replacement", storage.ErrConflict)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	branch := "weft/" + taskGraphID
	_, err := gitCommand(ctx, p.Path, p.ImportID != "", "worktree", "add", "-b", branch, "--", dir, commit)
	if err != nil {
		return "", err
	}
	return dir, nil
}

func ValidExecutionPreference(mode string) bool {
	return mode == "workflow" || mode == "local" || mode == "sandbox"
}

// Preferences are suggestions for future drafts, never execution authority.
func (m Manager) SetExecutionPreference(ctx context.Context, id, mode string, revision int64, actor string) (*Project, error) {
	if !ValidExecutionPreference(mode) {
		return nil, fmt.Errorf("%w: invalid execution preference", ErrInvalid)
	}
	p, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if revision < 1 || p.Revision != revision {
		return nil, storage.ErrConflict
	}
	p.ExecutionMode = mode
	if err = m.Store.AppendEvent(ctx, domain.NewEvent("project.execution_preference_requested", "", map[string]any{"projectId": id, "mode": mode}).WithActor(actor)); err != nil {
		return nil, err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	p.Revision, err = m.Store.PutSetting(ctx, prefix+id, b, revision, actor)
	return p, err
}

// SetChecks replaces the project's declared checks. Checks name commands
// that still run under each agent's shell.exec grant; declaring one grants
// nothing.
func (m Manager) SetChecks(ctx context.Context, id string, checks []domain.Check, revision int64, actor string) (*Project, error) {
	if err := domain.ValidateChecks(checks); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	p, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if revision < 1 || p.Revision != revision {
		return nil, storage.ErrConflict
	}
	p.Checks = checks
	names := make([]string, 0, len(checks))
	for _, c := range checks {
		names = append(names, c.Name)
	}
	if err = m.Store.AppendEvent(ctx, domain.NewEvent("project.checks_requested", "", map[string]any{"projectId": id, "checks": names}).WithActor(actor)); err != nil {
		return nil, err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	p.Revision, err = m.Store.PutSetting(ctx, prefix+id, b, revision, actor)
	return p, err
}
