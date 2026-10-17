#!/usr/bin/env bash
set -euo pipefail

readonly mode="${1:-}"

terraform init \
  -input=false \
  -backend-config=backend.defaults.hcl

case "$mode" in
  preview)
    terraform plan \
      -input=false \
      -lock-timeout=5m \
      -out=/slipramp/artifacts/plan.tfplan
    ;;
  apply)
    terraform apply \
      -input=false \
      -lock-timeout=5m \
      /slipramp/inputs/plan.tfplan
    ;;
  sync)
    terraform apply \
      -input=false \
      -auto-approve \
      -lock-timeout=5m
    ;;
  *)
    echo "usage: $0 preview|apply|sync" >&2
    exit 64
    ;;
esac
