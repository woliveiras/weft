#!/bin/sh
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
arch=$(docker compose exec -T sandbox-daemon uname -m)
case "$arch" in aarch64) arch=arm64;; x86_64) arch=amd64;; *) echo 'unsupported daemon architecture' >&2; exit 1;; esac
binary=$(mktemp)
journal=$(mktemp)
trap 'rm -f "$binary" "$journal"' EXIT
(cd server && GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go test -c -o "$binary" ./internal/workspacejournal)
# /tmp is periodically reset by Docker's entrypoint; keep this test executable
# outside it. Tests create and clean only their own temporary fixture directory.
docker compose cp "$binary" sandbox-daemon:/weft-workspace-journal-test
docker compose exec -T -e WEFT_TEST_KERNEL_JOURNAL=1 -e WEFT_TEST_JOURNAL_EXPORT=/weft-workspace-journal-e2e.jsonl sandbox-daemon /weft-workspace-journal-test -test.v -test.run '^TestKernelJournal(Live|ShutdownFlush)$'

docker compose cp sandbox-daemon:/weft-workspace-journal-e2e.jsonl "$journal"
(cd web && pnpm exec tsc --project tsconfig.e2e.json && WEFT_WORKSPACE_JOURNAL="$journal" pnpm exec playwright test --config playwright.journal.config.ts)
