import { Anchor, Group, Paper, Stack, Table, Text } from "@mantine/core";
import { Link } from "react-router";

import type { Promotion } from "~/api/generated";
import { promotionActivity } from "~/features/mascot/execution-activity";
import { KawaiiActivity } from "~/features/mascot/kawaii-activity";
import { PromotionLink } from "~/features/promotions/promotion-link";
import { PromotionProjectLink } from "~/features/promotions/promotion-project-link";
import { PromotionStageLink } from "~/features/promotions/promotion-stage-link";
import { promotionOverallStatus } from "~/features/promotions/promotion-status";
import { PromotionStatusBadge } from "~/features/promotions/promotion-status-badge";
import { abbreviateMiddle } from "~/shared/display";
import { RelativeTime } from "~/shared/display-values";
import { Identity } from "~/shared/identity";

export function PromotionTable({ promotions }: Readonly<{ promotions: Promotion[] }>) {
  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={940} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Promotion / Stage</Table.Th>
              <Table.Th miw={240} w={240}>
                Status
              </Table.Th>
              <Table.Th>Application</Table.Th>
              <Table.Th>Requested</Table.Th>
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
  const stageName = promotion.stageSnapshot.name;
  const application = promotion.stageSnapshot.enrichment?.application ?? "Kargo native";
  const status = promotionOverallStatus(promotion);

  return (
    <Table.Tr>
      <Table.Td>
        <Stack gap={0}>
          <PromotionLink
            label={abbreviateMiddle(promotion.externalPromotionId, 36)}
            promotion={promotion}
            title={promotion.externalPromotionId}
          />
          <Group gap={4} wrap="nowrap">
            <PromotionStageLink
              c="dimmed"
              fw={500}
              size="xs"
              stage={{ id: promotion.stageId, name: stageName }}
            />
            <Text c="dimmed" size="xs">
              ·
            </Text>
            <PromotionProjectLink c="dimmed" fw={500} project={promotion.stageSnapshot} size="xs" />
          </Group>
        </Stack>
      </Table.Td>
      <Table.Td>
        <Group gap="xs" justify="space-between" miw="max-content" wrap="nowrap">
          <PromotionStatusBadge status={status} />
          <KawaiiActivity
            activity={promotionActivity(promotion)}
            animationKey={promotion.id}
            compact
          />
        </Group>
      </Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <Anchor
            component={Link}
            to={`/promotions?application=${encodeURIComponent(application)}`}
          >
            {application}
          </Anchor>
          <Text c="dimmed" size="xs">
            {promotion.integration}
          </Text>
        </Stack>
      </Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <Text size="sm">{promotion.source === "kargo" ? "Kargo" : "Slipramp"}</Text>
          <Identity
            avatarSize={22}
            name={promotion.requestedBy.displayName}
            nameProps={{ c: "dimmed", fw: 500, size: "xs" }}
          />
        </Stack>
      </Table.Td>
      <Table.Td style={{ whiteSpace: "nowrap" }}>
        <RelativeTime size="sm" value={promotion.createdAt} />
      </Table.Td>
    </Table.Tr>
  );
}
