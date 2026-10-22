import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  NativeSelect,
  Paper,
  SimpleGrid,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { Form, Link } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listPromotions, type Promotion, type PromotionStatus } from "~/api/generated";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/promotions";

const promotionLimit = 100;

export function meta() {
  return [{ title: pageTitle("Promotions") }];
}

export async function loader({ request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const status = optionalStatus(url.searchParams.get("status"));
  const integration = optionalValue(url.searchParams.get("integration"));
  const project = optionalValue(url.searchParams.get("project"));
  const stageId = optionalValue(url.searchParams.get("stageId"));
  const client = createServerAPIClient(request);
  const query = { integration, limit: promotionLimit, project, stageId, status };
  const [result, pendingResult] = await Promise.all([
    listPromotions({ client, query }),
    status
      ? Promise.resolve(undefined)
      : listPromotions({ client, query: { ...query, status: "pending" } }),
  ]);
  const promotions = pendingResult?.data
    ? [
        ...pendingResult.data.items,
        ...(result.data?.items.filter((promotion) => promotion.status !== "pending") ?? []),
      ]
    : (result.data?.items ?? []);

  return {
    available:
      result.data !== undefined &&
      (pendingResult === undefined || pendingResult.data !== undefined),
    error:
      result.error?.detail ??
      result.error?.title ??
      pendingResult?.error?.detail ??
      pendingResult?.error?.title,
    filters: { integration, project, stageId, status },
    promotions,
    total: result.data?.total ?? 0,
  };
}

export default function Promotions({ loaderData }: Route.ComponentProps) {
  const promotions = loaderData.promotions.toSorted((left, right) => {
    const leftPending = left.status === "pending" ? 0 : 1;
    const rightPending = right.status === "pending" ? 0 : 1;
    return leftPending - rightPending || right.createdAt.localeCompare(left.createdAt);
  });

  return (
    <Stack gap="lg">
      <Group align="flex-start" justify="space-between">
        <Stack gap={2}>
          <Title order={1}>Promotions</Title>
          <Text c="dimmed">Kargo promotion preparation and operator review.</Text>
        </Stack>
        <Anchor component={Link} to="/promotion-stages">
          Manage Stage policies
        </Anchor>
      </Group>

      <PromotionFilters filters={loaderData.filters} />

      {!loaderData.available ? (
        <Alert color="red" title="Promotions unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : promotions.length === 0 ? (
        <Paper p="md" withBorder>
          <Text c="dimmed">No Promotions match these filters.</Text>
        </Paper>
      ) : (
        <PromotionTable promotions={promotions} />
      )}

      {loaderData.available && loaderData.total > promotions.length ? (
        <Text c="dimmed" size="sm">
          Showing the first {promotions.length} of {loaderData.total} matching Promotions.
        </Text>
      ) : null}
    </Stack>
  );
}

function PromotionFilters({
  filters,
}: Readonly<{
  filters: {
    integration?: string;
    project?: string;
    stageId?: string;
    status?: PromotionStatus;
  };
}>) {
  const filtered = Object.values(filters).some((value) => value !== undefined);

  return (
    <Paper p="md" withBorder>
      <Form method="get">
        <Stack gap="sm">
          <SimpleGrid cols={{ base: 1, md: 4 }}>
            <NativeSelect
              defaultValue={filters.status ?? ""}
              label="Status"
              name="status"
              data={[
                { label: "All statuses", value: "" },
                { label: "Pending review", value: "pending" },
                { label: "Preparing", value: "preparing" },
                { label: "Approved", value: "approved" },
                { label: "Rejected", value: "rejected" },
                { label: "Expired", value: "expired" },
                { label: "Error", value: "error" },
              ]}
            />
            <TextInput defaultValue={filters.integration} label="Integration" name="integration" />
            <TextInput defaultValue={filters.project} label="Project" name="project" />
            <TextInput defaultValue={filters.stageId} label="Stage ID" name="stageId" />
          </SimpleGrid>
          <Group justify="flex-end">
            {filtered ? (
              <Button component={Link} to="/promotions" variant="subtle">
                Clear
              </Button>
            ) : null}
            <Button type="submit" variant="light">
              Apply filters
            </Button>
          </Group>
        </Stack>
      </Form>
    </Paper>
  );
}

function PromotionTable({ promotions }: Readonly<{ promotions: Promotion[] }>) {
  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={1100}>
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Promotion</Table.Th>
              <Table.Th>Integration</Table.Th>
              <Table.Th>Stage</Table.Th>
              <Table.Th>Application</Table.Th>
              <Table.Th>Revisions</Table.Th>
              <Table.Th>Requested by</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Created</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {promotions.map((promotion) => (
              <PromotionRow key={promotion.id} promotion={promotion} />
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

function PromotionRow({ promotion }: Readonly<{ promotion: Promotion }>) {
  const stageName =
    snapshotString(promotion.stageSnapshot, "name") ?? promotion.stageId.slice(0, 8);
  const project = snapshotString(promotion.stageSnapshot, "project");
  const application = snapshotString(promotion.stageSnapshot, "application") ?? "—";

  return (
    <Table.Tr>
      <Table.Td>
        <Stack gap={0}>
          <Anchor component={Link} fw={600} to={`/promotions/${promotion.id}`}>
            {promotion.externalPromotionId}
          </Anchor>
          <Text c="dimmed" size="xs">
            {promotion.id.slice(0, 8)}
          </Text>
        </Stack>
      </Table.Td>
      <Table.Td>{promotion.integration}</Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <Text>{stageName}</Text>
          {project ? (
            <Text c="dimmed" size="xs">
              {project}
            </Text>
          ) : null}
        </Stack>
      </Table.Td>
      <Table.Td>{application}</Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <Code>{promotion.baseRevision.slice(0, 12)}</Code>
          <Text c="dimmed" size="xs">
            to <Code>{promotion.candidateRevision.slice(0, 12)}</Code>
          </Text>
        </Stack>
      </Table.Td>
      <Table.Td>{promotion.requestedBy.displayName}</Table.Td>
      <Table.Td>
        <Badge color={promotionStatusColor(promotion.status)} variant="light">
          {promotionStatusLabel(promotion.status)}
        </Badge>
      </Table.Td>
      <Table.Td>
        <Text component="time" dateTime={promotion.createdAt} size="sm">
          {formatTimestamp(promotion.createdAt)}
        </Text>
      </Table.Td>
    </Table.Tr>
  );
}

export function promotionStatusColor(status: PromotionStatus) {
  switch (status) {
    case "pending":
      return "yellow";
    case "approved":
      return "green";
    case "rejected":
    case "error":
      return "red";
    case "expired":
      return "gray";
    case "preparing":
      return "blue";
  }
}

export function promotionStatusLabel(status: PromotionStatus) {
  switch (status) {
    case "pending":
      return "Pending review";
    case "preparing":
      return "Preparing diffs";
    case "approved":
      return "Approved";
    case "rejected":
      return "Rejected";
    case "expired":
      return "Expired";
    case "error":
      return "Preparation failed";
  }
}

export function snapshotString(snapshot: Record<string, unknown>, key: string) {
  const value = snapshot[key];
  return typeof value === "string" ? value : undefined;
}

function optionalStatus(value: string | null): PromotionStatus | undefined {
  switch (value) {
    case "preparing":
    case "pending":
    case "approved":
    case "rejected":
    case "expired":
    case "error":
      return value;
    default:
      return undefined;
  }
}

function optionalValue(value: string | null) {
  return value?.trim() || undefined;
}

function formatTimestamp(value: string) {
  return new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}
