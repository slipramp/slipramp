import { Anchor, Badge, Group, Stack, Text, Title } from "@mantine/core";
import { Link, useSearchParams } from "react-router";

import type { PromotionFreightAlignmentStatus, PromotionProjectDetail } from "~/api/generated";
import { KargoLink } from "~/features/promotions/kargo-link";
import {
  promotionProjectAttention,
  type PromotionProjectAttention,
} from "~/features/promotions/promotion-project-filters";
import { projectAlignment } from "~/features/promotions/promotion-project-view";
import { SearchParamFilters } from "~/shared/list-controls";
import { promotionProjectDetailPath } from "~/features/promotions/promotion-project-inventory-state";

export function usePromotionInventoryPresentation() {
  const [searchParams, setSearchParams] = useSearchParams();
  const attention = promotionProjectAttention(searchParams.get("attention"));
  const alignment = projectAlignment(searchParams.get("alignment"));
  const query = searchParams.get("q") ?? "";

  function changeAttention(value?: PromotionProjectAttention) {
    const next = new URLSearchParams(searchParams);
    if (value) next.set("attention", value);
    else next.delete("attention");
    next.delete("page");
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  function changeAlignment(value?: PromotionFreightAlignmentStatus) {
    const next = new URLSearchParams(searchParams);
    if (value) next.set("alignment", value);
    else next.delete("alignment");
    next.delete("page");
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  function changeQuery(value: string) {
    const next = new URLSearchParams(searchParams);
    if (value) next.set("q", value);
    else next.delete("q");
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  return {
    alignment,
    attention,
    changeAlignment,
    changeAttention,
    changeQuery,
    query,
    searchParams,
    setSearchParams,
  };
}

export function PromotionInventoryScopeFilters({
  cluster,
  integration,
  path,
  project,
}: Readonly<{
  cluster?: string;
  integration?: string;
  path: string;
  project?: string;
}>) {
  return (
    <SearchParamFilters
      clearTo={path}
      filters={[
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
      ]}
      preserve={["q", "attention", "alignment"]}
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
    <Group align="flex-end" justify="space-between" wrap="wrap">
      <Stack gap={2}>
        <Group gap="sm" wrap="wrap">
          <Title order={2} size="h3">
            <Anchor component={Link} inherit to={viewPath}>
              {project.project}
            </Anchor>
          </Title>
          <Badge variant="light">{project.integration}</Badge>
        </Group>
        <Text c="dimmed" size="sm">
          Cluster{" "}
          <Anchor component={Link} to={`/clusters/${encodeURIComponent(project.cluster)}`}>
            {project.cluster}
          </Anchor>
        </Text>
      </Stack>
      {project.kargoUrl ? (
        <KargoLink url={project.kargoUrl}>Open Project in Kargo</KargoLink>
      ) : null}
    </Group>
  );
}
