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

chart_value() {
  local chart=$1
  local key=$2

  "$helm_bin" show chart "$chart" | awk -v key="$key" '
    $1 == key ":" {
      gsub(/^"|"$/, "", $2)
      print $2
      exit
    }
  '
}

charts=(
  "$repository_root/deploy/charts/slipramp"
  "$repository_root/deploy/charts/slipramp-controller"
)

for chart in "${charts[@]}"; do
  chart_name=$(chart_value "$chart" name)
  chart_version=$(chart_value "$chart" version)
  app_version=$(chart_value "$chart" appVersion)

  if [[ "$chart_version" != "$release_version" ]]; then
    printf 'chart packaging failed: %s version is %s, expected %s\n' \
      "$chart_name" "$chart_version" "$release_version" >&2
    exit 1
  fi
  if [[ "$app_version" != "$release_version" ]]; then
    printf 'chart packaging failed: %s appVersion is %s, expected %s\n' \
      "$chart_name" "$app_version" "$release_version" >&2
    exit 1
  fi

  while IFS= read -r image_tag; do
    if [[ "$image_tag" != "$release_version" ]]; then
      printf 'chart packaging failed: %s defaults image tag %s, expected %s\n' \
        "$chart_name" "$image_tag" "$release_version" >&2
      exit 1
    fi
  done < <(
    awk '$1 == "tag:" { gsub(/^"|"$/, "", $2); print $2 }' "$chart/values.yaml"
  )
done

mkdir -p "$output_directory"
for chart in "${charts[@]}"; do
  "$helm_bin" package "$chart" --destination "$output_directory"
done
