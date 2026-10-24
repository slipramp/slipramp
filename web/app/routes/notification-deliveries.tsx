import { Alert, Anchor, Badge, Group, Paper, Stack, Table, Text } from "@mantine/core";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listNotificationDeliveries, type NotificationDelivery } from "~/api/generated";
import { RelativeTime, Timestamp } from "~/components/display";
import { ListPagination } from "~/components/list-controls";
import { EmptyState, PageHeader } from "~/components/page";
import { shortIdentifier } from "~/display";
import {
  notificationDeliveryStateColor,
  notificationDeliveryStateLabel,
  notificationEventLabel,
  notificationSubjectHref,
  notificationSubjectLabel,
} from "~/notification-delivery";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/notification-deliveries";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Notification deliveries") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const page = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const result = await listNotificationDeliveries({
    client: createServerAPIClient(request, context),
    query: {
      limit: pageSize,
      offset: (page - 1) * pageSize,
    },
  });

  if (result.data) {
    const lastPage = Math.max(1, Math.ceil(result.data.total / pageSize));
    if (page > lastPage) {
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      return redirect(`${url.pathname}${url.search}`);
    }
  }

  return {
    available: result.data !== undefined,
    deliveries: result.data?.items ?? [],
    error: result.error?.detail ?? result.error?.title,
    page,
    total: result.data?.total ?? 0,
  };
}

export default function NotificationDeliveries({ loaderData }: Route.ComponentProps) {
  return (
    <Stack gap="lg">
      <PageHeader
        description="Outbound notification attempts and terminal delivery results, newest first."
        title="Notification deliveries"
      />

      {!loaderData.available ? (
        <Alert color="red" title="Notification history unavailable">
          {loaderData.error ?? "Notification delivery history could not be loaded."}
        </Alert>
      ) : loaderData.deliveries.length === 0 ? (
        <EmptyState
          description="No notification has matched a configured outbound route yet."
          title="No deliveries"
        />
      ) : (
        <DeliveryTable deliveries={loaderData.deliveries} />
      )}

      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.deliveries.length}
          noun="deliveries"
          page={loaderData.page}
          pageSize={pageSize}
          total={loaderData.total}
        />
      ) : null}
    </Stack>
  );
}

function DeliveryTable({ deliveries }: Readonly<{ deliveries: NotificationDelivery[] }>) {
  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={940} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Event / subject</Table.Th>
              <Table.Th>Destination</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Attempts</Table.Th>
              <Table.Th>Timing</Table.Th>
              <Table.Th>Last result</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {deliveries.map((delivery) => (
              <DeliveryRow delivery={delivery} key={delivery.id} />
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

function DeliveryRow({ delivery }: Readonly<{ delivery: NotificationDelivery }>) {
  return (
    <Table.Tr>
      <Table.Td>
        <Stack gap={0}>
          <Anchor component={Link} fw={600} to={notificationSubjectHref(delivery)}>
            {notificationEventLabel(delivery.eventType)}
          </Anchor>
          <Text c="dimmed" size="xs">
            {notificationSubjectLabel(delivery.subjectType)} {shortIdentifier(delivery.subjectId)}
          </Text>
        </Stack>
      </Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <Text size="sm">{delivery.destinationName}</Text>
          <Text c="dimmed" size="xs">
            {delivery.destinationType === "slack" ? "Slack" : delivery.destinationType}
          </Text>
        </Stack>
      </Table.Td>
      <Table.Td>
        <Badge color={notificationDeliveryStateColor(delivery.state)} variant="light">
          {notificationDeliveryStateLabel(delivery.state)}
        </Badge>
      </Table.Td>
      <Table.Td>
        <Text size="sm">
          {delivery.attemptCount} {delivery.attemptCount === 1 ? "attempt" : "attempts"}
        </Text>
      </Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <RelativeTime size="sm" value={delivery.occurredAt} />
          {delivery.lastAttemptedAt ? (
            <Group gap={4} wrap="nowrap">
              <Text c="dimmed" size="xs">
                Last attempt
              </Text>
              <Timestamp c="dimmed" size="xs" value={delivery.lastAttemptedAt} />
            </Group>
          ) : (
            <Text c="dimmed" size="xs">
              Not attempted
            </Text>
          )}
        </Stack>
      </Table.Td>
      <Table.Td>
        <DeliveryResult delivery={delivery} />
      </Table.Td>
    </Table.Tr>
  );
}

function DeliveryResult({ delivery }: Readonly<{ delivery: NotificationDelivery }>) {
  if (delivery.lastError) {
    return (
      <Stack gap={0} maw={360}>
        <Text c={delivery.state === "failed" ? "red" : undefined} size="sm">
          {delivery.lastError}
        </Text>
        {delivery.state === "pending" && delivery.nextAttemptAt ? (
          <Group gap={4} wrap="nowrap">
            <Text c="dimmed" size="xs">
              Next attempt
            </Text>
            <Timestamp c="dimmed" size="xs" value={delivery.nextAttemptAt} />
          </Group>
        ) : null}
      </Stack>
    );
  }

  if (delivery.deliveredAt) {
    return <Timestamp c="dimmed" size="sm" value={delivery.deliveredAt} />;
  }
  if (delivery.failedAt) {
    return <Timestamp c="dimmed" size="sm" value={delivery.failedAt} />;
  }

  if (delivery.nextAttemptAt) {
    return (
      <Stack gap={0}>
        <Text c="dimmed" size="sm">
          Waiting to deliver
        </Text>
        <Group gap={4} wrap="nowrap">
          <Text c="dimmed" size="xs">
            Next attempt
          </Text>
          <Timestamp c="dimmed" size="xs" value={delivery.nextAttemptAt} />
        </Group>
      </Stack>
    );
  }

  return (
    <Text c="dimmed" size="sm">
      Waiting to deliver
    </Text>
  );
}
