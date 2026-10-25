#!/bin/sh

set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
generated=$(mktemp "${TMPDIR:-/tmp}/slipramp-catalog-schema.XXXXXX")

cleanup() {
  rm -f "$generated"
}

trap cleanup EXIT HUP INT TERM

cd "$repository_root"
go run ./internal/catalogschema/generate "$generated"
diff -u schemas/catalog-v1alpha1.json "$generated"
