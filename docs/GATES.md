# Release Gates

Repository-defined Gates delay a Run or Kargo Promotion until a numeric
Prometheus observation passes. They are opt-in release policy: work without an
attached Gate remains valid, and a Gate never stops work that has already been
released.

## Configure a source

Configure the source before activating a catalog that references it. Helm
installations use `server.gates.sources`; direct Server installations set
`SLIPRAMP_GATE_SOURCES_FILE`. Source configuration owns the endpoint,
credentials, cadence, timeout, and concurrency. Repository authors supply only
the PromQL and numeric condition.

```yaml
apiVersion: slipramp.com/v1alpha1
kind: GateSourceConfig
sources:
  production:
    prometheus:
      url: https://prometheus.example.com
      evaluationInterval: 15s
      timeout: 5s
      maximumConcurrency: 4
      bearerTokenFile: /var/run/slipramp/gates/token
```

The Server evaluates one query per unique normalized definition, shares the
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
  prometheus:
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

## Rollout

1. Verify the metric and PromQL directly against production-like data.
2. Deploy the source configuration and confirm the Server restarts cleanly.
3. Activate the Gate definition without attaching it; unreferenced Gates are
   not evaluated.
4. Attach it to one mutating Action and exercise passing, blocking, source
   outage, and recovery behavior.
5. Attach it to the intended Kargo Stage only after the Action path is proven.

Every required Gate needs a fresh pass or a matching override in the same
release check. Passing observations must come from the current Server process
and active catalog revision. Overrides survive Server restarts and catalog
refreshes while the definition digest matches.

## Override a Gate

On the Run or Promotion detail page, open **Details**, find the Gate, and choose
**Override gate**. Check the observation and condition, enter the reason, and
select **Record override**. The decision applies only to this subject and Gate
definition. Its history shows who recorded it, when, and what the evaluator saw.
Review and all other requirements still apply.

Operators and admins can also send `PUT /api/v1/runs/{runId}/gates/{gateName}/override`
or `PUT /api/v1/promotions/{promotionId}/gates/{gateName}/override` with the
`definitionDigest` from the Gate detail and a nonblank `reason`. API credentials
need the matching `slipramp:runs:override-gates` or `slipramp:promotions:override-gates`
scope. A changed definition returns a conflict so the operator can review it.

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
