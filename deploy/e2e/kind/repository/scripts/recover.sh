set -eu

wait_for_release=${ACCEPTANCE_WAIT_FOR_RELEASE:-true}
if [ "$wait_for_release" = true ]; then
  mkfifo /tmp/acceptance-recovery
fi
echo "acceptance recovery started"
if [ "$wait_for_release" = true ]; then
  read -r release < /tmp/acceptance-recovery
  test "$release" = continue
fi
echo "acceptance recovery completed"
