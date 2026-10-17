#!/usr/bin/env bash
set -euo pipefail

readonly mode="${1:-}"
readonly limit="${2:-}"

case "$mode" in
  preview | apply) ;;
  *)
    echo "usage: $0 preview|apply exporter_nodes|flows1.den1.kcloud.zone" >&2
    exit 64
    ;;
esac

case "$limit" in
  exporter_nodes | flows1.den1.kcloud.zone) ;;
  *)
    echo "unsupported node_exporter limit: $limit" >&2
    exit 64
    ;;
esac

readonly ssh_private_key=/run/slipramp/credentials/ssh/id_ed25519
if [[ ! -r "$ssh_private_key" ]] || [[ ! -s "$ssh_private_key" ]]; then
  echo "SSH private key is required at $ssh_private_key" >&2
  exit 64
fi

readonly ssh_directory="${HOME:?HOME is required}/.ssh"
readonly known_hosts_path="$ssh_directory/known_hosts"

umask 077
install -d -m 0700 "$ssh_directory"
touch "$known_hosts_path"
chmod 0600 "$known_hosts_path"

arguments=(
  inventory.py
  node_exporter/deploy.py
  --limit "$limit"
  --ssh-key "$ssh_private_key"
)

case "$mode" in
  preview)
    arguments+=(--dry)
    ;;
  apply)
    arguments+=(-y)
    ;;
esac

pyinfra "${arguments[@]}"
