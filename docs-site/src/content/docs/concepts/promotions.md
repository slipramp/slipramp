---
title: Promotions
description: Promote exact Kargo Freight with optional Slipramp review and previews.
---

Slipramp inventories every Kargo Stage in a namespace assigned to a configured
integration. Every non-control-flow Stage can be promoted when Kargo reports
exact eligible Freight. The Stage does not need Slipramp annotations, a mapped
Repository, an inferred environment, or a known branch.

Native promotion snapshots the Stage name and Kubernetes UID, the selected
Freight, requesting operator, and effective policy. Slipramp then asks the
responsible Controller to create a Kargo Promotion using only Stage and Freight
identity. Kargo remains responsible for admission, its promotion template,
verification, promotion windows, rendering, and downstream reconciliation.

## Optional enrichment

Stage annotations and Server review configuration can add a resolved
Repository, Application, environment, target branch, and source paths. Complete
enrichment enables Slipramp change review, Git and Argo CD previews, links, and
branch-drift information. These are separate capabilities: missing enrichment
does not disable native promotion.

The default policy is `automatic`. An operator-created automatic Promotion is
approved immediately; Kargo object delivery and Kargo's eventual outcome remain
separate status facts. The callback used by review-oriented Kargo templates is
optional for automatic Promotions.

`review-required` can be selected only when the Stage has complete review
context. If a Stage later loses that context, Slipramp preserves the configured
policy and blocks new Promotions. It never silently changes the policy to
automatic. An operator can restore the context or deliberately change the
policy back to `automatic`.

## Reading Promotion status

A Promotion shows the following facts.

- Review state records automatic approval or the preparation, pending, and
  operator-decision lifecycle.
- Creation delivery shows whether a Slipramp-originated Kargo object is waiting,
  delivered, stopped by cancellation, or terminally failed.
- Kargo execution reports reconciliation and the exact terminal `Succeeded`,
  `Failed`, `Errored`, or `Aborted` phase.
- Cancellation records who requested it, when, and whether it completed or was
  overtaken by another outcome.

A failed Kargo execution does not rewrite an automatic approval as a review
failure. A native Promotion without review context also omits Repository,
branch, and preview sections instead of displaying placeholder values.

## Canceling a Promotion

Open the Promotion and choose **Cancel Promotion**. This stops pending review
and gate work and asks any linked preparation Run to cancel. If delivery has
never started, the Promotion shows **Canceled** immediately. An active Kargo
Promotion shows **Canceling** while the Controller requests an abort and waits
for Kargo's result. If Kargo finishes first, Slipramp shows its actual outcome
alongside the cancellation request.

Cancellation preserves earlier decisions and any changes Kargo steps already
made. The exact Kargo outcome remains available in the Promotion detail.
