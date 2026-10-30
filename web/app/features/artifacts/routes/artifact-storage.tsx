import { Alert, Paper, SimpleGrid, Stack, Text } from "@mantine/core";

import { createServerAPIClient } from "~/api/client.server";
import { getArtifactStorageUsage } from "~/api/generated";
import { artifactStoragePresentation } from "~/features/artifacts/artifact-storage";
import { PageHeader } from "~/shared/page";
import { pageTitle } from "~/shared/page-title";

import type { Route } from "./+types/artifact-storage";

export function meta() {
  return [{ title: pageTitle("Artifact storage") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const result = await getArtifactStorageUsage({
    client: createServerAPIClient(request, context),
  });

  return {
    error: result.error?.detail ?? result.error?.title,
    usage: result.data,
  };
}

export default function ArtifactStorage({ loaderData }: Route.ComponentProps) {
  const presentation = loaderData.usage ? artifactStoragePresentation(loaderData.usage) : undefined;

  return (
    <Stack gap="lg">
      <PageHeader
        description="Logical artifact-content consumption recorded in PostgreSQL metadata."
        title="Artifact storage"
      />

      {!loaderData.usage || !presentation ? (
        <Alert color="red" title="Artifact storage usage unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : (
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
          <UsageCard
            detail={presentation.exactBytes}
            label="Recorded content"
            value={presentation.recordedBytes}
          />
          <UsageCard
            detail="Open, sealed, and failed artifacts"
            label="Included metadata"
            value={presentation.artifactRecords}
          />
        </SimpleGrid>
      )}

      <Alert color="blue" title="Accounting semantics">
        <Stack gap="xs">
          <Text size="sm">
            The total sums each artifact&apos;s recorded size while its retention state is retained
            or expired. Live and failed logs contribute their durable recorded prefix. Expired
            artifacts remain counted until content deletion finishes and their metadata is marked
            deleted.
          </Text>
          <Text size="sm">
            This is recorded logical consumption, not object-store capacity, free space, physical
            allocation, replication overhead, or provider billing. This view does not define or
            enforce a quota.
          </Text>
        </Stack>
      </Alert>
    </Stack>
  );
}

function UsageCard({
  detail,
  label,
  value,
}: Readonly<{ detail: string; label: string; value: string }>) {
  return (
    <Paper p="lg" withBorder>
      <Stack gap={4}>
        <Text c="dimmed" size="xs" tt="uppercase">
          {label}
        </Text>
        <Text fw={700} size="xl">
          {value}
        </Text>
        <Text c="dimmed" size="sm">
          {detail}
        </Text>
      </Stack>
    </Paper>
  );
}
