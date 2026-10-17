set -eu

repository="$1"
url="$2"
commit="$3"
credentials="$4"

mkdir -p /tmp/slipramp-home
export HOME=/tmp/slipramp-home
export GIT_TERMINAL_PROMPT=0

if [ -n "$credentials" ]; then
  if [ -f "$credentials/gitconfig" ]; then
    export GIT_CONFIG_GLOBAL="$credentials/gitconfig"
  fi

  if [ -f "$credentials/ssh-privatekey" ]; then
    ssh_command="ssh -i $credentials/ssh-privatekey -o BatchMode=yes -o IdentitiesOnly=yes"
    if [ -f "$credentials/known_hosts" ]; then
      ssh_command="$ssh_command -o UserKnownHostsFile=$credentials/known_hosts -o StrictHostKeyChecking=yes"
    fi
    export GIT_SSH_COMMAND="$ssh_command"
  fi
fi

git init --quiet "$repository"
git -C "$repository" remote add origin "$url"
git -C "$repository" fetch --quiet --no-tags --depth=1 origin "$commit"
git -C "$repository" checkout --quiet --detach FETCH_HEAD
test "$(git -C "$repository" rev-parse HEAD)" = "$commit"

# The checkout and action images need not use the same numeric user. The
# workspace is private to this execution Pod, so make the frozen checkout
# writable by the runtime without coupling portable images to cluster UIDs.
chmod -R a+rwX "$repository"
