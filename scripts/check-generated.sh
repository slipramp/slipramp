#!/bin/sh

set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
snapshot=$(mktemp -d "${TMPDIR:-/tmp}/slipramp-generated.XXXXXX")

cleanup() {
  rm -rf "$snapshot"
}

trap cleanup EXIT HUP INT TERM

mkdir -p "$snapshot/internal/api" "$snapshot/web/app/api"

cp "$repository_root/internal/api/generated.go" "$snapshot/internal/api/generated.go"
cp -R "$repository_root/web/app/api/generated" "$snapshot/web/app/api/generated"

task --dir "$repository_root" api:generate

diff -u \
  "$snapshot/internal/api/generated.go" \
  "$repository_root/internal/api/generated.go"
diff -ru \
  "$snapshot/web/app/api/generated" \
  "$repository_root/web/app/api/generated"
