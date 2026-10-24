# Operator-started Kargo Promotions

Status: implementation in progress

The first backend slice is implemented: the shared contract includes operator
creation and `creating` state, Stage observations persist requested-Freight
topology, and an authorized request atomically validates and snapshots exact
Freight into a durable Promotion. Controller dispatch, callback hydration, and
the two UI entry points remain unfinished.

## Outcome

Allow an operator with `promotions:write` to create a Kargo Promotion from
Slipramp in two places:

- On the promotion pipelines page, promote an exact Freight from an actual
  upstream Stage into the selected Stage.
- On the branch drift page, promote an exact Freight already present in the
  Stage back into that same Stage.

The operator may either open the Promotion immediately or start it in the
background. Both choices create the same durable `Promotion`; they differ only
in what the web application does after submission.

## Accepted design

- `Promotion` remains the only user-facing resource. Do not add a
  `PromotionStart`, `PromotionRequest`, or other parallel lifecycle.
- Add `creating` to the Promotion status values. It covers queued Controller
  delivery, retries, and waiting for the Kargo task to submit its review data.
- Use the existing `error` status for terminal creation failures.
- Keep dispatch fields on the Promotion record. A separate dispatch table and
  public dispatch API are unnecessary for the first implementation.
- The Slipramp Server never talks to Kubernetes or the Kargo API. The Controller
  creates the Kargo `Promotion` resource with its existing dynamic Kubernetes
  client.
- Each action creates one Kargo Promotion for one exact Freight and one target
  Stage.
- The configured Kargo integration and Stage namespaces are the opt-in. Do not
  add another feature flag initially.
- Kargo remains authoritative for Freight availability, soak requirements,
  verification, promotion windows, and Promotion admission.

## User flows

### Promotion pipelines

Add a Promote action to a destination Stage card. Its choices come from the
Stage's declared Kargo Freight sources, not from the configured display order of
environments.

For each declared upstream Stage, offer its current Freight items whose origin
matches the destination Stage's requested Freight origin. If there is one
choice, select it by default. If there are several, the dialog requires the
operator to choose one. Do not offer direct Warehouse Freight that has not been
observed on an upstream Stage in this first version.

### Branch drift

Add a Promote action to a drifted Stage row. Offer the Stage's current Freight
items and target the same Stage. If there is one current Freight item, select it
by default; otherwise require an explicit choice.

### Shared dialog

The dialog shows:

- target Stage and environment;
- source Stage;
- exact Freight name and origin;
- the Freight's Git, image, and chart versions; and
- a short warning for an in-place promotion that it reruns the Stage's Kargo
  promotion template with the same Freight.

It has two submit buttons:

- **Promote and view** creates the Promotion and redirects immediately to its
  Slipramp detail page.
- **Promote in background** creates the same Promotion, closes the dialog, and
  shows a toast linking to it.

The Promotion detail page exists as soon as the POST succeeds. While its status
is `creating`, the page revalidates periodically and explains whether Slipramp
is delivering the request or is waiting for Kargo to call back. The Kargo link
is enabled after the Controller confirms that the Kargo resource exists.

## Promotion lifecycle and persistence

An operator-created Promotion is inserted before its Kargo resource exists. Use
a deterministic external Promotion name derived from the Slipramp Promotion ID,
for example `slipramp-<uuid>`.

Add the minimum state needed to the existing Promotion record:

- source: `kargo` for an externally submitted Promotion or `slipramp` for an
  operator-created Promotion;
- source Stage ID for an operator-created Promotion;
- a JSON snapshot of the selected Freight item;
- dispatch attempt count and next-attempt time;
- last dispatch time and successful dispatch time;
- Kargo Promotion UID when creation succeeds; and
- the latest dispatch diagnostic in the existing message field.

The current request digest, base revision, candidate revision, and candidate
branch are unknown while an operator-created Promotion is `creating`. Make them
nullable until the existing Kargo submission endpoint supplies them. Keep the
target Stage, repository, policy, selected Freight, and requesting operator
snapshots immutable from creation.

The effective lifecycle is:

```text
creating ──Kargo callback──> preparing ──> pending ──> approved/rejected/expired
    │                              └───────────────────────────────> error
    └──terminal creation failure──────────────────────────────────> error
```

Automatic-policy Promotions may still move from the Kargo callback directly to
`approved`, as they do today.

For an operator-created Promotion, `requestedBy` is the human operator and must
not be overwritten by the Kargo service account during callback correlation.
Externally created Kargo Promotions retain the current service-account
attribution.

## HTTP contract

Add the product endpoint:

```text
POST /api/v1/promotion-stages/{stageId}/promotions
```

Request body:

```json
{
  "sourceStageId": "uuid",
  "freightName": "exact-kargo-freight-name"
}
```

The Server resolves the integration, cluster, project, namespace, target Stage,
and source Freight from durable Stage observations. The browser must not submit
trusted Kubernetes coordinates. On success, return `201 Created`, the normal
`Promotion` representation, and its resource location. Protect the operation
with the existing `promotions:write` permission.

Extend the Controller contract with:

- a `create-kargo-promotion` command containing the Promotion ID, deterministic
  external name, integration, project namespace, target Stage name and UID, and
  Freight name; and
- a Controller-only dispatch report endpoint that accepts `created`,
  `retryable`, or `failed`, plus the Kargo UID and a bounded diagnostic where
  applicable.

Keep these changes in the existing promotion and controller OpenAPI files and
regenerate all Go and TypeScript clients.

## Server behavior

Creating a Promotion is one database transaction that:

1. Loads the target and source Stages by UUID.
2. Requires both observations to be active and fresh.
3. Requires the Stages to belong to the same integration, cluster, and Kargo
   project.
4. Confirms that the source Stage currently contains the exact Freight.
5. For cross-Stage promotion, confirms that the target declares the source
   Stage for the Freight origin. For in-place promotion, requires the source and
   target Stage IDs to match.
6. Snapshots the target Stage, repository, effective policy, selected Freight,
   and operator.
7. Inserts the Promotion in `creating` state with its deterministic Kargo name
   and initial dispatch time.

Controller polling selects due Promotion creation work for the Controller's
cluster. Keep the selection order simple: Run cancellations first, then Kargo
Promotion creation, then ordinary Run assignment. Advancing the attempt and
next-attempt time must happen atomically with selection so an ambiguous poll can
only cause an idempotent redelivery.

Use a small fixed bounded retry policy in code rather than new configuration.
Transient Kubernetes and transport failures are retried with backoff. Invalid
or forbidden Kargo admissions and an exhausted retry budget move the Promotion
to `error`. One failing Promotion must not block later eligible Promotions.

## Controller behavior

Create this resource in the observed Kargo project namespace:

```yaml
apiVersion: kargo.akuity.io/v1alpha1
kind: Promotion
metadata:
  name: slipramp-<promotion-uuid>
  annotations:
    slipramp.com/promotion-id: <promotion-uuid>
spec:
  stage: <target-stage>
  freight: <exact-freight-name>
```

Before creation, confirm that the observed target Stage still has the UID from
the command. Treat a matching existing Promotion as success. If an object with
the deterministic name exists but its annotation, Stage, or Freight differs,
report a terminal conflict instead of adopting it.

The Controller reports the result to the Server before polling again. An
ambiguous report is safe because both Kubernetes creation and the report are
idempotent.

Extend the controller Helm RBAC in configured Kargo namespaces with the minimum
Kargo permissions:

- `create` and `get` on `promotions.kargo.akuity.io`; and
- the Kargo-specific `promote` verb on `stages.kargo.akuity.io`.

Do not add Kargo API credentials, impersonation, Jobs, a message broker, or a
second Controller channel.

## Kargo callback correlation

The existing Kargo submission endpoint remains the entry point for review data.
When `(integration, externalPromotionId)` identifies a `creating` Slipramp
Promotion:

1. Verify that the callback's project and Stage match the snapshotted target.
2. Store the request digest, base and candidate revisions, and candidate branch.
3. Preserve the original operator attribution and policy snapshot.
4. Continue through the existing automatic or review-required preparation
   behavior.

Repeated identical callbacks remain idempotent. A callback with a different
payload for the same identity remains a conflict. The callback may win a race
with the Controller's success report; a late success report must not move the
Promotion backward from `preparing`, `pending`, or `approved`.

Externally created Kargo Promotions continue through the current insertion path
without first entering `creating`.

## Stage observation and page data

Extend normalized Stage observations with only the Kargo topology needed for
validation and the pipeline dialog:

- requested Freight origin;
- whether direct Warehouse Freight is allowed; and
- declared source Stage names.

Persist that normalized data with the Stage. Do not infer upstream relationships
from `environmentOrder`; it remains presentation configuration.

Expose the source relationships on pipeline Stage responses. The pipeline page
can then match an upstream Stage's current Freight to the destination's declared
origin and source Stage.

Add the current Freight to branch-drift Stage responses so the in-place action
does not need a second request. Existing Stage freshness and archived state
remain the basis for enabling the actions.

## Implementation sections

### 1. Contract, model, and storage

- Add the product create endpoint, `creating` status, source/topology models,
  Controller command, and dispatch report to OpenAPI.
- Regenerate all clients and server interfaces.
- Add the migration and update promotion store queries and domain validation.
- Implement transactional operator creation and due-dispatch selection.

Acceptance: an authorized API call creates and returns a durable `creating`
Promotion with immutable operator, Stage, policy, and Freight snapshots.

### 2. Stage topology and promotion candidates

- Normalize and persist Kargo `requestedFreight` sources.
- Return topology in pipeline data and current Freight in branch-drift data.
- Add domain tests for upstream matching, multiple Freight origins, stale
  Stages, archived Stages, and invalid cross-project sources.

Acceptance: both pages have enough server-provided data to name one exact
Freight and source Stage without guessing pipeline topology.

### 3. Controller dispatch

- Select due creation commands through the existing poll endpoint.
- Render and create the Kargo Promotion with the dynamic client.
- Verify deterministic-name collisions and report results.
- Add namespace-scoped Helm RBAC and chart tests.

Acceptance: redelivering the same command creates at most one matching Kargo
Promotion, and permanent admission failures become visible on the Slipramp
Promotion.

### 4. Callback hydration and Promotion detail

- Correlate the Kargo callback with pre-created Promotions.
- Relax callback-only fields while status is `creating` and enforce them after
  hydration.
- Render `creating` and creation failures on the existing Promotion detail and
  list pages.
- Revalidate the detail page while creation is active.

Acceptance: the same Slipramp URL follows the Promotion from operator submission
through its existing preparation and review flow.

### 5. Pipeline and branch-drift actions

- Load the current principal on both pages and require `promotions:write` for
  the action.
- Add a shared Freight promotion dialog.
- Add the pipeline target-Stage action and branch-drift in-place action.
- Implement the view and background submit behaviors.

Acceptance: both entry points create the same API resource, display exact
Freight context before submission, and surface API errors without losing the
page filters.

### 6. End-to-end acceptance and documentation

- Cover API authorization, validation, dispatch retry, callback races, and
  idempotency in unit and PostgreSQL integration tests.
- Add Controller tests for created, already-existing, UID mismatch, admission
  rejection, transient failure, and ambiguous result reporting.
- Add one disposable-cluster acceptance path from UI/API creation through the
  existing Kargo callback.
- Update `DESIGN.md`, `ROADMAP.md`, chart documentation, and configuration
  documentation when the implementation lands.

Acceptance: generated code is current, checks pass, and a live test reaches the
normal Slipramp Promotion review or automatic-approval flow.

Sections 2 and 3 can proceed in parallel after the contract in section 1 is
settled. Sections 4 and 5 can then proceed in parallel against those generated
types.

## Explicitly out of scope

- Promoting to every downstream Stage in one action.
- Browsing arbitrary Warehouse Freight or building a general Freight catalog.
- Precomputing all Kargo admission rules in Slipramp.
- Canceling or deleting a Kargo Promotion after creation.
- A new permission, audit subsystem, notification event, scheduler, or message
  broker.
- Server-to-Kargo HTTP credentials or direct Server Kubernetes access.
- Tracking post-approval Kargo execution beyond the existing integration
  contract.
- Compatibility aliases or support for older generated contracts and database
  shapes.

These can be added later only if operator use demonstrates a concrete need.
