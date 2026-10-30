#!/usr/bin/env bash

set -euo pipefail

repository_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
helm_bin=${SLIPRAMP_HELM_BIN:-helm}
server_chart="$repository_root/deploy/charts/slipramp"
controller_chart="$repository_root/deploy/charts/slipramp-controller"
testdata="$repository_root/deploy/charts/testdata"
test_directory=$(mktemp -d "${TMPDIR:-/tmp}/slipramp-chart-tests.XXXXXX")

cleanup() {
  rm -rf "$test_directory"
}
trap cleanup EXIT

fail() {
  printf 'chart check failed: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local file=$1
  local pattern=$2
  grep -Eq -- "$pattern" "$file" || fail "$file does not contain $pattern"
}

assert_not_contains() {
  local file=$1
  local pattern=$2
  if grep -Eq -- "$pattern" "$file"; then
    fail "$file unexpectedly contains $pattern"
  fi
}

assert_render_fails() {
  local description=$1
  shift
  if "$helm_bin" template --kube-version 1.30.0 "$@" >"$test_directory/invalid.out" 2>&1; then
    fail "$description rendered successfully"
  fi
}

"$helm_bin" lint "$server_chart" --values "$testdata/slipramp-required.yaml"
"$helm_bin" lint "$controller_chart" --values "$testdata/controller-required.yaml"

chart_version=$(
  "$helm_bin" show chart "$server_chart" |
    awk '$1 == "version:" { gsub(/^"|"$/, "", $2); print $2; exit }'
)
package_directory="$test_directory/packages"
SLIPRAMP_HELM_BIN="$helm_bin" \
  "$repository_root/scripts/package-charts.sh" "$chart_version" "$package_directory"
test -f "$package_directory/slipramp-$chart_version.tgz" || fail "Server chart was not packaged"
test -f "$package_directory/slipramp-controller-$chart_version.tgz" || fail "Controller chart was not packaged"
if SLIPRAMP_HELM_BIN="$helm_bin" \
  "$repository_root/scripts/package-charts.sh" 999.0.0 "$test_directory/invalid-packages" \
  >"$test_directory/invalid-package.out" 2>&1; then
  fail "Mismatched release version packaged successfully"
fi

server_render="$test_directory/server.yaml"
server_repeat="$test_directory/server-repeat.yaml"
"$helm_bin" template --kube-version 1.30.0 slipramp "$server_chart" \
  --namespace slipramp-system \
  --values "$testdata/slipramp-required.yaml" >"$server_render"
"$helm_bin" template --kube-version 1.30.0 slipramp "$server_chart" \
  --namespace slipramp-system \
  --values "$testdata/slipramp-required.yaml" >"$server_repeat"
cmp -s "$server_render" "$server_repeat" || fail "Server chart rendering is not deterministic"

assert_not_contains "$server_render" '^kind: Secret$'
assert_contains "$server_render" 'name: slipramp-server-secrets'
assert_contains "$server_render" 'automountServiceAccountToken: false'
assert_contains "$server_render" 'path: /healthz'
assert_contains "$server_render" 'path: /readyz'
assert_contains "$server_render" 'timeoutSeconds: 3'
assert_not_contains "$server_render" '^kind: (ClusterRole|Role|RoleBinding|ServiceAccount)$'

"$helm_bin" template --kube-version 1.30.0 slipramp "$server_chart" \
  --namespace slipramp-system \
  --values "$testdata/slipramp-required.yaml" \
  --values "$testdata/slipramp-metrics.yaml" \
  >"$test_directory/server-metrics.yaml"
assert_contains "$test_directory/server-metrics.yaml" 'prometheus.io/scrape: "true"'
assert_contains "$test_directory/server-metrics.yaml" 'prometheus.io/path: /metrics'
assert_contains "$test_directory/server-metrics.yaml" 'prometheus.io/port: "9091"'
assert_contains "$test_directory/server-metrics.yaml" 'name: metrics'
assert_contains "$test_directory/server-metrics.yaml" 'port: 9091'
assert_contains "$test_directory/server-metrics.yaml" 'name: SLIPRAMP_SERVER_METRICS_ADDRESS'
assert_contains "$test_directory/server-metrics.yaml" 'value: ":9091"'

checksum_default=$(grep -E 'checksum/placements:' "$server_render" | awk '{print $2}')
"$helm_bin" template --kube-version 1.30.0 slipramp "$server_chart" \
  --namespace slipramp-system \
  --values "$testdata/slipramp-required.yaml" \
  --set-string server.placements.primary.jobNamespace=other-runs \
  >"$test_directory/server-placement.yaml"
checksum_changed=$(grep -E 'checksum/placements:' "$test_directory/server-placement.yaml" | awk '{print $2}')
test "$checksum_default" != "$checksum_changed" || fail "Placement change did not change the rollout checksum"

"$helm_bin" template --kube-version 1.30.0 slipramp "$server_chart" \
  --namespace slipramp-system \
  --values "$testdata/slipramp-required.yaml" \
  --values "$testdata/slipramp-scheduling.yaml" \
  >"$test_directory/server-scheduling.yaml"
assert_contains "$test_directory/server-scheduling.yaml" '^kind: Ingress$'
assert_contains "$test_directory/server-scheduling.yaml" 'ingressClassName: nginx'
assert_contains "$test_directory/server-scheduling.yaml" 'topologySpreadConstraints:'
assert_contains "$test_directory/server-scheduling.yaml" 'name: registry-credentials'
assert_contains "$test_directory/server-scheduling.yaml" 'kubernetes.io/os: linux'
assert_not_contains "$test_directory/server-scheduling.yaml" 'path: /metrics'

"$helm_bin" template --kube-version 1.30.0 slipramp "$server_chart" \
  --namespace slipramp-system \
  --values "$testdata/slipramp-required.yaml" \
  --set-string server.artifactTokenSecret.value=acceptance-artifact-token-secret-32 \
  --set-string server.artifactTokenSecret.existingSecret.name= \
  --set-string server.artifactTokenSecret.existingSecret.key= \
  >"$test_directory/server-managed-secret.yaml"
assert_contains "$test_directory/server-managed-secret.yaml" '^kind: Secret$'
assert_contains "$test_directory/server-managed-secret.yaml" 'acceptance-artifact-token-secret-32'

"$helm_bin" template --kube-version 1.30.0 slipramp "$server_chart" \
  --namespace slipramp-system \
  --values "$testdata/slipramp-required.yaml" \
  --values "$testdata/slipramp-notifications.yaml" \
  >"$test_directory/server-notifications.yaml"
assert_contains "$test_directory/server-notifications.yaml" 'SLIPRAMP_NOTIFICATIONS_PUBLIC_URL'
assert_contains "$test_directory/server-notifications.yaml" '/var/run/slipramp/notification-secrets/operations/webhook-url'
assert_contains "$test_directory/server-notifications.yaml" 'secretName: slipramp-slack'
assert_contains "$test_directory/server-notifications.yaml" 'key: webhook-url'
assert_not_contains "$test_directory/server-notifications.yaml" 'hooks.slack.com'

controller_render="$test_directory/controller.yaml"
controller_repeat="$test_directory/controller-repeat.yaml"
"$helm_bin" template --kube-version 1.30.0 slipramp-controller "$controller_chart" \
  --namespace slipramp-system \
  --values "$testdata/controller-required.yaml" \
  --values "$testdata/controller-multiple-namespaces.yaml" \
  >"$controller_render"
"$helm_bin" template --kube-version 1.30.0 slipramp-controller "$controller_chart" \
  --namespace slipramp-system \
  --values "$testdata/controller-required.yaml" \
  --values "$testdata/controller-multiple-namespaces.yaml" \
  >"$controller_repeat"
cmp -s "$controller_render" "$controller_repeat" || fail "Controller chart rendering is not deterministic"

test "$(grep -Ec '^kind: Role$' "$controller_render")" = 2 || fail "Controller chart did not create one Role per execution namespace"
test "$(grep -Ec '^kind: RoleBinding$' "$controller_render")" = 2 || fail "Controller chart did not create one RoleBinding per execution namespace"
assert_not_contains "$controller_render" '^kind: ClusterRole'
assert_not_contains "$controller_render" '^kind: Secret$'
assert_contains "$controller_render" '^  replicas: 1$'
assert_contains "$controller_render" 'namespace: runs-blue'
assert_contains "$controller_render" 'namespace: runs-green'
assert_contains "$controller_render" 'value: "\[\\"runs-blue\\",\\"runs-green\\"\]"'
assert_contains "$controller_render" 'name: slipramp-controller-credentials'
assert_contains "$controller_render" 'github-readonly'
assert_contains "$controller_render" 'name: SLIPRAMP_CONTROLLER_METRICS_ADDRESS'
assert_contains "$controller_render" 'value: ":9091"'
assert_contains "$controller_render" 'name: metrics'
assert_contains "$controller_render" 'port: 9091'

"$helm_bin" template --kube-version 1.30.0 slipramp-controller "$controller_chart" \
  --namespace slipramp-system \
  --values "$testdata/controller-required.yaml" \
  --values "$testdata/controller-metrics.yaml" \
  >"$test_directory/controller-metrics.yaml"
assert_contains "$test_directory/controller-metrics.yaml" 'prometheus.io/scrape: "true"'
assert_contains "$test_directory/controller-metrics.yaml" 'prometheus.io/path: /metrics'
assert_contains "$test_directory/controller-metrics.yaml" 'prometheus.io/port: "9091"'

kargo_controller_render="$test_directory/controller-kargo.yaml"
"$helm_bin" template --kube-version 1.30.0 slipramp-controller "$controller_chart" \
  --namespace slipramp-system \
  --values "$testdata/controller-required.yaml" \
  --set-json 'kargo.integrations=[{"name":"prod","stageNamespaces":["kargo-core","kargo-platform"]},{"name":"edge","stageNamespaces":["kargo-core"]}]' \
  >"$kargo_controller_render"
test "$(grep -Ec '^kind: Role$' "$kargo_controller_render")" = 3 || fail "Controller chart did not create one execution Role and one Role per distinct Kargo namespace"
test "$(grep -Ec '^kind: RoleBinding$' "$kargo_controller_render")" = 3 || fail "Controller chart did not bind every namespaced Controller Role"
assert_contains "$kargo_controller_render" 'namespace: kargo-core'
assert_contains "$kargo_controller_render" 'namespace: kargo-platform'
assert_contains "$kargo_controller_render" 'apiGroups: \[kargo.akuity.io\]'
assert_contains "$kargo_controller_render" 'resources: \[stages\]'
assert_contains "$kargo_controller_render" 'verbs: \[get, list, promote\]'
assert_contains "$kargo_controller_render" 'resources: \[freights\]'
assert_contains "$kargo_controller_render" 'resources: \[promotions\]'
assert_contains "$kargo_controller_render" 'verbs: \[create, get, list\]'
assert_not_contains "$kargo_controller_render" '^kind: ClusterRole'

assert_render_fails "Missing required Server configuration" \
  invalid "$server_chart"
assert_render_fails "Server replica count greater than one" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" --set server.replicaCount=2
assert_render_fails "Server latest image" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" --set-string server.image.tag=latest
assert_render_fails "Server readiness timeout shorter than the application deadline" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" --set server.readinessProbe.timeoutSeconds=2
assert_render_fails "Missing Server database credential" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" \
  --set-string server.database.url.existingSecret.name= \
  --set-string server.database.url.existingSecret.key=
assert_render_fails "Ambiguous Server database credential" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" --set-string server.database.url.value=postgres-secret
assert_render_fails "Kargo integration without environment order" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" \
  --set-string server.kargo.integrations.prod.cluster=primary \
  --set-json 'server.kargo.integrations.prod.repositories=["ops"]' \
  --set-string server.kargo.integrations.prod.preparation.repository=ops \
  --set-string server.kargo.integrations.prod.preparation.target=kargo-promotions \
  --set-string server.kargo.integrations.prod.preparation.action=prepare-review \
  --set-string server.kargo.integrations.prod.maximumStageObservationAge=5m \
  --set-string server.kargo.integrations.prod.pendingReviewRetention=192h
assert_render_fails "Notification destination without public URL" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" \
  --set-string server.notifications.destinations.operations.type=slack \
  --set-string server.notifications.destinations.operations.secretName=slipramp-slack \
  --set-string server.notifications.destinations.operations.secretKey=webhook-url \
  --set-json 'server.notifications.routes=[{"events":["review.requested"],"destinations":["operations"]}]'
assert_render_fails "Notification route with unknown destination" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" \
  --values "$testdata/slipramp-notifications.yaml" \
  --set-json 'server.notifications.routes=[{"events":["review.requested"],"destinations":["unknown"]}]'
assert_render_fails "Notification route with unknown event" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" \
  --values "$testdata/slipramp-notifications.yaml" \
  --set-json 'server.notifications.routes=[{"events":["run.failed"],"destinations":["operations"]}]'
assert_render_fails "Missing required Controller configuration" \
  invalid "$controller_chart"
assert_render_fails "Controller replica count greater than one" \
  invalid "$controller_chart" --values "$testdata/controller-required.yaml" --set replicaCount=2
assert_render_fails "Missing Controller credential" \
  invalid "$controller_chart" --values "$testdata/controller-required.yaml" \
  --set-string credentials.bearerToken.existingSecret.name= \
  --set-string credentials.bearerToken.existingSecret.key=
assert_render_fails "Controller without an execution namespace" \
  invalid "$controller_chart" --values "$testdata/controller-required.yaml" --set-json executionNamespaces='[]'
assert_render_fails "Controller latest Harness image" \
  invalid "$controller_chart" --values "$testdata/controller-required.yaml" --set-string harnessImage.tag=latest

printf 'Helm chart checks passed\n'
