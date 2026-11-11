import { Group, Paper, SimpleGrid, Stack, Text } from "@mantine/core";
import { useOutletContext } from "react-router";

import { RelativeTime, Timestamp } from "~/shared/display-values";
import { SectionHeader } from "~/shared/page";
import { CodeList, ResourceRequirementsTable, TechnicalValue } from "~/shared/technical-definition";

import type { PlacementOutletContext } from "./placement";

export default function PlacementOverview() {
  const { detail } = useOutletContext<PlacementOutletContext>();
  const { configurationChecksum, placement } = detail;

  return (
    <Stack gap="lg">
      <Paper p="lg" withBorder>
        <Stack gap="lg">
          <SectionHeader
            description="Defaults applied when a Target Action does not provide an override."
            title="Execution configuration"
          />
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <TechnicalValue label="Service account" value={placement.serviceAccount} />
            <TechnicalValue label="Default timeout" value={placement.timeout ?? "No default"} />
          </SimpleGrid>
          <ResourceRequirementsTable
            empty="No resource defaults"
            label="Resource defaults"
            limits={placement.resources.limits}
            requests={placement.resources.requests}
          />
          <CodeList
            empty="No logical credential sources"
            label="Credential names"
            values={placement.credentialNames}
          />
        </Stack>
      </Paper>

      <Paper p="lg" withBorder>
        <Stack gap="lg">
          <SectionHeader
            description="Observed connectivity and the effective configuration version behind this view."
            title="Server observation"
          />
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <Stack gap={4}>
              <Text c="dimmed" fw={700} size="xs" tt="uppercase">
                Last Controller contact
              </Text>
              {placement.controllerLastSeenAt ? (
                <Group gap="xs" wrap="wrap">
                  <RelativeTime fw={650} value={placement.controllerLastSeenAt} />
                  <Timestamp c="dimmed" size="xs" value={placement.controllerLastSeenAt} />
                </Group>
              ) : (
                <Text c="dimmed" size="sm">
                  Never checked in
                </Text>
              )}
            </Stack>
            <TechnicalValue
              description="Identifies the complete validated Placement set loaded by this Server."
              label="Configuration checksum"
              value={configurationChecksum}
            />
          </SimpleGrid>
        </Stack>
      </Paper>
    </Stack>
  );
}
