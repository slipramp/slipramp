import { Anchor, Badge, Group, Stack, Text } from "@mantine/core";
import { Link } from "react-router";

import type { PromotionFreightAlignmentStatus, PromotionProjectDetail } from "~/api/generated";
import { PromotionProjectLink } from "~/features/promotions/promotion-project-link";
import type { PromotionInventoryStatus } from "~/features/promotions/promotion-project-filters";
import { SearchParamFilters, type SearchParamFilter } from "~/shared/list-controls";
import { promotionProjectDetailPath } from "~/features/promotions/promotion-project-inventory-state";

export function PromotionInventoryFilters({
  cluster,
  integration,
  inventoryState,
  path,
  project,
}: Readonly<{
  cluster?: string;
  integration?: string;
  inventoryState?: {
    alignment?: PromotionFreightAlignmentStatus;
    status?: PromotionInventoryStatus;
  };
  path: string;
  project?: string;
}>) {
  const filters: SearchParamFilter[] = [
    {
      active: Boolean(integration),
      kind: "text",
      label: "Integration",
      name: "integration",
      value: integration,
    },
    {
      active: Boolean(project),
      kind: "text",
      label: "Project",
      name: "project",
      value: project,
    },
    {
      active: Boolean(cluster),
      kind: "text",
      label: "Cluster",
      name: "cluster",
      value: cluster,
    },
  ];

  if (inventoryState) {
    filters.push(
      {
        active: Boolean(inventoryState.status),
        data: [
          { label: "All statuses", value: "" },
          { label: "Non-ready", value: "unhealthy" },
          { label: "Stale", value: "stale" },
          { label: "Pending review", value: "pending-review" },
        ],
        kind: "select",
        label: "Status",
        name: "attention",
        value: inventoryState.status,
      },
      {
        active: Boolean(inventoryState.alignment),
        data: [
          { label: "All alignment states", value: "" },
          { label: "Different", value: "different" },
          { label: "Unknown", value: "unknown" },
          { label: "Aligned", value: "aligned" },
        ],
        kind: "select",
        label: "Freight alignment",
        name: "alignment",
        value: inventoryState.alignment,
      },
    );
  }

  return (
    <SearchParamFilters
      clearTo={path}
      filters={filters}
      preserve={inventoryState ? ["freight"] : ["q", "attention", "alignment", "freight"]}
    />
  );
}

export function PromotionProjectSectionHeader({
  project,
  view,
}: Readonly<{ project: PromotionProjectDetail; view: "pipelines" | "stages" }>) {
  const detailPath = promotionProjectDetailPath(project);
  const viewPath = view === "pipelines" ? detailPath : `${detailPath}?view=stages`;

  return (
    <Stack gap={2}>
      <Group gap="sm" wrap="wrap">
        <PromotionProjectLink heading project={project} to={viewPath} />
        <Badge variant="light">{project.integration}</Badge>
      </Group>
      <Text c="dimmed" size="sm">
        Cluster{" "}
        <Anchor component={Link} to={`/clusters/${encodeURIComponent(project.cluster)}`}>
          {project.cluster}
        </Anchor>
      </Text>
    </Stack>
  );
}
