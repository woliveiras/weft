#!/usr/bin/env sh
# Disposable native installation only; never uses the default user data directory.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WEFT_NATIVE_E2E_DATA=$(mktemp -d "${TMPDIR:-/tmp}/weft-native-e2e.XXXXXX")
export WEFT_NATIVE_E2E_DATA
trap 'rm -rf "$WEFT_NATIVE_E2E_DATA"' EXIT HUP INT TERM
cd "$ROOT/web"
pnpm exec tsc --project tsconfig.e2e.json
pnpm exec playwright test --config playwright.native.config.ts
