//go:build e2e

package main

// The fixture only models Docker's observable lifecycle. It never executes
// repository commands. Its state lives beside the script in the temporary root.
const dockerFixture = `#!/bin/sh
set -eu
root=$(dirname "$0")
for arg in "$@"; do last="$arg"; done
case "$1" in
 version) echo linux/arm64;;
 build|buildx)
  if [ "$1" = build ] || [ "$2" = build ]; then
   data=$(cat)
   case "$data" in *E2E_BUILD_FAIL*) echo 'E2E controlled build failure' >&2; exit 1;; esac
   case "$data" in *E2E_FAIL_ONCE*)
    if [ ! -f "$root/failed-once" ]; then
     touch "$root/failed-once"; echo 'E2E transient build failure' >&2; exit 1
    fi;;
   esac
  fi;;
 run)
  name=""
  while [ "$#" -gt 0 ]; do
   if [ "$1" = --name ]; then shift; name="$1"; break; fi
   shift
  done
  if [ -n "$name" ]; then echo true > "$root/container-$name"; fi;;
 inspect)
  if [ ! -f "$root/container-$last" ]; then echo 'No such container' >&2; exit 1; fi
  case "$*" in
   *State.Running*) cat "$root/container-$last";;
  esac;;
 stop) echo false > "$root/container-$last";;
 start)
  if [ ! -f "$root/container-$last" ]; then echo 'No such container' >&2; exit 1; fi
  echo true > "$root/container-$last";;
 rm) rm -f "$root/container-$last";;
 exec) echo e2e-executed;;
esac
`
