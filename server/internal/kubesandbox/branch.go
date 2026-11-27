package kubesandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/woliveiras/weft/server/internal/workspace"
)

// Git reads agent-controlled metadata only inside a workload pod. The
// controller carries an opaque, size-bounded Git bundle between two mounts.
const gitPreamble = `set -eu
test -n "$WEFT_POD_UID" && test "$WEFT_POD_UID" = "$1" || exit 70
shift
export LC_ALL=C GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_ALLOW_PROTOCOL=file
export GIT_NO_REPLACE_OBJECTS=1 GIT_NO_LAZY_FETCH=1 GIT_TERMINAL_PROMPT=0
git_safe() {
 git -c core.hooksPath=/dev/null -c core.fsmonitor=false -c gc.auto=0 -c transfer.fsckObjects=true "$@"
}
branch=$1
ref="refs/heads/$branch"
git_safe check-ref-format "$ref"
`

const exportBranch = gitPreamble + `
test -d .git && test ! -L .git || exit 74
if test -n "$2"; then
 test "$(git_safe symbolic-ref HEAD)" = "$ref" || exit 72
 test "$(git_safe rev-parse --verify "$ref^{commit}")" = "$2" || exit 72
elif git_safe show-ref --exists "$ref"; then
 :
else
 test "$?" = 2
 ref=HEAD
fi
git_safe bundle create - "$ref"
`

const receiveBundle = gitPreamble + `
bundle=$(mktemp /tmp/weft-branch.XXXXXXXX)
trap 'rm -f "$bundle"' EXIT
cat > "$bundle"
line=$(git_safe bundle list-heads "$bundle")
oid=${line%% *}
source_ref=${line#* }
test "$source_ref" = "$ref" || test "$source_ref" = HEAD
case "$oid" in *[!a-f0-9]*|'') exit 71;; esac
case ${#oid} in 40) format=sha1;; 64) format=sha256;; *) exit 71;; esac
`

const seedBranch = receiveBundle + `
git_safe init -q --template= --object-format="$format" --initial-branch="$branch" checkout
git_safe -C checkout fetch -q --no-tags --no-recurse-submodules "$bundle" "$source_ref"
git_safe -C checkout reset --hard -q "$oid"
git_safe -C checkout config user.name "Weft Agent"
git_safe -C checkout config user.email agent@weft.local
rm checkout/.git/FETCH_HEAD
`

// Import objects without a tracking branch or FETCH_HEAD. Updating the one
// server-derived ref uses Git's lock and compares against the reviewed tip.
const publishBranch = receiveBundle + `
test -d .git && test ! -L .git || exit 74
test "$source_ref" = "$ref" && test "$oid" = "$2" || exit 72
if git_safe symbolic-ref --quiet "$ref" >/dev/null; then exit 73; fi
if current=$(git_safe show-ref --verify --hash "$ref"); then
 test "$current" != "$oid" || exit 0
 test "$current" = "$3" || exit 73
else
 if git_safe show-ref --exists "$ref"; then exit 73; else test "$?" = 2; fi
 test -z "$3" || exit 73
fi
worktrees=$(git_safe worktree list --porcelain)
if printf '%s\n' "$worktrees" | grep -Fxq "branch $ref"; then exit 73; fi
git_safe fetch -q --no-prune --refmap= --no-tags --no-write-fetch-head --no-auto-maintenance --no-recurse-submodules "$bundle" "$source_ref"
if test -n "$3"; then
 if git_safe merge-base --is-ancestor "$3" "$oid"; then :; else
  code=$?
  test "$code" != 1 || exit 73
  exit "$code"
 fi
fi
if ! git_safe update-ref --no-deref -m "Weft replica publication" "$ref" "$oid" "$3"; then
 current=$(git_safe show-ref --verify --hash "$ref") || current=
 test "$current" != "$oid" || exit 0
 test "$current" = "$3" || exit 73
 exit 1
fi
`

const alreadyPublished = gitPreamble + `
test -d .git && test ! -L .git || exit 74
if git_safe symbolic-ref --quiet "$ref" >/dev/null; then exit 73; fi
test "$(git_safe show-ref --verify --hash "$ref")" = "$2" || exit 73
`

const maxBundleBytes = 64 << 20

type limitedBundle struct {
	file      *os.File
	remaining int64
	err       error
}

func (w *limitedBundle) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		w.err = errors.New("replica Git bundle exceeds 64 MiB")
		return 0, w.err
	}
	n, err := w.file.Write(data)
	w.remaining -= int64(n)
	if err != nil {
		w.err = err
	}
	return n, err
}

func TransferBranch(ctx context.Context, root, image string, request workspace.ReplicaBranchTransfer) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	file, err := os.CreateTemp("", "weft-replica-*.bundle")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	run := func(dir string, readonly bool, script string, in io.Reader, out io.Writer) (err error) {
		sandbox, err := New(Replica{Root: root, Workdir: dir, Image: image, Graph: request.Graph, Node: request.Node, Index: request.Replica, ReadOnly: readonly})
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, sandbox.Close()) }()
		if err = sandbox.ready(ctx, 5*time.Minute); err != nil {
			return err
		}
		argv := []string{"/bin/sh", "-ec", script, "weft-git", string(sandbox.pod.UID), request.Branch, request.Commit, request.Previous}
		return sandbox.stream(ctx, argv, in, out)
	}
	bundle := &limitedBundle{file: file, remaining: maxBundleBytes}
	err = run(request.Source, true, exportBranch, nil, bundle)
	if err = errors.Join(err, bundle.err); err != nil {
		var exit interface{ ExitStatus() int }
		if errors.As(err, &exit) && exit.ExitStatus() == 74 {
			return errors.New("source repository must have its Git metadata inside the mounted workspace")
		}
		if errors.As(err, &exit) && exit.ExitStatus() == 72 {
			// A previously successful request can be retried after the replica
			// has already started its next revision.
			check := run(request.Destination, true, alreadyPublished, nil, io.Discard)
			if check == nil {
				return nil
			}
			if !errors.As(check, &exit) || exit.ExitStatus() != 73 {
				return fmt.Errorf("verify prior publication: %w", check)
			}
			return fmt.Errorf("%w: source branch changed", workspace.ErrBranchConflict)
		}
		return fmt.Errorf("export replica branch: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	script := seedBranch
	if request.Commit != "" {
		script = publishBranch
	}
	if err := run(request.Destination, false, script, file, io.Discard); err != nil {
		var exit interface{ ExitStatus() int }
		if errors.As(err, &exit) && (exit.ExitStatus() == 72 || exit.ExitStatus() == 73) {
			return fmt.Errorf("%w: branch changed or checked out", workspace.ErrBranchConflict)
		}
		return fmt.Errorf("receive replica branch: %w", err)
	}
	return nil
}
