#!/usr/bin/env sh
# Build a host-native bundle; never starts or changes an installation.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
command -v go >/dev/null || { echo "Install the Go version specified in server/go.mod to build from source." >&2; exit 1; }
command -v pnpm >/dev/null || { echo "Install pnpm to build the bundled UI." >&2; exit 1; }
PLATFORM=$(go env GOOS)-$(go env GOARCH)
case "$PLATFORM" in darwin-arm64|darwin-amd64|linux-arm64|linux-amd64) ;; *) echo "Unsupported initial native target: $PLATFORM" >&2; exit 1;; esac
OUT=${1:-"$ROOT/dist/native/$PLATFORM-$(date +%Y%m%d-%H%M%S)"}
if [ -e "$OUT" ]; then echo "Output already exists; choose a new bundle directory." >&2; exit 1; fi
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
(cd "$ROOT/web" && CI=true pnpm install --frozen-lockfile && pnpm run build)
(cd "$ROOT/server" && go build -o "$OUT/weftd" ./cmd/weftd && go build -o "$OUT/weft" ./cmd/weft)
mkdir "$OUT/web"
cp -R "$ROOT/web/dist/." "$OUT/web/"
cp "$ROOT/docs/standalone.md" "$OUT/README.md"
printf 'Native bundle: %s\nInitialize a NEW profile: %s/weftd --init-local\nStart: %s/weftd --local\nStop: Ctrl-C (data is retained)\n' "$OUT" "$OUT" "$OUT"
