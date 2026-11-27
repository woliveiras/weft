#!/bin/sh
# Run against a clean checkout so local binaries cannot enter the build context.
set -eu
cd "$(dirname "$0")/.."

check_image() {
  label=$1
  shift
  image=$(docker build -q "$@")
  docker run --rm --network none --read-only --user 0 --entrypoint /bin/sh "$image" -ec '
    for exe in python python2 python3 pip pip2 pip3; do
      if command -v "$exe"; then exit 1; fi
    done
    # Also check versioned executables and packages outside PATH. Inspect the
    # image root filesystem only, excluding procfs and other runtime mounts.
    matches=$(find / -xdev \( -type f -o -type l \) \( \
      -name python -o -name python2 -o -name python3 -o -name "python[23].[0-9]*" \
      -o -name pip -o -name pip2 -o -name pip3 -o -name "pip[23].[0-9]*" \
      -o -name "libpython*.so*" \) -print)
    packages=$(find / -xdev -type d \( -name langgraph -o -name "langgraph-*.dist-info" -o -name "pip-*.dist-info" \) -print)
    if [ -n "$matches$packages" ]; then
      printf "%s\n%s\n" "$matches" "$packages" >&2
      exit 1
    fi
  '
  printf '%s passed (%s)\n' "$label" "$image"
}

check_image go-test -f scripts/Dockerfile.go-test scripts
check_image web-test --target web-source .
check_image web-build --target web .
check_image controller-build --target build .
check_image controller .
check_image agent-build --target build agents/eino
check_image agent agents/eino
check_image default-sandbox -f Dockerfile.sandbox .
