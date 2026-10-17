#!/bin/sh

set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
snapshot=$(mktemp -d "${TMPDIR:-/tmp}/slipramp-sqlc.XXXXXX")

cleanup() {
  rm -rf "$snapshot"
}

trap cleanup EXIT HUP INT TERM

mkdir -p "$snapshot/internal/store"
cp -R "$repository_root/internal/store/storedb" "$snapshot/internal/store/storedb"

task --dir "$repository_root" store:generate

diff -ru \
  "$snapshot/internal/store/storedb" \
  "$repository_root/internal/store/storedb"
