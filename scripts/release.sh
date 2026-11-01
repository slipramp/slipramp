#!/usr/bin/env bash

set -euo pipefail

repository_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
release_version=${1:?usage: release.sh VERSION}
release_tag="v$release_version"

fail() {
  printf 'release failed: %s\n' "$*" >&2
  exit 1
}

if [[ ! "$release_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
  fail "$release_version is not a supported release version"
fi

cd "$repository_root"

test "$(git branch --show-current)" = main || fail "releases must be cut from main"
test -z "$(git status --porcelain)" || fail "working tree must be clean"

git fetch --quiet origin main
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" || \
  fail "local main must match origin/main"

if git rev-parse --quiet --verify "refs/tags/$release_tag" >/dev/null; then
  fail "$release_tag already exists locally"
fi
if test -n "$(git ls-remote --tags origin "refs/tags/$release_tag")"; then
  fail "$release_tag already exists on origin"
fi

task charts:package CHART_VERSION="$release_version"
git tag -a "$release_tag" -m "$release_tag"
git push origin "$release_tag"
