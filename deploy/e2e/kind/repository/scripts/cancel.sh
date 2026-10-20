set -eu

trap 'echo "acceptance cancellation observed"; exit 0' TERM INT
echo "acceptance cancellation started"
while true; do
  sleep 1
done
