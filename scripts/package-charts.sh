#!/usr/bin/env bash

set -euo pipefail

repository_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
helm_bin=${SLIPRAMP_HELM_BIN:-helm}
release_version=${1:?usage: package-charts.sh VERSION OUTPUT_DIRECTORY}
output_directory=${2:?usage: package-charts.sh VERSION OUTPUT_DIRECTORY}

if [[ ! "$release_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
  printf 'chart packaging failed: %s is not a supported release version\n' "$release_version" >&2
  exit 1
fi

charts=(
  "$repository_root/deploy/charts/slipramp"
  "$repository_root/deploy/charts/slipramp-controller"
)

mkdir -p "$output_directory"
for chart in "${charts[@]}"; do
  "$helm_bin" package "$chart" \
    --version "$release_version" \
    --app-version "$release_version" \
    --destination "$output_directory"
done
