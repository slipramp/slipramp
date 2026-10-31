#!/bin/sh

set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
snapshot=$(mktemp -d "${TMPDIR:-/tmp}/slipramp-generated.XXXXXX")

cleanup() {
  rm -rf "$snapshot"
}

trap cleanup EXIT HUP INT TERM

mkdir -p "$snapshot/internal/api" "$snapshot/ios/Slipramp" "$snapshot/web/app/api"

cp "$repository_root/internal/api/generated.go" "$snapshot/internal/api/generated.go"
cp "$repository_root/ios/Slipramp/openapi.yaml" "$snapshot/ios/Slipramp/openapi.yaml"
cp -R \
  "$repository_root/ios/Slipramp/GeneratedSources" \
  "$snapshot/ios/Slipramp/GeneratedSources"
cp -R "$repository_root/web/app/api/generated" "$snapshot/web/app/api/generated"

task --dir "$repository_root" api:generate

diff -u \
  "$snapshot/internal/api/generated.go" \
  "$repository_root/internal/api/generated.go"
diff -u \
  "$snapshot/ios/Slipramp/openapi.yaml" \
  "$repository_root/ios/Slipramp/openapi.yaml"
diff -ru \
  "$snapshot/ios/Slipramp/GeneratedSources" \
  "$repository_root/ios/Slipramp/GeneratedSources"
diff -ru \
  "$snapshot/web/app/api/generated" \
  "$repository_root/web/app/api/generated"
