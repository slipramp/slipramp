import type {
  Cluster,
  ProductPrincipal,
  Promotion,
  ReadinessResponse,
  Repository,
  Run,
} from "~/api/generated";

export type InboxItemKind =
  | "promotion-review"
  | "promotion-error"
  | "repository-refresh"
  | "cluster-unreachable"
  | "run-observation"
  | "dependency-unavailable";

export type InboxItem = {
  actionLabel: string;
  description: string;
  href: string;
  id: string;
  kind: InboxItemKind;
  occurredAt?: string;
  title: string;
};

type InboxInput = {
  activeRuns: Run[];
  principal?: ProductPrincipal;
  promotions: Promotion[];
  readiness?: ReadinessResponse;
  repositories: Repository[];
  unreachableClusters: Cluster[];
};

export function buildInbox(input: InboxInput) {
  const decisions = canReviewPromotions(input.principal)
    ? input.promotions
        .filter((promotion) => promotion.status === "pending")
        .map(promotionReviewItem)
    : [];
  const unreachableClusterNames = new Set(input.unreachableClusters.map((cluster) => cluster.name));
  const unknownRuns = input.activeRuns.filter((run) => run.observationState === "unknown");

  const problems: InboxItem[] = [
    ...dependencyItems(input.readiness),
    ...input.repositories
      .filter((repository) => repository.refresh.status === "failed")
      .map(repositoryRefreshItem),
    ...input.unreachableClusters.map((cluster) =>
      clusterItem(cluster, unknownRuns.filter((run) => run.cluster === cluster.name).length),
    ),
    ...unknownRuns
      .filter((run) => !unreachableClusterNames.has(run.cluster))
      .map(runObservationItem),
    ...input.promotions.filter((promotion) => promotion.status === "error").map(promotionErrorItem),
  ];

  return {
    decisions: decisions.toSorted(newestFirst),
    problems: problems.toSorted(newestFirst),
  };
}

function canReviewPromotions(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}

function promotionReviewItem(promotion: Promotion): InboxItem {
  const stage = snapshotString(promotion.stageSnapshot, "name");
  const application = snapshotString(promotion.stageSnapshot, "application");
  const destination = stage ? ` to ${stage}` : "";
  const context = [application, `${promotion.candidateBranch} → ${promotion.targetBranch}`]
    .filter(Boolean)
    .join(" · ");

  return {
    actionLabel: "Review",
    description: context,
    href: `/promotions/${promotion.id}`,
    id: `promotion-review:${promotion.id}`,
    kind: "promotion-review",
    occurredAt: promotion.createdAt,
    title: `Promotion${destination} awaits review`,
  };
}

function promotionErrorItem(promotion: Promotion): InboxItem {
  const stage = snapshotString(promotion.stageSnapshot, "name");

  return {
    actionLabel: "View promotion",
    description:
      promotion.message ??
      `Slipramp could not prepare the review artifacts${stage ? ` for ${stage}` : ""}.`,
    href: `/promotions/${promotion.id}`,
    id: `promotion-error:${promotion.id}`,
    kind: "promotion-error",
    occurredAt: promotion.updatedAt,
    title: stage ? `Promotion to ${stage}` : "Promotion preparation",
  };
}

function repositoryRefreshItem(repository: Repository): InboxItem {
  return {
    actionLabel: "View repository",
    description:
      repository.refresh.failureReason ??
      "The latest branch refresh failed. The last accepted catalog remains active.",
    href: `/repositories/${repository.id}`,
    id: `repository-refresh:${repository.id}`,
    kind: "repository-refresh",
    occurredAt: repository.refresh.attemptedAt,
    title: `${repository.name} could not refresh`,
  };
}

function clusterItem(cluster: Cluster, affectedRuns: number): InboxItem {
  const impact =
    affectedRuns === 0
      ? "Run observations may become unknown until the Controller reconnects."
      : `${affectedRuns} active ${affectedRuns === 1 ? "Run has" : "Runs have"} an unknown observation.`;

  return {
    actionLabel: "View cluster",
    description: impact,
    href: `/clusters/${encodeURIComponent(cluster.name)}`,
    id: `cluster-unreachable:${cluster.name}`,
    kind: "cluster-unreachable",
    occurredAt: cluster.lastSeenAt,
    title: `${cluster.name} is unreachable`,
  };
}

function runObservationItem(run: Run): InboxItem {
  return {
    actionLabel: "View Run",
    description: `The Run remains ${run.phase} on ${run.cluster}, but its execution state is stale.`,
    href: `/runs/${run.id}`,
    id: `run-observation:${run.id}`,
    kind: "run-observation",
    occurredAt: run.lastObservedAt ?? run.assignedAt ?? run.createdAt,
    title: `${run.target} · ${run.action} needs investigation`,
  };
}

function dependencyItems(readiness?: ReadinessResponse): InboxItem[] {
  if (!readiness) return [];

  const items: InboxItem[] = [];
  if (readiness.database === "unavailable") {
    items.push(dependencyItem("database", "PostgreSQL"));
  }
  if (readiness.artifactStorage === "unavailable") {
    items.push(dependencyItem("artifact-storage", "Artifact storage"));
  }

  return items;
}

function dependencyItem(id: string, label: string): InboxItem {
  return {
    actionLabel: "View system status",
    description: `${label} is required for normal Slipramp operation.`,
    href: "/clusters",
    id: `dependency-unavailable:${id}`,
    kind: "dependency-unavailable",
    title: `${label} is unavailable`,
  };
}

function snapshotString(snapshot: Record<string, unknown>, key: string) {
  const value = snapshot[key];
  return typeof value === "string" ? value : undefined;
}

function newestFirst(left: InboxItem, right: InboxItem) {
  if (!left.occurredAt) return -1;
  if (!right.occurredAt) return 1;
  return right.occurredAt.localeCompare(left.occurredAt);
}
