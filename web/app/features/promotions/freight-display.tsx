import { Badge, Code, Group, Stack, Text, Tooltip } from "@mantine/core";

import type { PromotionFreightItem } from "~/api/generated";
import {
  freightOriginLabel,
  freightSourceKindLabel,
} from "~/features/promotions/promotion-freight";
import { shortIdentifier } from "~/shared/display";
import { RelativeTime } from "~/shared/display-values";

export function FreightIdentifier({ freight }: Readonly<{ freight: PromotionFreightItem }>) {
  return (
    <Stack align="flex-start" gap={4}>
      <Tooltip label={freight.name} openDelay={350}>
        <Code style={{ width: "fit-content" }}>{shortIdentifier(freight.name)}</Code>
      </Tooltip>
      {freight.discoveredAt ? (
        <RelativeTime c="dimmed" size="xs" value={freight.discoveredAt} />
      ) : (
        <Text c="dimmed" size="xs">
          Unknown
        </Text>
      )}
    </Stack>
  );
}

export function FreightContents({ freight }: Readonly<{ freight: PromotionFreightItem }>) {
  return (
    <Stack gap={4}>
      {freight.artifacts.length > 0 ? (
        freight.artifacts.map((artifact) => (
          <Group
            align="center"
            gap="xs"
            key={`${artifact.kind}:${artifact.repository ?? ""}:${artifact.version}`}
            wrap="wrap"
          >
            <Badge size="xs" variant="light">
              {freightSourceKindLabel(artifact.kind)}
            </Badge>
            <Text size="xs" style={{ overflowWrap: "anywhere" }}>
              {artifact.repository ?? freightOriginLabel(freight.origin)}
            </Text>
            <Code>{artifact.version}</Code>
          </Group>
        ))
      ) : (
        <Text c="dimmed" size="xs">
          No versioned artifacts
        </Text>
      )}
    </Stack>
  );
}
