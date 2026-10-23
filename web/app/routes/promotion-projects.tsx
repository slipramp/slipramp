import { Alert, Anchor, Badge, Group, Paper, Stack, Table, Text, TextInput } from "@mantine/core";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listPromotionProjects, type PromotionProject } from "~/api/generated";
import { RelativeTime } from "~/components/display";
import { KargoLink } from "~/components/kargo-link";
import { ListPagination, SearchParamFilters } from "~/components/list-controls";
import { EmptyState, PageHeader } from "~/components/page";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/promotion-projects";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Promotion projects") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const integration = optionalValue(url.searchParams.get("integration"));
  const cluster = optionalValue(url.searchParams.get("cluster"));
  const project = optionalValue(url.searchParams.get("project"));
  const requestedPage = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const result = await listPromotionProjects({
    client: createServerAPIClient(request, context),
    query: {
      cluster,
      integration,
      limit: pageSize,
      offset: (requestedPage - 1) * pageSize,
      project,
    },
  });

  if (result.data) {
    const lastPage = Math.max(1, Math.ceil(result.data.total / pageSize));
    if (requestedPage > lastPage) {
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      return redirect(`${url.pathname}${url.search}`);
    }
  }

  return {
    available: result.data !== undefined,
    cluster,
    error: result.error?.detail ?? result.error?.title,
    integration,
    page: requestedPage,
    project,
    projects: result.data?.items ?? [],
    total: result.data?.total ?? 0,
  };
}

export default function PromotionProjects({ loaderData }: Route.ComponentProps) {
  const filtered = Boolean(loaderData.cluster || loaderData.integration || loaderData.project);

  return (
    <Stack gap="lg">
      <PageHeader
        description="Observed Kargo projects with direct links to their promotion pipelines."
        title="Promotion projects"
      />

      <SearchParamFilters active={filtered} clearTo="/promotion-projects">
        <TextInput
          defaultValue={loaderData.integration}
          label="Integration"
          name="integration"
          style={{ flex: "1 1 200px" }}
          type="search"
        />
        <TextInput
          defaultValue={loaderData.project}
          label="Project"
          name="project"
          style={{ flex: "1 1 240px" }}
          type="search"
        />
        <TextInput
          defaultValue={loaderData.cluster}
          label="Cluster"
          name="cluster"
          style={{ flex: "1 1 200px" }}
          type="search"
        />
      </SearchParamFilters>

      {!loaderData.available ? (
        <Alert color="red" title="Promotion projects unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : loaderData.projects.length === 0 ? (
        <EmptyState
          description={
            filtered
              ? "No active observed Kargo projects match these filters."
              : "No Kargo projects with active Stage observations are available yet."
          }
        />
      ) : (
        <ProjectTable projects={loaderData.projects} />
      )}

      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.projects.length}
          noun="Projects"
          page={loaderData.page}
          pageSize={pageSize}
          total={loaderData.total}
        />
      ) : null}
    </Stack>
  );
}

function ProjectTable({ projects }: Readonly<{ projects: PromotionProject[] }>) {
  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={760} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Project</Table.Th>
              <Table.Th>Integration / Cluster</Table.Th>
              <Table.Th>Inventory</Table.Th>
              <Table.Th>Last observed</Table.Th>
              <Table.Th>Activity</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {projects.map((project) => (
              <ProjectRow
                key={[project.integration, project.cluster, project.project].join("/")}
                project={project}
              />
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

function ProjectRow({ project }: Readonly<{ project: PromotionProject }>) {
  const scope = `integration=${encodeURIComponent(project.integration)}&project=${encodeURIComponent(project.project)}`;

  return (
    <Table.Tr>
      <Table.Td>
        {project.kargoUrl ? (
          <KargoLink fw={650} url={project.kargoUrl}>
            {project.project}
          </KargoLink>
        ) : (
          <Text fw={650}>{project.project}</Text>
        )}
      </Table.Td>
      <Table.Td>
        <Group gap="xs" wrap="wrap">
          <Badge variant="light">{project.integration}</Badge>
          <Anchor component={Link} to={`/clusters/${encodeURIComponent(project.cluster)}`}>
            {project.cluster}
          </Anchor>
        </Group>
      </Table.Td>
      <Table.Td>
        <Group gap={6} wrap="wrap">
          <Anchor component={Link} to={`/promotion-stages?${scope}`}>
            {project.stageCount} Stage{project.stageCount === 1 ? "" : "s"}
          </Anchor>
          <Text c="dimmed" size="sm">
            ·
          </Text>
          <Anchor component={Link} to={`/promotion-pipelines?${scope}`}>
            {project.applicationCount} application{project.applicationCount === 1 ? "" : "s"}
          </Anchor>
        </Group>
      </Table.Td>
      <Table.Td>
        <RelativeTime size="sm" value={project.lastObservedAt} />
      </Table.Td>
      <Table.Td>
        <Anchor component={Link} to={`/promotions?${scope}`}>
          View Promotions
        </Anchor>
      </Table.Td>
    </Table.Tr>
  );
}

function optionalValue(value: string | null) {
  return value?.trim() || undefined;
}
