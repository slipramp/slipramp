#!/usr/bin/env bash

# Sourced by kind-acceptance.sh after its authenticated execution checks.

submit_kargo_promotion() {
  curl --fail-with-body --silent --show-error \
    -H 'Content-Type: application/json' \
    -H "Authorization: Bearer $kargo_api_key" \
    --data "$1" \
    "$server_url/api/v1/kargo/acceptance/promotions"
}

wait_for_pending_promotion() {
  local promotion_id=$1
  local deadline=$((SECONDS + timeout_seconds))
  local response status
  while ((SECONDS < deadline)); do
    if response=$(api_get "/api/v1/promotions/$promotion_id" 2>/dev/null); then
      status=$(jq -r '.status' <<<"$response")
      if [[ "$status" == pending ]]; then
        printf '%s\n' "$response"
        return 0
      fi
      if [[ "$status" != preparing ]]; then
        printf 'Promotion %s reached %s instead of pending\n' \
          "$promotion_id" "$status" >&2
        printf '%s\n' "$response" >&2
        return 1
      fi
    fi
    sleep 1
  done

  printf 'Promotion %s did not reach pending\n' "$promotion_id" >&2
  printf '%s\n' "${response:-no response}" >&2
  return 1
}

check_guarded_promotion() {
  local external_id=$1
  local candidate_revision=$2
  local candidate_branch=$3
  local body response promotion_id run_id git_id argo_id
  local expected_git="$temporary_directory/$external_id.expected.diff"
  local actual_git="$temporary_directory/$external_id.git.diff"
  local actual_argo="$temporary_directory/$external_id.argo.json"

  body=$(jq -cn \
    --arg externalId "$external_id" \
    --arg base "$fixture_commit" \
    --arg candidate "$candidate_revision" \
    --arg branch "$candidate_branch" '{
      externalPromotionId: $externalId,
      project: "kargo-acceptance",
      stage: "acceptance",
      baseRevision: $base,
      candidateRevision: $candidate,
      candidateBranch: $branch
    }')
  response=$(submit_kargo_promotion "$body")
  promotion_id=$(jq -er '.id' <<<"$response")
  jq -e '.reviewRequired and (.status == "preparing" or .status == "pending")' \
    <<<"$response" >/dev/null
  response=$(wait_for_pending_promotion "$promotion_id")
  run_id=$(jq -er '.preparationRunId' <<<"$response")
  git_id=$(jq -er '.gitDiffArtifactId' <<<"$response")
  argo_id=$(jq -er '.argoDiffArtifactId' <<<"$response")

  jq -e \
    --arg promotion "$promotion_id" \
    --arg run "$run_id" \
    --arg commit "$fixture_commit" \
    --arg candidate "$candidate_revision" \
    --arg catalog "$(jq -r '.id' <<<"$catalog_response")" \
    --arg principal "$kargo_principal_id" '
      .reviewRequired and .status == "pending" and
      .requestedBy.principalId == $principal and
      .candidateRevision == $candidate and
      .preparationRun.phase == "finished" and
      .preparationRun.outcome == "succeeded" and
      .preparationRun.commitSha == $commit and
      .preparationRun.catalogRevisionId == $catalog and
      .preparationRun.source.type == "promotion" and
      .preparationRun.source.promotion.promotionId == $promotion and
      .preparationRun.createdBy.principalId == $principal and
      all(.gitDiffArtifact, .argoDiffArtifact;
        .kind == "output" and .state == "sealed" and .sensitive and
        .retentionState == "retained" and
        .provenance.producerRunId == $run and
        .provenance.commitSha == $commit and
        .provenance.catalogRevisionId == $catalog) and
      .gitDiffArtifact.mediaType == "text/x-diff; charset=utf-8" and
      .argoDiffArtifact.mediaType == "application/vnd.slipramp.argocd-diff+json"
    ' <<<"$response" >/dev/null

  wait_for_job_deletion "$run_id"
  download_artifact "$git_id" "$actual_git"
  download_artifact "$argo_id" "$actual_argo"
  git -C "$fixture_directory" diff --no-ext-diff --no-color --find-renames \
    "$fixture_commit" "$candidate_revision" >"$expected_git"
  cmp "$expected_git" "$actual_git"
  jq -e '.resources == [{
    group: "apps", kind: "Deployment", namespace: "default", name: "acceptance",
    diff: "@@ -1 +1 @@\n-image: acceptance:old\n+image: acceptance:new"
  }]' "$actual_argo" >/dev/null
  if [[ "$candidate_revision" == "$fixture_commit" ]]; then
    test ! -s "$actual_git"
    jq -e '.gitDiffArtifact.sizeBytes == 0 and
      .gitDiffArtifact.sha256 == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"' \
      <<<"$response" >/dev/null
  else
    test -s "$actual_git"
  fi

  response=$(submit_kargo_promotion "$body")
  jq -e --arg id "$promotion_id" '.id == $id and .status == "pending"' \
    <<<"$response" >/dev/null
  api_get "/api/v1/runs?repositoryId=$repository_id&target=kargo-promotions" |
    jq -e --arg id "$promotion_id" --arg run "$run_id" '
      [.items[] | select(.source.promotion.promotionId == $id) | .id] == [$run]
    ' >/dev/null
  assert_log_contains "$run_id" 'prepared Git and Argo CD review artifacts'
  log "Promotion $external_id reached pending with retained diff artifacts"
}

log "configuring the Kargo acceptance service account and Stage snapshot"
kargo_principal_id=$(curl --fail-with-body --silent --show-error \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $product_api_key" \
  --data '{"displayName":"Kargo acceptance","role":"operator"}' \
  "$server_url/api/v1/service-accounts" | jq -er '.principalId')
kargo_api_key=$(curl --fail-with-body --silent --show-error \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $product_api_key" \
  --data '{"name":"Kargo acceptance","scopes":["slipramp:promotions:submit"]}' \
  "$server_url/api/v1/service-accounts/$kargo_principal_id/api-keys" | jq -er '.token')

kargo_snapshot=$(jq -cn \
  --arg observedAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg repositoryURL "$(jq -r '.cloneUrl' <<<"$repository_response")" '{
    snapshotId: "00000000-0000-4000-8000-000000000001",
    observedAt: $observedAt,
    stages: [{
      project: "kargo-acceptance", name: "acceptance",
      kubernetesUid: "acceptance-stage", resourceVersion: "1",
      application: "acceptance", environment: "acceptance",
      repositoryUrl: $repositoryURL, targetBranch: "stage/acceptance/acceptance",
      sourcePaths: ["review/manifest.yaml"], freight: {}
    }]
  }')
curl --fail-with-body --silent --show-error -X PUT \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer acceptance-controller-token' \
  --data "$kargo_snapshot" \
  "$server_url/controller/v1/kargo/acceptance/stages"
kargo_stage_id=$(api_get '/api/v1/promotion-stages?integration=acceptance' |
  jq -er '.items[] | select(.name == "acceptance") | .id')

log "testing automatic Promotion approval without a preparation Run"
automatic_request=$(jq -cn \
  --arg base "$fixture_commit" --arg candidate "$candidate_commit" '{
    externalPromotionId: "acceptance-automatic", project: "kargo-acceptance",
    stage: "acceptance", baseRevision: $base, candidateRevision: $candidate,
    candidateBranch: "kargo/promotion/acceptance"
  }')
automatic_response=$(submit_kargo_promotion "$automatic_request")
automatic_id=$(jq -er '.id' <<<"$automatic_response")
jq -e '.status == "approved" and .reviewRequired == false' \
  <<<"$automatic_response" >/dev/null
api_get "/api/v1/promotions/$automatic_id" |
  jq -e '.status == "approved" and (.preparationRunId == null)' >/dev/null
api_get "/api/v1/runs?repositoryId=$repository_id&target=kargo-promotions" |
  jq -e '.items == []' >/dev/null
automatic_retry=$(submit_kargo_promotion "$automatic_request")
jq -e --arg id "$automatic_id" '.id == $id and .status == "approved"' \
  <<<"$automatic_retry" >/dev/null

log "testing guarded Promotion execution and real artifact uploads"
curl --fail-with-body --silent --show-error -X PUT \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $product_api_key" \
  --data '{"mode":"review-required","reason":"Acceptance review preparation"}' \
  "$server_url/api/v1/promotion-stages/$kargo_stage_id/policy" |
  jq -e '.effectiveMode == "review-required"' >/dev/null
check_guarded_promotion acceptance-guarded "$candidate_commit" kargo/promotion/acceptance
check_guarded_promotion acceptance-empty "$fixture_commit" kargo/promotion/acceptance-empty

api_get "/api/v1/runs?repositoryId=$repository_id&target=kargo-promotions" |
  jq -e --arg automatic "$automatic_id" '
    (.items | length) == 2 and
    all(.items[]; .source.promotion.promotionId != $automatic)
  ' >/dev/null
