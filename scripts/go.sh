#!/usr/bin/env sh
# Run the Go toolchain inside a container so contributors only need Docker.
# Module and build caches live in named volumes so repeated runs are fast.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
GO_IMAGE=${GO_IMAGE:-golang:1.26-alpine}
if [ "${1:-}" = test ]; then
  # Real workspace integration tests need Git. Build a cached toolchain image;
  # never install it on the host or silently skip these tests.
  GO_IMAGE=$(docker build -q --build-arg "GO_IMAGE=$GO_IMAGE" -f "$ROOT/scripts/Dockerfile.go-test" "$ROOT/scripts")
fi
exec docker run --rm \
  -v "$ROOT:/repo" \
  -v weft-gomod:/go/pkg/mod \
  -v weft-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 -e GOFLAGS="${GOFLAGS:-}" \
  -e WEFT_TEST_OLLAMA_URL -e WEFT_TEST_OLLAMA_MODEL \
  -e WEFT_EVAL_OLLAMA_URL -e WEFT_EVAL_MODELS -e WEFT_EVAL_REPORT -e WEFT_EVAL_GUIDANCE -e WEFT_EVAL_WORKFLOW -e WEFT_EVAL_REPEAT \
  -e WEFT_EVAL_OPENAI_URL -e WEFT_EVAL_OPENAI_KEY -e WEFT_TEST_OPENAI_URL -e WEFT_TEST_OPENAI_MODEL -e WEFT_TEST_OPENAI_KEY \
  -e WEFT_TEST_GEMINI_KEY -e WEFT_TEST_GEMINI_MODEL \
  -w /repo/server "$GO_IMAGE" go "$@"
