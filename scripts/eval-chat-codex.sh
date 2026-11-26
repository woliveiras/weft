#!/usr/bin/env sh
# Build with the repo toolchain and run on the host so Codex owns its login.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
case_name=${CHAT_EVAL_CASE:-}
case "$case_name" in *[!a-zA-Z0-9]*) echo "Use an exact Chat eval case name." >&2; exit 2;; esac
command -v codex >/dev/null || { echo "Install Codex CLI and sign in with ChatGPT." >&2; exit 1; }
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
case $(uname -s) in Darwin) target_os=darwin;; Linux) target_os=linux;; *) echo "Use macOS or Linux to run the local CLI eval." >&2; exit 2;; esac
case $(uname -m) in arm64|aarch64) target_arch=arm64;; x86_64) target_arch=amd64;; *) echo "Unsupported host architecture" >&2; exit 2;; esac
binary="$scratch/chat-eval.test"
cd "$root/server"
if command -v go >/dev/null; then
  CGO_ENABLED=0 go test -c -o "$binary" ./internal/api
else
  docker run --rm -v "$root:/repo:ro" -v weft-gomod:/go/pkg/mod -v weft-gocache:/root/.cache/go-build \
    -w /repo/server -e CGO_ENABLED=0 -e GOOS="$target_os" -e GOARCH="$target_arch" golang:1.26-alpine \
    sh -ec 'go test -c -o /tmp/chat-eval.test ./internal/api >&2; cat /tmp/chat-eval.test' > "$binary"
  chmod 700 "$binary"
fi
filter='^TestChatEval'
if [ -n "$case_name" ]; then
  filter="^TestChatEval${case_name}$"
  [ "$("$binary" -test.list "$filter")" = "TestChatEval$case_name" ] || { echo "Unknown Chat eval: $case_name" >&2; exit 2; }
fi
"$binary" -test.v -test.count=1 -test.run "$filter" -chat-eval-codex -chat-eval-model "${CHAT_EVAL_MODEL:-}"
