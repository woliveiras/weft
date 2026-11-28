#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
trap 'exit 1' HUP INT TERM

cd "$tmp"
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
zero=0000000000000000000000000000000000000000
printf 'refs/tags/v1 %s refs/tags/v1 %s\nrefs/heads/other %s refs/heads/other %s\n(delete) %s refs/heads/main %s\n' \
  "$zero" "$zero" "$zero" "$zero" "$zero" "$zero" |
  "$root/.githooks/pre-push" origin fixture

git init -q --template= --initial-branch=main fixture
cd fixture
git config user.name 'Weft fixture'
git config user.email 'weft-fixture@example.invalid'
cat > Makefile <<'EOF'
check:
	test "$$(cat result)" = pass
	test ! -e untracked
	test -z "$$GIT_DIR"
EOF
printf 'pass\n' > result
git add Makefile result
git commit -qm 'Passing commit'
passing=$(git rev-parse HEAD)
printf 'fail\n' > result
git add result
git commit -qm 'Failing commit'
failing=$(git rev-parse HEAD)
printf 'dirty\n' > result
touch untracked

run_hook() {
  printf 'refs/heads/main %s refs/heads/main %s\n' "$1" "$zero" > "$tmp/refs"
  GIT_DIR="$PWD/.git" git -c core.hooksPath="$root/.githooks" hook run --to-stdin="$tmp/refs" pre-push -- origin fixture
}

run_hook "$passing"
if run_hook "$failing" > "$tmp/failure" 2>&1; then
  echo 'The hook accepted a failing commit' >&2
  exit 1
fi
grep -Fq "pre-push: make check failed for $failing" "$tmp/failure"
test "$(git rev-parse HEAD)" = "$failing"
test "$(cat result)" = dirty
test -e untracked
test -z "$(find .git -maxdepth 1 -name 'weft-pre-push.*' -print -quit)"
