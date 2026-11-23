# Release Gates

Repository-defined Gates delay a Run or Kargo Promotion until a Prometheus query or
subject-specific webhook passes, or an operator records an override. Actions
and Stages opt in by attaching Gate names.

MCP clients can manage scope policy and waive eligible Gates on individual
Runs and Promotions. [MCP Gate tools](./MCP.md#working-with-gates)

## Manage Server locks

Run assignment checks the global and Target locks and weekly windows for every
Action with frozen `reconcile` or `operation` intent. A preview checks only
its attached repository Gates. Mediated Promotions check the global and
destination Stage policies together with any repository Gates on the Stage.

| Scope | Resource | Minimum role to change policy |
| --- | --- | --- |
| Global | `/api/v1/gate-scopes/global` | Administrator |
| Target | `/api/v1/gate-scopes/targets/{targetId}` | Operator |
| Kargo Stage | `/api/v1/gate-scopes/stages/{stageId}` | Operator |

In the web app, open **Administer → Global Gates** for installation-wide
policy, or open a Target or Stage and select **Gates**. Each local tab lists
its **Repository Gates** first, followed by its own lock, weekly window, and
policy history. Target attachments are listed by Action.

While a global lock is active, a warning appears on every signed-in product
page. It shows who created the lock, the reason if supplied, and its expiry or
**Until released**. The warning checks for changes while the page is visible
and when you return to the tab. Follow **Gates** in the warning to inspect the
policy and its history. An active Target lock appears above the back link on the
Target and its Run detail pages. An active Stage lock appears in the same place
on the Stage and its Promotion detail pages. When both a global and a local
lock are active, the global warning appears first. Each warning links to its
own **Gates** view.

Choose **Create lock** and add a reason if desired. Select **1 hour**, **1 day**,
or **1 week** to fill the expiry in UTC. You can also enter a custom time or
leave the expiry empty to hold until released.
**Release lock** clears the hold for everyone waiting on that
scope. To let one Run or Promotion through, open **Details** beside its Gate
and use the **Override gate** form.
**Policy history** records who created or released the hold and when.

Use `GET` on the scope resource to read its current lock, lock version, and
20 most recent changes, newest first. Inspect the global and local scopes
separately. A scoped API key needs `slipramp:read` to inspect a scope and
`slipramp:gates:manage` to change its lock.

Send `PUT` to the resource's `/lock` child with a JSON object. Both `reason`
and `expiresAt` are optional.

```json
{"reason":"Database maintenance","expiresAt":"2027-01-15T18:00:00Z"}
```

An omitted expiry keeps the lock active until an authorized operator sends
`DELETE` to the same child. At the exact expiry instant the lock becomes
inactive. Its history retains the original expiry, including after a Server
restart. Repeating the same active lock request as its creator returns the
existing version. A conflicting request returns HTTP 409. Clearing an inactive
lock succeeds without adding an event. Re-locking creates a new version.
Target and Stage locks survive archival and reactivation.

## Manage weekly windows

On the same page, choose **Set weekly window**. Enter the IANA time zone,
select weekdays, and enter the local start and end times. Use **Add interval**
for another range. The saved window shows its schedule, effective state,
observation validity, and the next transition when one is known.

**Edit weekly window** loads the saved intervals. Saving a new version
invalidates waivers for the previous version. **Remove window** clears the
schedule for that scope. Both changes appear in **Policy history**.

Send `PUT` to the scope's `/window` child. For example, this body opens a
Denver window on Mondays and Tuesdays from 09:00 up to 17:00 local time.

```json
{
  "timeZone": "America/Denver",
  "weekly": [{"days": ["monday", "tuesday"], "start": "09:00", "end": "17:00"}]
}
```

Use lowercase weekday names and an IANA time zone. Split overnight hours into
two entries, ending the first at `24:00` and starting the next at `00:00`.
Adjacent intervals form one continuous window. If a clock change skips an
opening or closing time, that occurrence stays closed. A repeated opening uses
the later instant and a repeated closing uses the earlier instant.

Inspect `window.state` on the scope to see whether the window is passing,
blocking, or unavailable. Its observation gives the evaluation time, exclusive
expiry, and next transition when one exists. Every accepted `PUT` advances
`windowVersion`, even for the same hours, so earlier waivers expire. Send
`DELETE` to remove the schedule. The scope retains the last 20 window changes
in `windowEvents`. Window and lock versions advance independently.

## Read Gate evidence

Open **Run details** or **Promotion details** to see the Gates table. Choose
**Details** on a Gate’s row to see its observation and override history.
Repository Gates show their evaluator and release condition. For locks and
weekly windows, select the policy version to open the corresponding change in
the global, Target, or Stage policy history. Older versions open the history
section when the change is outside the retained list.

| Gate | Evidence shown when available |
| --- | --- |
| Prometheus | Numeric sample, comparison threshold, and evaluation time. **Open in Prometheus** graphs the recorded query when a viewer is configured. |
| Webhook | Decision code, message, and evaluation time. |
| Weekly window | Observed open or closed state, time zone, evaluation time, and known interval boundaries. |
| Lock | Reason, actor, creation time, expiry, and when Slipramp checked the lock. |

While work waits, an expired window observation can show **Open** alongside
an **Unavailable** Gate state. The diagnostic explains the hold. After release,
**Evidence at release** preserves the observation and release time. Override
history shows who recorded each decision, when, and their reason if supplied.

## Inspect a waiting Run

Open the Run details to see the locks, windows, and repository Gates that apply.
An active lock holds the Run until it expires, is cleared, or receives a waiver.
Every configured window must be open with a fresh observation at assignment
unless the Run has a waiver for that window version. An unavailable observation
holds the Run even when its last evaluation found the window open.

A blocked Run stays queued. Controller polls can assign later eligible Runs
while it waits. Assignment records both scopes’ policy versions, the window
observations, and the overrides used. Run details retain that evidence after
the policy changes and on Controller redelivery. Runs assigned before migration
078 retain their original release records.

## Inspect a waiting Promotion

An automatic Promotion created in Slipramp waits before its first Kargo
creation delivery. A review-required Promotion can be created and prepared
while a lock is active. After approval, the Kargo PromotionTask receives
`gated` until every applicable requirement passes or has a matching waiver.
A Kargo-originated Promotion checks the same policies on its approved callback.

Open the Promotion details to see global and Stage locks, global and Stage
windows, then repository Gates. The Stage’s **Gates** tab shows current policies.
A waiver belongs to one Promotion and one exact policy version. Once released,
the Promotion keeps the evidence from that instant across callbacks and
delivery retries. Migration 080 preserves earlier releases and begins checking
policies for Promotions still waiting.

A Promotion created directly in Kargo that never calls Slipramp bypasses
Slipramp Gate checks. Kargo-native promotion windows still apply independently.

## Action intent and upgrades

Declare an intent on each Action, including Actions with no Gates. Use
`preview` for a plan or inspection, `reconcile` for applying desired state,
and `operation` for changes such as a restart. An explicit repository Gate
can delay any of them.

When upgrading from optional Action intent, commit and refresh the updated
catalogs before upgrading the Server. Upgrade iOS clients with the Server for
product API contract 6, which includes lock and window evidence in Run responses.
Reading a stored Target or catalog containing an unclassified Action returns HTTP 409. The Action explanation also returns 409
when the selected Action lacks a valid intent.

Migration 072 requires each queued or assigned Run to have a valid frozen
intent. This read-only query identifies affected Runs before the upgrade.

```sql
SELECT id, target_name, action_name, phase
FROM runs
WHERE phase IN ('queued', 'assigned')
  AND (snapshot #>> '{action,Intent}' IN ('preview', 'reconcile', 'operation')) IS NOT TRUE;
```

Let these Runs advance past assignment, or cancel them if the work is no longer
wanted. A catalog refresh only changes future Runs. For a finished Run that
lacks intent, use a latest rerun against the corrected Action when eligible.
For a Run with Artifact inputs, create fresh inputs against the corrected revision.

## Configure a source

Configure the source before activating a catalog that references it. Helm
installations use `server.gates.sources`; direct Server installations set
`SLIPRAMP_GATE_SOURCES_FILE`. Source configuration owns the endpoint,
credentials, cadence, timeout, and concurrency. Repository authors supply only
the PromQL and numeric condition or the webhook policy input.

```yaml
apiVersion: slipramp.com/v1alpha1
kind: GateSourceConfig
sources:
  production:
    type: prometheus
    url: https://prometheus.monitoring.svc:9090
    publicUrl: https://metrics.example.com/prometheus
    evaluationInterval: 15s
    timeout: 5s
    maximumConcurrency: 4
    bearerTokenFile: /var/run/slipramp/gates/token
```

Set the optional `publicUrl` to the base URL users visit for this Prometheus
source, including any path prefix. **Open in Prometheus** on a Run or Promotion
opens Graph view using the user's own browser access. The graph ends at the
Gate’s observation time when available and opens a live query otherwise. A released
Gate uses the source and expression saved at release.

For Prometheus, the Server evaluates one query per unique normalized definition, shares the
result across all waiting subjects, and bounds concurrent requests separately
for each source. Unavailable results back off to at most one minute with small
jitter. A successful numeric result restores the normal interval.

## Define and attach a Gate

Define the Gate in the same Repository catalog as the Action that uses it:

```yaml
apiVersion: slipramp.com/v1alpha1
kind: Gate
metadata:
  name: teamspeak-idle
  title: TeamSpeak is idle
spec:
  evaluator:
    type: prometheus
    source: production
    query: max_over_time(teamspeak_active_users[1m])
    condition:
      operator: equal
      value: 0
```

Attach its name to only the mutating Actions that must wait. A Kargo Stage uses
the `slipramp.com/promotion-gates` annotation with a JSON array of Gate names.
The Stage must also map to exactly one allowed Slipramp Repository.

Validate a checkout against the installation's non-secret source names before
refreshing it:

```sh
slipramp catalog validate --gate-sources ./gate-sources.yaml
```

## Use a webhook policy

Configure a fixed evaluation endpoint in the Server's Gate source file.

```yaml
sources:
  change-policy:
    type: webhook
    url: https://policy.example.com/slipramp/evaluate
    bearerTokenFile: /var/run/slipramp/gates/policy-token
```

Use HTTPS across untrusted networks. HTTP requires a trusted encrypted network.
Webhook sources support the same interval, timeout, concurrency, CA and client
certificate settings as Prometheus. The Server posts to the exact configured
URL and rejects redirects.

A repository Gate selects this source and supplies an object containing public
policy input. Attach the Gate name to an Action or Stage as usual.

```yaml
spec:
  evaluator:
    type: webhook
    source: change-policy
    input:
      policy: production-deploy
```

Input can contain JSON objects, arrays, strings, finite numbers, booleans and
null. The encoded object may use up to 8 KiB, eight levels and 64 properties
in total. Keys are 1–64 characters and strings are at most 1,024 characters.
Use strings for integer identifiers outside the JSON safe-integer range.

The endpoint receives an `application/json` POST for each waiting Run or
Promotion. Use a side-effect-free handler that can answer repeated requests.
The endpoint gets these fields.

| Field | Meaning |
| --- | --- |
| `apiVersion` | `slipramp.com/gate-evaluation/v1alpha1` |
| `evaluationId`, `evaluationKey`, `evaluatedAt` | Unique attempt UUID, stable subject-and-definition key, and Server evaluation time in UTC |
| `repositoryId`, `repositoryName`, `catalogRevisionId`, `commitSha` | Repository and active catalog that supplied the policy |
| `definitionDigest`, `input` | Exact policy digest and repository-supplied input object |
| `subject.type`, `subject.id` | `run` or `promotion`, and the waiting subject's UUID |
| `subject.run` | Run-only fields `targetId`, `targetName`, `actionName`, `intent`, `catalogRevisionId`, and `commitSha`. The last two identify the Run's frozen catalog and checkout. |
| `subject.promotion` | Promotion-only fields `stageId`, `integration`, `cluster`, `project`, and `stageName` |

If a required policy or subject field is missing, the Server marks the
observation unavailable and skips the request. A catalog refresh can advance
the top-level policy commit while a queued Run keeps its original
`subject.run.commitSha`.

Return HTTP 2xx and `Content-Type: application/json` with all four fields.

```json
{
  "apiVersion": "slipramp.com/gate-evaluation/v1alpha1",
  "decision": "passing",
  "code": "change-approved",
  "message": "Approved change record is active"
}
```

Return `passing`, `blocking`, or `unavailable`. The code must match
`^[a-z0-9][a-z0-9._-]{0,63}$`. The message must contain 1–256 printable
characters and include visible text. Both fields are operator-visible and must
contain only public information. Responses over 4 KiB, unknown or duplicate
fields, invalid decisions, timeouts and transport failures produce an
unavailable observation with a bounded diagnostic. The evaluator retries
while the subject waits. A pass lasts three evaluation intervals and applies
only to that subject, active catalog revision and Server process.

The Gate panel shows the webhook code and message for a passing or blocking
observation. On release, the subject retains that evidence or its matching
override. For an outage, inspect the diagnostic and the `failure_class` on
Gate evaluation metrics with `source_type="webhook"`. After a Server restart,
waiting subjects need a fresh decision from the new process. Restoring the
endpoint lets the next successful retry release each subject that passes.

## Upgrading existing Prometheus Gates

Move catalog policy fields from `spec.prometheus` to `spec.evaluator` and set
`type: prometheus` inside that object. In each Server or Helm source entry,
move the fields up one level and set `type: prometheus` beside `url`. Deploy
the matching source configuration with the Server upgrade and refresh the
updated catalog. An unchanged policy retains its definition digest and overrides.

The database migration preserves live and released samples. Run and Promotion
API clients read the sample at `evidence.prometheus.value`. An override with a
sample records it at `observedEvidence.prometheus.value`. A zero is a recorded
sample. An override made before an observation has no sample field.

## Rollout

1. Verify the metric and PromQL directly against production-like data.
2. Deploy the source configuration and confirm the Server restarts cleanly.
3. Activate the Gate definition without attaching it; unreferenced Gates are
   not evaluated.
4. Attach it to one mutating Action and exercise passing, blocking, source
   outage, and recovery behavior.
5. Attach it to the intended Kargo Stage only after the Action path is proven.

Every required Gate needs a fresh pass or a matching override in the same
release check. Passing repository observations must come from the current Server process
and active catalog revision. Overrides survive Server restarts and catalog
refreshes while the definition digest matches.

## Override a Gate

In the Run or Promotion’s Gates table, choose **Details** on the Gate’s row.
Check the observation and condition, add a reason if desired, and select
**Override Gate**. The decision applies only to this subject and Gate
definition. Its history shows who recorded it, when, and what the evaluator saw.
Review and all other requirements still apply.

To inspect repository Gates, locks, and windows for one subject, send `GET` to
`/api/v1/runs/{runId}/gate-overrides` or
`/api/v1/promotions/{promotionId}/gate-overrides`. The `eligible` list gives
the Gates available to waive. The `decisions` list shows prior waivers in
chronological order, including waivers for holds that have ended.

Operators and admins can send `POST` to the same URL. Copy the `key` and
`version` from the eligible Gate. You may include a `reason` of up to 1024 characters. A repository
Gate key looks like `repository/teamspeak-idle`. A Server lock key looks like
`lock/<scope-uuid>`. A waiver applies to this subject and this hold alone.
If the scope is locked again, the new hold needs its own waiver. A weekly window
uses `window/<scope-uuid>`. Its waiver applies only to that window version.

A successful request returns HTTP 200 with the affected Gate and its full
override history. A changed version, a fresh pass, or a Gate that no longer
applies returns HTTP 409. Refresh the subject before deciding again. API
credentials need the matching `slipramp:runs:override-gates` or
`slipramp:promotions:override-gates` scope.

## Troubleshooting

Start with the Gate panel on the Run or Promotion detail page, then correlate
it with these bounded metrics and the Server's `gate_evaluator` logs:

- `unevaluated`: confirm the subject is still eligible to wait, the Gate still
  exists in the active catalog, and the configured source name matches.
- `blocking`: run the exact PromQL at the displayed evaluation time and verify
  the numeric threshold. Slipramp accepts only a scalar or a one-sample instant
  vector.
- `unavailable`: check Server-to-Prometheus network reachability, TLS and token
  mounts, the request timeout, HTTP status, response size, and result
  cardinality. Diagnostics are intentionally sanitized.
- `stale`: the last observation passed its three-interval validity window.
  Check `slipramp_gate_evaluation_attempts_total` and evaluation duration for
  continuing attempts and source recovery.
- a Promotion remains gated after approval: approval grants permission but
  does not release the Gate. Confirm the Kargo `PromotionTask` timeout is long
  enough for the operational wait and that callbacks continue polling.
- a definition was removed while work waits: restore the same Gate name or
  cancel and recreate the subject after deliberately changing its attachment.
  Waiting work fails closed.

`slipramp_gate_waiting` reports the current number of waiting Runs and
Promotions without exposing Repository, Gate, Target, Stage, or query labels.
Expired observations with no matching waiter are removed after a 24-hour grace
period; released subjects retain their immutable evidence.
