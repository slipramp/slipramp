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

A Promotion presents three independent facts:

- Review state records automatic approval or the preparation, pending, and
  operator-decision lifecycle.
- Creation delivery shows whether a Slipramp-originated Kargo object is waiting,
  delivered, or terminally failed.
- Kargo execution reports reconciliation and the exact terminal `Succeeded`,
  `Failed`, `Errored`, or `Aborted` phase.

A failed Kargo execution does not rewrite an automatic approval as a review
failure. A native Promotion without review context also omits Repository,
branch, and preview sections instead of displaying placeholder values.
